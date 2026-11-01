package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

const readResponseBudget = time.Second
const responseByteLimit = 24 << 20

type responseSnapshot struct {
	body    []byte
	header  http.Header
	checked int64
}
type readBoundary struct {
	mu         sync.Mutex
	active     map[string]int
	cache      map[string]responseSnapshot
	order      []string
	bytes      int
	generation uint64
}

func newReadBoundary() *readBoundary {
	return &readBoundary{active: map[string]int{}, cache: map[string]responseSnapshot{}}
}
func (b *readBoundary) admit(owner string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active[owner] >= 2 {
		return false
	}
	total := 0
	for _, n := range b.active {
		total += n
	}
	if total >= 16 {
		return false
	}
	b.active[owner]++
	return true
}
func (b *readBoundary) release(owner string) {
	b.mu.Lock()
	b.active[owner]--
	if b.active[owner] == 0 {
		delete(b.active, owner)
	}
	b.mu.Unlock()
}
func (b *readBoundary) remember(key string, r *limitedResponse) {
	if r.Code != 200 || r.err != nil {
		return
	}
	var document map[string]any
	if decodeDocument(r.Body.Bytes(), &document) != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.cache[key]; ok {
		b.bytes -= len(old.body)
	} else {
		b.order = append(b.order, key)
	}
	body := append([]byte(nil), r.Body.Bytes()...)
	b.cache[key] = responseSnapshot{body: body, header: r.Header().Clone(), checked: time.Now().UnixMilli()}
	b.bytes += len(body)
	for (b.bytes > 32<<20 || len(b.order) > 128) && len(b.order) > 0 {
		k := b.order[0]
		b.order = b.order[1:]
		b.bytes -= len(b.cache[k].body)
		delete(b.cache, k)
	}
}
func (b *readBoundary) fallback(w http.ResponseWriter, key, reason string) bool {
	b.mu.Lock()
	old, ok := b.cache[key]
	b.mu.Unlock()
	if !ok {
		return false
	}
	var document map[string]any
	if decodeDocument(old.body, &document) != nil {
		return false
	}
	mark := map[string]any{"partial": true, "cache_used": true, "checked_at": old.checked, "issues": []any{map[string]any{"path": "", "reason": reason, "cached": true}}}
	document["read_state"] = mark
	if note, ok := document["note"].(map[string]any); ok {
		note["read_only"] = true
	}
	if _, ok := document["results"]; ok {
		document["partial"] = []any{map[string]any{"name": "", "read_state": mark}}
	}
	writeJSON(w, document)
	return true
}

type limitedResponse struct {
	*httptest.ResponseRecorder
	err error
}

func (r *limitedResponse) Write(data []byte) (int, error) {
	if r.Body.Len()+len(data) > responseByteLimit {
		r.err = errors.New("response byte budget exceeded")
		return 0, r.err
	}
	return r.ResponseRecorder.Write(data)
}
func (b *readBoundary) serve(w http.ResponseWriter, r *http.Request, next http.Handler, fallback func(http.ResponseWriter, string) bool) {
	b.mu.Lock()
	generation := b.generation
	b.mu.Unlock()
	if len(r.URL.RequestURI()) > 8192 {
		writeError(w, errors.New("read request exceeds URL budget"), http.StatusRequestURITooLong)
		return
	}
	key := fmt.Sprintf("%d:%s", generation, r.URL.RequestURI())
	cacheable := r.Method == http.MethodGet
	owner := r.URL.Query().Get("vault")
	if strings.HasPrefix(r.URL.Path, "/api/asset") || r.URL.Path == "/api/ogp" {
		owner += "/media"
	}
	fail := func(reason string) {
		if (cacheable && b.fallback(w, key, reason)) || fallback(w, reason) {
			return
		}
		writeError(w, errors.New(reason), http.StatusServiceUnavailable)
	}
	if !b.admit(owner) {
		fail("read capacity is unavailable; previously verified information may be stale")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readResponseBudget)
	defer cancel()
	result := make(chan *limitedResponse, 1)
	go func() {
		defer b.release(owner)
		response := &limitedResponse{ResponseRecorder: httptest.NewRecorder()}
		defer func() {
			if recovered := recover(); recovered != nil {
				response.err = fmt.Errorf("read handler failed: %v", recovered)
			}
			result <- response
		}()
		next.ServeHTTP(response, r.WithContext(ctx))
	}()
	select {
	case response := <-result:
		b.mu.Lock()
		current := generation == b.generation
		b.mu.Unlock()
		if !current {
			writeError(w, errors.New("storage generation changed; retry this read"), 503)
			return
		}
		if response.err != nil {
			fail(response.err.Error())
			return
		}
		if cacheable && response.Code >= 500 {
			if b.fallback(w, key, "storage read failed; showing the last verified response") {
				return
			}
		}
		if cacheable {
			b.remember(key, response)
		}
		for name, values := range response.Header() {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	case <-ctx.Done():
		fail("read deadline reached; storage has not been fully verified")
	}
}

func decodeDocument(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(out)
}
func (b *readBoundary) advance(generation uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.generation = generation
	b.cache = map[string]responseSnapshot{}
	b.order = nil
	b.bytes = 0
}
