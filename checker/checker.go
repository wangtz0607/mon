package checker

import (
	"context"
	"fmt"
	"time"

	"mon/duration"
)

// Result holds the outcome of a single health check.
type Result struct {
	Time     time.Time         `json:"time"`
	OK       bool              `json:"ok"`
	Detail   string            `json:"detail"`
	Duration duration.Duration `json:"duration"`
}

func (r Result) String() string {
	var ok string
	if r.OK {
		ok = "✅ ok"
	} else {
		ok = "❌ error"
	}
	return fmt.Sprintf("[%s] %s: %s (%.3v)", r.Time.Format("2006-01-02T15:04:05.000Z07:00"), ok, r.Detail, r.Duration)
}

// Checker performs a health check against a service.
type Checker interface {
	Check(ctx context.Context) Result
	Type() string
}
