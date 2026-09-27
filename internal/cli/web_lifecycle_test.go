package cli

import (
	"context"
	"testing"
	"time"
)

func TestWatchDesktopParentCancelsAfterReparenting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	watchDesktopParent(ctx, cancel, 4242, func() int { return 1 }, time.Millisecond)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("web server context did not stop after its app parent disappeared")
	}
}

func TestWatchDesktopParentStopsWithServerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	watchDesktopParent(ctx, cancel, 4242, func() int { return 4242 }, time.Millisecond)
	cancel()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("web server context did not cancel")
	}
}
