package note

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

var ErrETagMismatch = errors.New("note changed on disk; reload and retry")

// ContentETag returns the optimistic-concurrency token used for note writes. It is derived from
// the exact bytes on disk, so any edit — including one that only shifts task line numbers — makes
// a token stale.
func ContentETag(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

// CheckContentETag verifies that raw is the exact note version a client previously loaded.
func CheckContentETag(raw []byte, expected string) error {
	if ContentETag(raw) != expected {
		return ErrETagMismatch
	}
	return nil
}
