package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"mon/config"
	"mon/proxy"
)

var markdownReplacer = strings.NewReplacer(
	"_", "\\_",
	"*", "\\*",
	"[", "\\[",
	"]", "\\]",
	"(", "\\(",
	")", "\\)",
	"~", "\\~",
	"`", "\\`",
	">", "\\>",
	"#", "\\#",
	"+", "\\+",
	"-", "\\-",
	"=", "\\=",
	"|", "\\|",
	"{", "\\{",
	"}", "\\}",
	".", "\\.",
	"!", "\\!",
)

// TelegramNotifier sends messages via the Telegram Bot API.
type TelegramNotifier struct {
	name              string
	botToken          string
	chatID            string
	baseURL           string
	client            *http.Client
	maxAttempts       int
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration
	logger            *zap.SugaredLogger
}

func NewTelegramNotifier(name string, cfg *config.TelegramNotifierConfig, logger *zap.SugaredLogger) (*TelegramNotifier, error) {
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
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &TelegramNotifier{
		name:              name,
		botToken:          cfg.BotToken,
		chatID:            cfg.ChatID,
		baseURL:           baseURL,
		client:            client,
		maxAttempts:       cfg.MaxAttempts,
		retryInitialDelay: time.Duration(*cfg.RetryInitialDelay),
		retryMaxDelay:     time.Duration(*cfg.RetryMaxDelay),
		logger:            logger,
	}, nil
}

func (t *TelegramNotifier) Type() string { return "telegram" }
func (t *TelegramNotifier) Name() string { return t.name }

func (t *TelegramNotifier) Notify(ctx context.Context, subject, body string) error {
	return withRetry(ctx, t.logger, t.name, t.maxAttempts, t.retryInitialDelay, t.retryMaxDelay, isRetryable, func(ctx context.Context) error {
		return t.send(ctx, subject, body)
	})
}

func (t *TelegramNotifier) send(ctx context.Context, subject, body string) error {
	text := fmt.Sprintf("*%s*\n%s", escapeMarkdown(subject), escapeMarkdown(body))
	payload := map[string]string{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", t.baseURL, t.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
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

// escapeMarkdown escapes special characters for Telegram MarkdownV2.
func escapeMarkdown(s string) string {
	return markdownReplacer.Replace(s)
}
