package checker

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"mon/config"
	"mon/duration"
)

// CustomChecker runs a shell command and treats exit code 0 as success.
type CustomChecker struct {
	command string
	shell   string
	timeout time.Duration
}

func NewCustomChecker(cfg *config.CustomCheckerConfig) *CustomChecker {
	return &CustomChecker{
		command: cfg.Command,
		shell:   cfg.Shell,
		timeout: time.Duration(cfg.Timeout),
	}
}

func (c *CustomChecker) Type() string { return "custom" }

func (c *CustomChecker) Check(ctx context.Context) Result {
	start := time.Now()

	cmdCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, c.shell, "-c", c.command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 50 * time.Millisecond
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
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
