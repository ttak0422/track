package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// EnsureInitialized populates a new or interrupted cache before a caller relies
// on its title dictionary. Full preserves historical activity metadata, unlike
// treating every previously unindexed note as a newly observed edit.
func (ix *Indexer) EnsureInitialized() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), StageBudget)
	defer cancel()
	return ix.EnsureInitializedContext(ctx)
}

func (ix *Indexer) EnsureInitializedContext(ctx context.Context) (bool, error) {
	initialized, err := ix.store.Initialized()
	if err != nil || initialized {
		return false, err
	}
	// Serialize cold-start builders across CLI, LSP and web processes, then check
	// again. Otherwise a slower startup could rebuild over a note saved after the
	// first startup finished. Never unlink this lock file while users hold it.
	lock, err := os.OpenFile(ix.cfg.DBPath+".init.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, fmt.Errorf("open index initialization lock: %w", err)
	}
	defer lock.Close()
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return false, fmt.Errorf("lock index initialization: %w", err)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, fmt.Errorf("index initialization is still pending: %w", ctx.Err())
		case <-timer.C:
		}
	}
	initialized, err = ix.store.Initialized()
	if err != nil || initialized {
		return false, err
	}
	snap, err := ix.Stage(ctx, nil)
	if err == nil {
		err = ix.Publish(snap)
	}
	return err == nil, err
}
