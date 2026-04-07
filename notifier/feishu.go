package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"

	"mon/config"
	"mon/proxy"
)

// FeishuNotifier sends messages via a Feishu group bot webhook.
type FeishuNotifier struct {
	name              string
	webhookURL        string
	secret            string
	client            *http.Client
	maxAttempts       int
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration
	logger            *zap.SugaredLogger
}

func NewFeishuNotifier(name string, cfg *config.FeishuNotifierConfig, logger *zap.SugaredLogger) (*FeishuNotifier, error) {
	transport, err := proxy.HTTPTransport(cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}
	client := &http.Client{
		Timeout: time.Duration(cfg.Timeout),
	}
	if transport != nil {
		client.Transport = transport
	}
	return &FeishuNotifier{
		name:              name,
		webhookURL:        cfg.WebhookURL,
		secret:            cfg.Secret,
		client:            client,
		maxAttempts:       cfg.MaxAttempts,
		retryInitialDelay: time.Duration(*cfg.RetryInitialDelay),
		retryMaxDelay:     time.Duration(*cfg.RetryMaxDelay),
		logger:            logger,
	}, nil
}

func (f *FeishuNotifier) Type() string { return "feishu" }
func (f *FeishuNotifier) Name() string { return f.name }

func (f *FeishuNotifier) Notify(ctx context.Context, subject, body string) error {
	return withRetry(ctx, f.logger, f.name, f.maxAttempts, f.retryInitialDelay, f.retryMaxDelay, isRetryableFeishu, func(ctx context.Context) error {
		return f.send(ctx, subject, body)
	})
}

func (f *FeishuNotifier) send(ctx context.Context, subject, body string) error {
	payload := map[string]any{
		"msg_type": "post",
		"content": map[string]any{
			"post": map[string]any{
				"en_us": map[string]any{
					"title": subject,
					"content": [][]map[string]any{
						{{"tag": "text", "text": body}},
					},
				},
			},
		},
	}

	if f.secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		payload["timestamp"] = ts
		payload["sign"] = feishuSign(ts, f.secret)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.webhookURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return &httpAPIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response: %s", respBody)
	}
	if result.Code != 0 {
		return &feishuAPIError{Code: result.Code, Msg: result.Msg}
	}
	return nil
}

// feishuAPIError represents a Feishu API business error.
type feishuAPIError struct {
	Code int
	Msg  string
}

func (e *feishuAPIError) Error() string {
	return fmt.Sprintf("feishu API error: (%d) %s", e.Code, e.Msg)
}

// isRetryableFeishu determines whether a Feishu error is worth retrying.
// Code 11232 (rate limited due to system pressure) is retryable;
// other API errors are not.
func isRetryableFeishu(err error) bool {
	var feishuErr *feishuAPIError
	if errors.As(err, &feishuErr) {
		return feishuErr.Code == 11232
	}
	return isRetryable(err)
}

// feishuSign computes the webhook signature.
// stringToSign = timestamp + "\n" + secret
// sign = base64(HMAC-SHA256(key=stringToSign, data=""))
func feishuSign(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	h := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
