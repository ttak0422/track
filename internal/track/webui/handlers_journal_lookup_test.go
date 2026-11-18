package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func seedLookupJournal(t *testing.T, v *vaultView, id int64, title, body string) {
	t.Helper()
	path := v.cfg.PathForKind("journal", id)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(v.cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
		t.Fatal(err)
	}
	n, err := note.ReadFileContext(context.Background(), path, v.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.store.UpsertNote(n); err != nil {
		t.Fatal(err)
	}
}
func lookupRequest(s *Server, method, title, vault string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(method, "http://localhost/api/journal/lookup?title="+title+"&vault="+vault, nil))
	return w
}
func TestJournalLookupScopeAndNoWrites(t *testing.T) {
	workspace, _, main, work := twoVaultWorkspace(t)
	if w := lookupRequest(workspace, "GET", "20240229", ""); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if w := lookupRequest(workspace, "GET", "20240229", "work"); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if _, ok := workspace.cachedView("work"); ok {
		t.Fatal("lookup initialized foreign index")
	}
	foreign, err := workspace.viewByName("work")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []*vaultView{workspace.active, foreign} {
		markActivityReady(t, v)
		seedLookupJournal(t, v, 901, "20240229", "body"+v.label)
		seedLookupJournal(t, v, 902, "202402", "month")
		if err := v.store.UpsertNote(&note.Note{ID: 20240229, Kind: "note", Meta: note.Metadata{Title: "20240229"}}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, root := range []string{main, work, filepath.Dir(filepath.Dir(workspace.cfg.DBPath))} {
			if err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				info, e := d.Info()
				if e != nil {
					return e
				}
				value := info.ModTime().String()
				if !d.IsDir() {
					raw, e := os.ReadFile(p)
					if e != nil {
						return e
					}
					if strings.HasSuffix(p, ".db-shm") {
						value = "existing SQLite reader marks"
					} else {
						value += string(raw)
					}
				}
				out[p] = value
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	before := snapshot()
	for _, tc := range []struct{ vault, label string }{{"", ""}, {"main", ""}, {"work", "work"}} {
		for _, title := range []string{"20240229", "202402", "202403"} {
			w := lookupRequest(workspace, "GET", title, tc.vault)
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			var got struct {
				Vault, Title string
				Found        bool
				Note         *store.JournalLookupNote
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Vault != tc.label || got.Title != title || got.Found != (title != "202403") {
				t.Fatal(got)
			}
			if got.Found && (got.Note == nil || got.Note.FileKind != "journal" || got.Note.Title != title) {
				t.Fatal(got)
			}
			if title == "20240229" && got.Note.NoteID != 901 {
				t.Fatal("numeric ordinary note shadowed journal", got)
			}
			if !got.Found && got.Note != nil {
				t.Fatal(got)
			}
		}
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("lookup changed canonical or index data/files")
	}
	for _, method := range []string{"POST", "HEAD", "PUT"} {
		if w := lookupRequest(workspace, method, "202402", ""); w.Code != 405 {
			t.Fatal(w.Code)
		}
	}
	if w := lookupRequest(workspace, "GET", "202402", "missing"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	for _, title := range []string{"20230229", "202413", "id:202402", "202402%20"} {
		if w := lookupRequest(workspace, "GET", title, ""); w.Code != 400 {
			t.Fatal(title, w.Code)
		}
	}
	for _, headers := range []map[string]string{{"Origin": "https://evil.example"}, {"Host": "evil.example"}} {
		req := httptest.NewRequest("GET", "http://localhost/api/journal/lookup?title=202402", nil)
		for k, v := range headers {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		w := httptest.NewRecorder()
		workspace.Handler().ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "http://localhost/api/journal/lookup?title=202402", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	workspace.Handler().ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestJournalLookupRejectsStaleOrUnsafeSource(t *testing.T) {
	for _, kind := range []string{"rename", "delete", "kind-change", "nonregular", "fifo", "symlink", "journal-symlink", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			workspace, _, _, _ := twoVaultWorkspace(t)
			v := workspace.active
			markActivityReady(t, v)
			seedLookupJournal(t, v, 901, "20240229", "journal body")
			path := v.cfg.PathForKind("journal", 901)
			switch kind {
			case "rename":
				if err := note.WriteMetadata(v.cfg.MetadataPath(901), note.Metadata{Title: "renamed"}); err != nil {
					t.Fatal(err)
				}
			case "delete":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "kind-change":
				if err := os.Rename(path, v.cfg.NotePath(901)); err != nil {
					t.Fatal(err)
				}
			case "nonregular", "fifo", "symlink", "journal-symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "nonregular" {
					err = os.Mkdir(path, 0755)
				} else if kind == "fifo" {
					err = syscall.Mkfifo(path, 0600)
				} else {
					target := filepath.Join(t.TempDir(), "linked.md")
					if kind == "journal-symlink" {
						target = v.cfg.PathForKind("journal", 902)
					}
					if err = os.WriteFile(target, []byte("linked body"), 0600); err == nil {
						err = os.Symlink(target, path)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				seedLookupJournal(t, v, 903, "20240229", "other journal")
			}
			w := lookupRequest(workspace, "GET", "20240229", "")
			want := 503
			if kind == "journal-symlink" {
				want = 200
			}
			if kind == "duplicate" {
				want = 409
			}
			if w.Code != want {
				t.Fatalf("%d want%d %s", w.Code, want, w.Body.String())
			}
		})
	}
}
func TestJournalLookupNoFallback(t *testing.T) {
	boundary := newReadBoundary()
	target := "http://localhost/api/journal/lookup?title=202402"
	if w := boundaryRequest(boundary, target, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"found":false,"note":null}`)) }); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := boundaryRequest(boundary, target, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if len(boundary.cache) != 0 {
		t.Fatal("lookup cached")
	}
}
