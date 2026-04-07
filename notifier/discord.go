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

// DiscordNotifier sends messages via a Discord incoming webhook.
type DiscordNotifier struct {
	name              string
	webhookURL        string
	client            *http.Client
	maxAttempts       int
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration
	logger            *zap.SugaredLogger
}

func NewDiscordNotifier(name string, cfg *config.DiscordNotifierConfig, logger *zap.SugaredLogger) (*DiscordNotifier, error) {
	transport, err := proxy.HTTPTransport(cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}
	client := &http.Client{Timeout: time.Duration(cfg.Timeout)}
	if transport != nil {
		client.Transport = transport
	}
	return &DiscordNotifier{
		name:              name,
		webhookURL:        cfg.WebhookURL,
		client:            client,
		maxAttempts:       cfg.MaxAttempts,
		retryInitialDelay: time.Duration(*cfg.RetryInitialDelay),
		retryMaxDelay:     time.Duration(*cfg.RetryMaxDelay),
		logger:            logger,
	}, nil
}

func (d *DiscordNotifier) Type() string { return "discord" }
func (d *DiscordNotifier) Name() string { return d.name }

func (d *DiscordNotifier) Notify(ctx context.Context, subject, body string) error {
	return withRetry(ctx, d.logger, d.name, d.maxAttempts, d.retryInitialDelay, d.retryMaxDelay, isRetryable, func(ctx context.Context) error {
		return d.send(ctx, subject, body)
	})
}

type discordEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       int    `json:"color"`
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

func discordColor(subject string) int {
	switch {
	case strings.Contains(subject, "🔴"):
		return 0xE74C3C // red
	case strings.Contains(subject, "🟢"):
		return 0x2ECC71 // green
	default:
		return 0x95A5A6 // gray
	}
}

func (d *DiscordNotifier) send(ctx context.Context, subject, body string) error {
	payload := discordPayload{
		Embeds: []discordEmbed{{
			Title:       subject,
			Description: body,
			Color:       discordColor(subject),
		}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Discord returns 204 No Content on success
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &httpAPIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}
