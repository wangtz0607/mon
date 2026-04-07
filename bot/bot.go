package bot

import "context"

type Bot interface {
	Type() string
	Name() string
	// Start runs the bot's event loop. It blocks until ctx is cancelled.
	Start(ctx context.Context) error
}
