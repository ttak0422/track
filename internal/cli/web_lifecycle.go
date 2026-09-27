package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// These process-scoped variables are private coordination between Track.app and its own Go child.
// They do not change the track CLI's documented flags or web protocol.
const (
	desktopParentPIDEnv  = "TRACK_WEB_DESKTOP_PARENT_PID"
	desktopReadyTokenEnv = "TRACK_WEB_DESKTOP_READY_TOKEN"
)

func webServerContext() (context.Context, context.CancelFunc, error) {
	rawParentPID := os.Getenv(desktopParentPIDEnv)
	if rawParentPID == "" {
		// Preserve the existing CLI's signal behavior. Contextual cleanup is opt-in for the app's
		// explicitly supervised child, not a new public track web lifecycle mode.
		return context.Background(), func() {}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	parentPID, err := strconv.Atoi(rawParentPID)
	if err != nil || parentPID <= 1 {
		cancel()
		return nil, nil, fmt.Errorf("invalid desktop supervisor pid %q", rawParentPID)
	}
	if os.Getppid() != parentPID {
		cancel()
		return nil, nil, fmt.Errorf("desktop supervisor %d is not this process's parent", parentPID)
	}
	watchDesktopParent(ctx, cancel, parentPID, os.Getppid, 100*time.Millisecond)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(signals)
		select {
		case <-ctx.Done():
			return
		case <-signals:
			cancel()
		}
	}()
	return ctx, cancel, nil
}

func watchDesktopParent(ctx context.Context, cancel context.CancelFunc, expectedPID int, parentPID func() int, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// A reparented child means the app crashed or was forcibly terminated. A PID check
				// alone can mistake a recycled PID for the parent; getppid is the kernel's current
				// parent relationship and changes as soon as the app is gone.
				if parentPID() != expectedPID {
					cancel()
					return
				}
			}
		}
	}()
}
