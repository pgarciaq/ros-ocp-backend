package reship

import "context"

type reshipAttemptKey struct{}

// WithReshipAttempt attaches the poller retry attempt number to ctx for observability.
func WithReshipAttempt(ctx context.Context, attempt int) context.Context {
	if attempt <= 0 {
		attempt = 1
	}
	return context.WithValue(ctx, reshipAttemptKey{}, attempt)
}

func reshipAttemptFromContext(ctx context.Context) int {
	if ctx == nil {
		return 1
	}
	if attempt, ok := ctx.Value(reshipAttemptKey{}).(int); ok && attempt > 0 {
		return attempt
	}
	return 1
}
