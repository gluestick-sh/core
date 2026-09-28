package bootstrap

import (
	"context"
)

// contextOrBackground returns ctx, or context.Background() when ctx is nil, so
// callers can pass a nil context without panicking.
func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
