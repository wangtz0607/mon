package notifier

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"time"

	"go.uber.org/zap"
)

// httpAPIError represents an error response from an HTTP API with a status code.
type httpAPIError struct {
	StatusCode int
	Body       string
}

func (e *httpAPIError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// isRetryable determines whether an error is worth retrying.
// Network errors are retryable. For HTTP responses, 429 and 5xx are retryable; other 4xx are not.
func isRetryable(err error) bool {
	var apiErr *httpAPIError
	if errors.As(err, &apiErr) {
		return isRetryableHTTPStatus(apiErr.StatusCode)
	}
	// Network errors (dial, DNS, timeout, etc.) are retryable.
	var netErr net.Error
	return errors.As(err, &netErr)
}

// isRetryableHTTPStatus returns true for HTTP 429 (rate limit) and 5xx (server errors).
func isRetryableHTTPStatus(code int) bool {
	return code == 429 || code >= 500
}

// withRetry calls fn up to maxAttempts times, using exponential backoff
// with full jitter between attempts. The delay for attempt n is a random
// value in (0, min(initialDelay*2^(n-1), maxDelay)].
// isRetryable determines whether a failed attempt should be retried.
func withRetry(ctx context.Context, logger *zap.SugaredLogger, name string, maxAttempts int, initialDelay, maxDelay time.Duration, isRetryable func(error) bool, fn func(context.Context) error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			base := initialDelay * time.Duration(1<<uint(attempt-1))
			if base > maxDelay || base <= 0 { // base <= 0 catches overflow
				base = maxDelay
			}
			wait := time.Duration(rand.Int64N(int64(base))) + 1
			logger.Warnf("%s: %v", name, err)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		err = fn(ctx)
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			logger.Warnf("%s: %v", name, err)
			return err
		}
	}
	return err
}
