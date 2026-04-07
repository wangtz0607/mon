package checker

import (
	"context"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"mon/config"
	"mon/duration"
)

// PingChecker checks host reachability using the system ping command.
type PingChecker struct {
	host    string
	timeout time.Duration
}

func NewPingChecker(cfg *config.PingCheckerConfig) *PingChecker {
	return &PingChecker{
		host:    cfg.Host,
		timeout: time.Duration(cfg.Timeout),
	}
}

func (c *PingChecker) Type() string { return "ping" }

func (c *PingChecker) Check(ctx context.Context) Result {
	start := time.Now()
	timeoutSecs := int(math.Ceil(c.timeout.Seconds()))

	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(timeoutSecs), "--", c.host)
	out, err := cmd.CombinedOutput()
	elapsed := duration.Since(start)

	detail := strings.TrimSpace(string(out))
	if detail == "" && err != nil {
		detail = err.Error()
	}
	if err != nil {
		return Result{Time: start, OK: false, Detail: detail, Duration: elapsed}
	}
	return Result{Time: start, OK: true, Detail: detail, Duration: elapsed}
}
