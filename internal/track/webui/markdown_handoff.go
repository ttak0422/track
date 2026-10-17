package webui

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ttak0422/track/internal/track/markdownhandoff"
)

const markdownHandoffTTL = time.Minute
const markdownQueueBytes = 32 << 20

type markdownGrant struct {
	body    []byte
	expires time.Time
}
type markdownInbox struct {
	mu     sync.Mutex
	grants map[string]markdownGrant
}

// Every route is loopback-only and behind the workspace's Host/Origin/Fetch-Metadata guard.
// A snapshot contains bytes, not server-readable paths. No route lists pending capabilities.
func (s *Server) handleMarkdownHandoff(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]string{"protocol": markdownhandoff.Protocol})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, markdownhandoff.MaxWireBytes))
	if err != nil {
		writeError(w, fmt.Errorf("snapshot is too large"), http.StatusRequestEntityTooLarge)
		return
	}
	var snapshot markdownhandoff.Snapshot
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		writeError(w, fmt.Errorf("invalid Markdown snapshot"), http.StatusBadRequest)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeError(w, fmt.Errorf("invalid trailing snapshot data"), http.StatusBadRequest)
		return
	}
	if err := snapshot.Validate(); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(key)
	s.markdown.mu.Lock()
	defer s.markdown.mu.Unlock()
	s.markdown.prune(time.Now())
	queued := len(body)
	for _, grant := range s.markdown.grants {
		queued += len(grant.body)
	}
	if queued > markdownQueueBytes || len(s.markdown.grants) >= 8 {
		writeError(w, fmt.Errorf("Markdown handoff queue is full; retry shortly"), http.StatusServiceUnavailable)
		return
	}
	if s.markdown.grants == nil {
		s.markdown.grants = map[string]markdownGrant{}
	}
	s.markdown.grants[token] = markdownGrant{body: body, expires: time.Now().Add(markdownHandoffTTL)}
	// A fragment keeps the capability out of HTTP request URLs and Referer headers.
	writeJSON(w, map[string]string{"route": "/markdown#handoff=" + token})
}

func (inbox *markdownInbox) prune(now time.Time) {
	for token, grant := range inbox.grants {
		if !now.Before(grant.expires) {
			delete(inbox.grants, token)
		}
	}
}

func (s *Server) handleMarkdownConsume(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&input) != nil || len(input.Token) != 64 {
		writeError(w, fmt.Errorf("invalid handoff token"), http.StatusBadRequest)
		return
	}
	s.markdown.mu.Lock()
	s.markdown.prune(time.Now())
	grant, ok := s.markdown.grants[input.Token]
	delete(s.markdown.grants, input.Token)
	s.markdown.mu.Unlock()
	if !ok {
		writeError(w, fmt.Errorf("Markdown handoff expired or already opened; run :Track markdown again"), http.StatusGone)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(grant.body)
}
