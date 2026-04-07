package notifier

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"

	"mon/config"
	"mon/proxy"
)

// SpugNotifier sends messages via the Spug push assistant (push.spug.cc).
type SpugNotifier struct {
	name              string
	userID            string
	channel           string
	client            *http.Client
	maxAttempts       int
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration
	logger            *zap.SugaredLogger
}

func NewSpugNotifier(name string, cfg *config.SpugNotifierConfig, logger *zap.SugaredLogger) (*SpugNotifier, error) {
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
	return &SpugNotifier{
		name:              name,
		userID:            cfg.UserID,
		channel:           cfg.Channel,
		client:            client,
		maxAttempts:       cfg.MaxAttempts,
		retryInitialDelay: time.Duration(*cfg.RetryInitialDelay),
		retryMaxDelay:     time.Duration(*cfg.RetryMaxDelay),
		logger:            logger,
	}, nil
}

func (s *SpugNotifier) Type() string { return "spug" }
func (s *SpugNotifier) Name() string { return s.name }

func (s *SpugNotifier) Notify(ctx context.Context, subject, body string) error {
	return withRetry(ctx, s.logger, s.name, s.maxAttempts, s.retryInitialDelay, s.retryMaxDelay, isRetryable, func(ctx context.Context) error {
		return s.send(ctx, subject, body)
	})
}

func (s *SpugNotifier) send(ctx context.Context, subject, body string) error {
	// Append a random nonce to the subject to avoid channels deduplicating repeated identical messages.
	var nonce [4]byte
	_, _ = rand.Read(nonce[:])
	subject = fmt.Sprintf("%s (%x)", subject, nonce)

	payload := map[string]any{
		"title":   subject,
		"content": body,
	}
	if s.channel != "" {
		payload["channel"] = s.channel
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://push.spug.cc/xsend/%s", url.PathEscape(s.userID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &httpAPIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}
