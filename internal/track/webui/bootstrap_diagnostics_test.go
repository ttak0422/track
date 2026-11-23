package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func TestProgressRetainsCurrentFileDiagnosticsWithoutDatabaseAccess(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), VaultName: "work"}
	state := store.ReadState{Partial: true, CacheUsed: true, CheckedAt: 123, Omitted: 2,
		Issues: []store.ReadIssue{{NoteID: 2, Path: cfg.NotePath(2), Reason: "metadata unavailable", Cached: true}}}
	srv := &Server{active: &vaultView{cfg: cfg, progress: readProgress{verified: true, state: state}}}
	// No Store is supplied: serving progress must not wait on a database.
	b := newBootstrap(context.Background(), nil, nil, nil)
	b.cfg, b.server = cfg, srv
	b.generation, b.cfgGeneration, b.serverGeneration = 7, 7, 7
	b.progress[1] = &note.Note{ID: 1, Kind: "note", Meta: note.Metadata{Title: "Healthy"}, Body: "confirmed"}
	for _, path := range []string{"/api/notes?vault=work", "/api/search?q=Healthy&vault=work", "/api/note?id=1&vault=work"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			if !b.progressResponse(w, httptest.NewRequest("GET", path, nil), "storage verification is incomplete") {
				t.Fatal("confirmed read was unavailable")
			}
			var response struct {
				State   store.ReadState `json:"read_state"`
				Partial []struct {
					State store.ReadState `json:"read_state"`
				} `json:"partial"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			got := response.State
			if !got.Partial || !got.CacheUsed || got.CheckedAt != state.CheckedAt || got.Omitted != 2 || len(got.Issues) != 2 || got.Issues[0] != state.Issues[0] {
				t.Fatalf("file health was replaced: %s", w.Body.String())
			}
			if got.Issues[1].Path != cfg.VaultDir || got.Issues[1].Reason != "storage verification is incomplete" {
				t.Fatal("fallback uncertainty lost", got)
			}
			if len(response.Partial) > 0 && !reflect.DeepEqual(response.Partial[0].State, got) {
				t.Fatal("list health disagrees")
			}
			if !reflect.DeepEqual(srv.active.asyncState(), state) {
				t.Fatal("response changed shared health")
			}
		})
	}
	// A superseded server must never supply diagnostic data to a new request.
	b.serverGeneration--
	w := httptest.NewRecorder()
	b.progressResponse(w, httptest.NewRequest("GET", "/api/notes", nil), "new generation pending")
	var response struct {
		State store.ReadState `json:"read_state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.State.CacheUsed || response.State.CheckedAt != 0 || len(response.State.Issues) != 1 || response.State.Issues[0].NoteID != 0 {
		t.Fatalf("old server diagnostics escaped: %s", w.Body.String())
	}
}

func TestProgressDiagnosticsRemainBounded(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	state := store.ReadState{CheckedAt: 123}
	for i := range 256 {
		state.Add(store.ReadIssue{NoteID: int64(i + 1), Path: fmt.Sprintf("note/%d.md", i+1), Reason: "unavailable"})
	}
	b := newBootstrap(context.Background(), nil, nil, nil)
	b.cfg, b.server = cfg, &Server{active: &vaultView{cfg: cfg, progress: readProgress{verified: true, state: state}}}
	b.generation, b.cfgGeneration, b.serverGeneration = 1, 1, 1
	w := httptest.NewRecorder()
	b.progressResponse(w, httptest.NewRequest("GET", "/api/notes", nil), "fallback")
	var response struct {
		State store.ReadState `json:"read_state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.State.Issues) != 256 || response.State.Omitted != 1 || !response.State.Partial {
		t.Fatalf("unbounded diagnostics: %s", w.Body.String())
	}
}
