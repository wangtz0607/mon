package notifier

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"go.uber.org/zap"

	"mon/config"
)

// CustomNotifier runs a shell command to send a notification.
// The subject and body are passed via MON_SUBJECT and MON_BODY
// environment variables. Exit code 0 is treated as success.
type CustomNotifier struct {
	name              string
	command           string
	shell             string
	timeout           time.Duration
	maxAttempts       int
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration
	logger            *zap.SugaredLogger
}

func NewCustomNotifier(name string, cfg *config.CustomNotifierConfig, logger *zap.SugaredLogger) (*CustomNotifier, error) {
	return &CustomNotifier{
		name:              name,
		command:           cfg.Command,
		shell:             cfg.Shell,
		timeout:           time.Duration(cfg.Timeout),
		maxAttempts:       cfg.MaxAttempts,
		retryInitialDelay: time.Duration(*cfg.RetryInitialDelay),
		retryMaxDelay:     time.Duration(*cfg.RetryMaxDelay),
		logger:            logger,
	}, nil
}

func (c *CustomNotifier) Type() string { return "custom" }
func (c *CustomNotifier) Name() string { return c.name }

func (c *CustomNotifier) Notify(ctx context.Context, subject, body string) error {
	return withRetry(ctx, c.logger, c.name, c.maxAttempts, c.retryInitialDelay, c.retryMaxDelay, func(error) bool { return true }, func(ctx context.Context) error {
		return c.run(ctx, subject, body)
	})
}

func (c *CustomNotifier) run(ctx context.Context, subject, body string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, c.shell, "-c", c.command)
	cmd.Env = append(os.Environ(), "MON_SUBJECT="+subject, "MON_BODY="+body)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 50 * time.Millisecond
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := string(out)
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("command exited with error: %s", detail)
	}
	return nil
}
