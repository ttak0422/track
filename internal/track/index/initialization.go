package index

import (
	"fmt"
	"os"
	"syscall"
)

// EnsureInitialized populates a new or interrupted cache before a caller relies
// on its title dictionary. Full preserves historical activity metadata, unlike
// treating every previously unindexed note as a newly observed edit.
func (ix *Indexer) EnsureInitialized() (bool, error) {
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
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return false, fmt.Errorf("lock index initialization: %w", err)
	}
	initialized, err = ix.store.Initialized()
	if err != nil || initialized {
		return false, err
	}
	_, err = ix.Full()
	return err == nil, err
}
