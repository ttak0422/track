package request

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
)

const maxListLimit = 100

// ErrNotFound identifies an absent record or an invalid path selector.
var ErrNotFound = errors.New("request not found")

// Store is a read-only view of a vault's archived requests. It never creates a directory,
// changes a record, or recovers an interrupted update.
type Store struct{ dir string }

func New(cfg *config.Config) *Store { return &Store{dir: cfg.RequestsDir()} }

// Get returns one archived request.
func (s *Store) Get(id string) (Request, error) {
	return s.load(id)
}

// List returns requests newest first, up to limit (bounded by maxListLimit), with a cursor for the
// next page. The cursor is the id of the last returned request; the next call resumes after it. An
// empty result with an empty cursor means there is no next page. Listing reads the requests directory
// directly; the archive needs no separate index.
func (s *Store) List(limit int, cursor string) ([]Request, string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Request{}, "", nil
		}
		return nil, "", fmt.Errorf("list requests: %w", err)
	}
	requests := make([]Request, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue // request files only; temp files (hidden, .tmp) never surface here
		}
		r, err := s.load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			return nil, "", err
		}
		requests = append(requests, r)
	}
	// Newest timestamp first, then descending ID. IDs embed unix milliseconds;
	// requests within one millisecond are ordered by their random suffix.
	slices.SortFunc(requests, func(a, b Request) int {
		if a.CreatedAt != b.CreatedAt {
			return strings.Compare(b.CreatedAt, a.CreatedAt)
		}
		return strings.Compare(b.ID, a.ID)
	})
	if cursor != "" {
		start := -1
		for i, r := range requests {
			if r.ID == cursor {
				start = i + 1
				break
			}
		}
		if start == -1 {
			return []Request{}, "", nil // a cursor this vault does not know: no further page
		}
		requests = requests[start:]
	}
	next := ""
	if len(requests) > limit {
		next = requests[limit-1].ID
		requests = requests[:limit]
	}
	return requests, next, nil
}

// load reads, parses, and validates one request file. A missing file is ErrNotFound; a file
// that is not valid JSON or carries an unsupported version is refused rather than half-read.
func (s *Store) load(id string) (Request, error) {
	if strings.TrimSpace(id) == "" || id == "." || !filepath.IsLocal(id) || filepath.Base(id) != id || strings.ContainsAny(id, "\\\x00") {
		return Request{}, fmt.Errorf("%w: invalid request id", ErrNotFound)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Request{}, fmt.Errorf("%w: %q", ErrNotFound, id)
		}
		return Request{}, fmt.Errorf("read request %s: %w", id, err)
	}
	var r Request
	if err := json.Unmarshal(raw, &r); err != nil {
		return Request{}, fmt.Errorf("request %s is not valid JSON: %w", id, err)
	}
	if r.Version < 1 || r.Version > CurrentVersion {
		return Request{}, fmt.Errorf("request %s has unsupported version %d (this build reads 1..%d)",
			id, r.Version, CurrentVersion)
	}
	if !ValidStatus(r.Status) {
		return Request{}, fmt.Errorf("request %s has invalid status %q", id, r.Status)
	}
	for _, d := range r.Attempts {
		if !ValidDispatchStatus(d.Status) {
			return Request{}, fmt.Errorf("request %s has invalid attempt status %q", id, d.Status)
		}
	}
	return r, nil
}
