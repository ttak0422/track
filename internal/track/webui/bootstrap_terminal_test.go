package webui

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

// Drive the actual extracted strict-worker path with a deterministic main-open
// failure and teardown, without relying on goroutine timing or sleeps.
func TestBootstrapStrictTeardownPreservesOpenFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	teardown := make(chan struct{})
	cause := errors.New("read-only cache protected vault: fixture peer is missing")
	b := newBootstrap(context.Background(), func(ctx context.Context, _ string, strict bool) (*config.Config, error) {
		if !strict {
			t.Error("expected strict worker")
		}
		close(entered)
		<-ctx.Done()
		return nil, fmt.Errorf("strict worker stopped: %w", ctx.Err())
	}, func(string) (*store.Store, error) { return nil, cause }, nil)
	b.generation = 1
	b.status.Generation = 1
	result := b.strictConfiguration(ctx, 1, teardown)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("strict worker did not start")
	}
	_, err := b.open("unused synthetic cache")
	b.fail(1, err)
	// Same ordering as start's deferred owner teardown: mark, then cancel.
	close(teardown)
	cancel()
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("strict worker did not finish")
	}
	status := b.Status()
	if status.Reason != cause.Error() || status.Phase != "unavailable" || !status.ReadOnly {
		t.Fatalf("terminal cause overwritten: %+v", status)
	}
}

func TestBootstrapIndependentStrictErrorsRemainVisible(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		ended bool
	}{
		{"independent cancellation", context.Canceled, false},
		{"strict deadline after owner ended", context.DeadlineExceeded, true},
		{"real config failure after owner ended", errors.New("invalid peer configuration"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			teardown := make(chan struct{})
			if tc.ended {
				close(teardown)
			}
			b := newBootstrap(context.Background(), func(context.Context, string, bool) (*config.Config, error) { return nil, tc.err }, nil, nil)
			b.generation = 1
			b.status.Generation = 1
			b.fail(1, errors.New("previous cause"))
			result := b.strictConfiguration(context.Background(), 1, teardown)
			select {
			case <-result:
			case <-time.After(time.Second):
				t.Fatal("strict worker did not finish")
			}
			if got := b.Status().Reason; got != tc.err.Error() {
				t.Fatalf("independent error hidden: %q", got)
			}
		})
	}
}
