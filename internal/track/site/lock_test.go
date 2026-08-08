package site

import (
	"bytes"
	"strings"
	"testing"
)

// TestLockRoundTrip pins the published format: locked bytes are not the data (no plaintext survives in
// them), and Unlock with the site's key gives it back exactly.
func TestLockRoundTrip(t *testing.T) {
	key := LockKey("https://example.test", PublishID(100))
	plain := []byte(`{"notes":[{"note_id":"abc","title":"Secret Title"}]}`)

	blob, err := lock(key, plain)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if bytes.Contains(blob, []byte("Secret Title")) || bytes.Contains(blob, []byte("note_id")) {
		t.Fatalf("locked bytes still read as data:\n%q", blob)
	}

	got, err := Unlock(key, blob)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip changed the data:\ngot  %s\nwant %s", got, plain)
	}

	// Another site's key does not open it, and the key is stable across rebuilds of the same site.
	if _, err := Unlock(LockKey("https://other.test", PublishID(100)), blob); err == nil {
		t.Fatalf("a different site's key should not unlock the bundle")
	}
	if !bytes.Equal(key, LockKey("https://example.test", PublishID(100))) {
		t.Fatalf("the key should be stable for the same site")
	}
}

// TestLockKeyString is the form the page carries (web/src/lock.ts decodes exactly this).
func TestLockKeyString(t *testing.T) {
	s := LockKeyString(LockKey("", ""))
	if len(s) == 0 || strings.ContainsAny(s, `"<>`) {
		t.Fatalf("key must be inlineable in HTML, got %q", s)
	}
}
