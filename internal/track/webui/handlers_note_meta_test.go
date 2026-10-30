package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func TestNoteMetaRejectedRequestContract(t *testing.T) {
	for _, tc := range []struct {
		name, method, query, body, message string
		status                             int
	}{
		{"id before method", http.MethodPut, "", "", "id is required", http.StatusBadRequest},
		{"invalid id before method", http.MethodPut, "?id=bad", "", "invalid id:", http.StatusBadRequest},
		{"lookup before method", http.MethodPut, "?id=999", "", "note 999 is not indexed", http.StatusNotFound},
		{"lookup before JSON", http.MethodPost, "?id=999", "{", "note 999 is not indexed", http.StatusNotFound},
		{"zero id lookup", http.MethodPut, "?id=0", "", "note 0 is not indexed", http.StatusNotFound},
		{"unsupported method", http.MethodPut, "?id=100", "", "method PUT not allowed", http.StatusMethodNotAllowed},
		{"malformed JSON", http.MethodPost, "?id=100", "{", "decode request:", http.StatusBadRequest},
		{"revision before props", http.MethodPost, "?id=100", `{"etag":"stale","props":"["}`, "metadata changed since it was loaded; reload before saving", http.StatusConflict},
		{"props validation", http.MethodPost, "?id=100", `{"props":"- list"}`, "parse props:", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, cfg := putNoteSetup(t, 100, "Alpha", "Body.\n")
			before, err := os.ReadFile(cfg.MetadataPath(100))
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(tc.method, server.URL+"/api/note/meta"+tc.query, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			message, _ := result["error"].(string)
			if resp.StatusCode != tc.status || !strings.HasPrefix(message, tc.message) {
				t.Fatalf("status=%d body=%v; want %d %q", resp.StatusCode, result, tc.status, tc.message)
			}
			after, err := os.ReadFile(cfg.MetadataPath(100))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("rejected request changed metadata: %v", err)
			}
			body, err := os.ReadFile(cfg.NotePath(100))
			if err != nil || string(body) != "Body.\n" {
				t.Fatalf("rejected request changed note body: %q %v", body, err)
			}
		})
	}
}

func TestNoteMetaUsesAddressedVault(t *testing.T) {
	server, main, work := twoVaultServer(t)
	mainCfg, workCfg := &config.Config{VaultDir: main}, &config.Config{VaultDir: work}
	before, err := os.ReadFile(mainCfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	seed := getVaultJSON(t, server.URL+"/api/note/meta?id=100&vault=work")
	if seed["title"] != "Alpha in work" {
		t.Fatalf("metadata read selected wrong vault: %v", seed)
	}
	payload, err := json.Marshal(map[string]any{"etag": seed["etag"], "description": "Updated work"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/note/meta?id=100&vault=work", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata write status: %d", resp.StatusCode)
	}
	saved := getVaultJSON(t, server.URL+"/api/note/meta?id=100&vault=work")
	if saved["title"] != "Alpha in work" || saved["description"] != "Updated work" || saved["etag"] == seed["etag"] {
		t.Fatalf("metadata save did not update the addressed vault: %v", saved)
	}
	after, err := os.ReadFile(mainCfg.MetadataPath(100))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("foreign metadata save changed launch vault: %v", err)
	}
	body, err := os.ReadFile(workCfg.NotePath(100))
	if err != nil || string(body) != "# Alpha in work\n" {
		t.Fatalf("metadata save changed note body: %q %v", body, err)
	}
	resp, err = http.Get(server.URL + "/api/note/meta?vault=unknown&id=bad")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rejected map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rejected); err != nil {
		t.Fatal(err)
	}
	message, _ := rejected["error"].(string)
	if resp.StatusCode != http.StatusBadRequest || message != `vault "unknown" is not registered` {
		t.Fatalf("unknown vault must be rejected before the id: %d %v", resp.StatusCode, rejected)
	}
}
