package notifier

import "context"

// Notifier sends alert messages.
type Notifier interface {
	Type() string
	Name() string
	Notify(ctx context.Context, subject, body string) error
}
