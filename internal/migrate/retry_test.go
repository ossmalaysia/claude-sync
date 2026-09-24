package migrate

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

func noSleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

func testOpts() Options {
	o := DefaultOptions()
	o.Sleep = noSleep
	o.WritePause = 0
	return o
}

func httpErr(status int, kind error) error { return &claudeapi.HTTPError{Status: status, Kind: kind} }

func TestWithRetryRetriesTransientThenSucceeds(t *testing.T) {
	calls := 0
	v, attempts, err := withRetry(context.Background(), testOpts(), func() (int, error) {
		calls++
		if calls < 3 {
			return 0, httpErr(429, claudeapi.ErrRateLimited)
		}
		return 7, nil
	})
	if err != nil || v != 7 || attempts != 3 {
		t.Fatalf("got v=%d attempts=%d err=%v", v, attempts, err)
	}
}

func TestWithRetryGivesUpAfterFiveAttempts(t *testing.T) {
	calls := 0
	_, attempts, err := withRetry(context.Background(), testOpts(), func() (int, error) {
		calls++
		return 0, httpErr(503, claudeapi.ErrServer)
	})
	if !errors.Is(err, claudeapi.ErrServer) || calls != 5 || attempts != 5 {
		t.Fatalf("calls=%d attempts=%d err=%v", calls, attempts, err)
	}
}

func TestWithRetryDoesNotRetryPermanentErrors(t *testing.T) {
	for _, e := range []error{
		httpErr(400, nil),
		httpErr(401, claudeapi.ErrAuth),
		httpErr(403, claudeapi.ErrForbidden),
		httpErr(404, claudeapi.ErrNotFound),
		context.Canceled,
	} {
		calls := 0
		_, _, err := withRetry(context.Background(), testOpts(), func() (int, error) { calls++; return 0, e })
		if calls != 1 || !errors.Is(err, e) {
			t.Errorf("%v: calls=%d err=%v", e, calls, err)
		}
	}
}

func TestWithRetryRetriesNetworkErrors(t *testing.T) {
	calls := 0
	_, attempts, err := withRetry(context.Background(), testOpts(), func() (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("TypeError: Failed to fetch")
		}
		return 1, nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestWithRetryStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := withRetry(ctx, testOpts(), func() (int, error) { return 0, httpErr(429, claudeapi.ErrRateLimited) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

func TestWithRetryDoesNotRetryClosedWindow(t *testing.T) {
	calls := 0
	_, attempts, err := withRetry(context.Background(), testOpts(), func() (int, error) {
		calls++
		return 0, fmt.Errorf("eval: %w", claudeapi.ErrSessionGone)
	})
	if !errors.Is(err, claudeapi.ErrSessionGone) || calls != 1 || attempts != 1 {
		t.Fatalf("calls=%d attempts=%d err=%v", calls, attempts, err)
	}
}
