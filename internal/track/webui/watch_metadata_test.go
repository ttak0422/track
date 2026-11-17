//go:build linux || darwin

package webui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// Poll only committed SQLite state, never an HTTP read that could repair the
// missed watcher event and turn this regression into a false positive.
func awaitMetadataChange(t *testing.T, ch <-chan serverEvent, ready func() bool) {
	t.Helper()
	timeout := time.After(8 * time.Second)
	for {
		select {
		case event := <-ch:
			if event.name == "change" && ready() {
				return
			}
		case <-timeout:
			t.Fatal("metadata-only change was not indexed and broadcast without a read request")
		}
	}
}
func hasWatchedStatus(st *store.Store, id int64, status string) bool {
	props, err := st.NoteProps(id)
	if err != nil {
		return false
	}
	for _, prop := range props {
		if prop.Key == "status" && prop.Value == status {
			return true
		}
	}
	return false
}
func TestWatchMetadataDirectoryCreatedAfterStartup(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), Extensions: []string{".md"}, DateFormat: "2006-01-02", JournalDateFormat: "20060102"}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(cfg, st)
	defer s.closeViews()
	s.startWatch()
	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)
	if err := os.MkdirAll(cfg.NoteDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(7), note.Metadata{Title: "Fixture", Props: map[string]any{"status": "ALPHA"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(7), []byte("unchanged host body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	awaitMetadataChange(t, ch, func() bool { return hasWatchedStatus(st, 7, "ALPHA") })
	// No note body or directory changes from here: only the nested sidecar.
	if err := note.WriteMetadata(cfg.MetadataPath(7), note.Metadata{Title: "Fixture", Props: map[string]any{"status": "BRAVO"}}); err != nil {
		t.Fatal(err)
	}
	// The legacy (non-canonical) freshness path intentionally compares second
	// mtimes. Advance this fixture timestamp without sleeping or changing body.
	changed := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfg.MetadataPath(7), changed, changed); err != nil {
		t.Fatal(err)
	}
	awaitMetadataChange(t, ch, func() bool { return hasWatchedStatus(st, 7, "BRAVO") })
}
func TestWatchCanonicalMetadataUpdateAndLossRetainSafety(t *testing.T) {
	f := newSentinelFixture(t)
	policy, err := newReadPolicy(f.cache, []string{f.configuration})
	if err != nil {
		t.Fatal(err)
	}
	st, err := policy.open(f.cfg, store.Open)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := newServerWithReadPolicy(f.cfg, st, true, policy)
	defer s.closeViews()
	s.enableAsync()
	s.refresh(s.active)
	sentinelViewReady(t, s.active)
	s.startWatch()
	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)
	meta, _, err := note.ReadMetadata(f.cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	meta.Props = map[string]any{"status": "CHARLIE"}
	if err := note.WriteMetadata(f.cfg.MetadataPath(100), meta); err != nil {
		t.Fatal(err)
	}
	baseline := canonicalInventory(t, f.canonical()...)
	awaitMetadataChange(t, ch, func() bool { return hasWatchedStatus(st, 100, "CHARLIE") })
	assertCanonicalInventory(t, baseline, f.canonical()...)
	if err := os.Remove(f.cfg.MetadataPath(100)); err != nil {
		t.Fatal(err)
	}
	baseline = canonicalInventory(t, f.canonical()...)
	awaitMetadataChange(t, ch, func() bool {
		state, err := st.ReadState()
		return err == nil && state.Partial && hasWatchedStatus(st, 100, "CHARLIE")
	})
	if _, err := os.Stat(f.cfg.MetadataPath(100)); !os.IsNotExist(err) {
		t.Fatalf("missing sidecar was recreated: %v", err)
	}
	assertCanonicalInventory(t, baseline, f.canonical()...)
}
