package migrate

import (
	"context"
	"errors"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

// withRetry calls fn until it succeeds, fails permanently, or runs out of
// attempts. It returns the number of attempts made.
func withRetry[T any](ctx context.Context, opts Options, fn func() (T, error)) (T, int, error) {
	var zero T
	for attempt := 1; ; attempt++ {
		v, err := fn()
		if err == nil {
			return v, attempt, nil
		}
		if !retryable(err) || attempt > len(opts.Backoff) {
			return zero, attempt, err
		}
		if serr := opts.Sleep(ctx, opts.Backoff[attempt-1]); serr != nil {
			return zero, attempt, serr
		}
	}
}

func retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, claudeapi.ErrSessionGone) {
		return false
	}
	var he *claudeapi.HTTPError
	if errors.As(err, &he) {
		return errors.Is(err, claudeapi.ErrRateLimited) || errors.Is(err, claudeapi.ErrServer)
	}
	return true // network or other browser errors are transient
}
