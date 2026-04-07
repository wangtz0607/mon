package checker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"mon/config"
	"mon/duration"
	"mon/proxy"
)

// HTTPGetChecker performs an HTTP GET and considers 2xx a success.
type HTTPGetChecker struct {
	url    string
	client *http.Client
}

func NewHTTPGetChecker(cfg *config.HTTPGetCheckerConfig) (*HTTPGetChecker, error) {
	transport, err := proxy.HTTPTransport(cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}

	// Configure redirect behavior: stop following redirects by default
	client := &http.Client{
		Timeout: time.Duration(cfg.Timeout),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !cfg.FollowRedirects {
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	if transport != nil {
		client.Transport = transport
	}

	return &HTTPGetChecker{
		url:    cfg.URL,
		client: client,
	}, nil
}

func (c *HTTPGetChecker) Type() string { return "http_get" }

func (c *HTTPGetChecker) Check(ctx context.Context) Result {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return Result{Time: start, OK: false, Detail: err.Error(), Duration: 0}
	}
	resp, err := c.client.Do(req)
	elapsed := duration.Since(start)
	if err != nil {
		return Result{Time: start, OK: false, Detail: err.Error(), Duration: elapsed}
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return Result{Time: start, OK: true, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode), Duration: elapsed}
	}
	return Result{Time: start, OK: false, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode), Duration: elapsed}
}
