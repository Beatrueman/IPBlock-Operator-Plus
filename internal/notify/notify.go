package notify

import "context"

type Notifier interface {
	Notify(ctx context.Context, eventType string, vars map[string]string) error
}
