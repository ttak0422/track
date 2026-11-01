package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/ttak0422/track/internal/track/note"
	"os"
	"time"
)

// ReadIssue identifies data that was not verified by the current read. Cached
// means its previous index entry was retained; it does not certify freshness.
type ReadIssue struct {
	NoteID    int64  `json:"note_id,omitempty"`
	Path      string `json:"path"`
	Reason    string `json:"reason"`
	Cached    bool   `json:"cached,omitempty"`
	BodyMtime int64  `json:"body_mtime,omitempty"`
	MetaMtime int64  `json:"meta_mtime,omitempty"`
}

// ReadState travels with partial responses and survives subsequent CLI processes.
// CheckedAt is a check time, not a claim that retained rows form a fresh snapshot.
type ReadState struct {
	Partial   bool        `json:"partial"`
	CacheUsed bool        `json:"cache_used"`
	CheckedAt int64       `json:"checked_at"`
	Issues    []ReadIssue `json:"issues"`
}

func (r *ReadState) Add(issue ReadIssue) {
	r.Partial = true
	r.CacheUsed = r.CacheUsed || issue.Cached
	for _, old := range r.Issues {
		if old == issue {
			return
		}
	}
	r.Issues = append(r.Issues, issue)
}

func (s *Store) ReadState() (ReadState, error) {
	var raw string
	err := s.runner().QueryRow(`SELECT value FROM index_state WHERE key='read_state'`).Scan(&raw)
	if isNoRows(err) {
		return ReadState{}, nil
	}
	if err != nil {
		return ReadState{}, err
	}
	var state ReadState
	err = json.Unmarshal([]byte(raw), &state)
	return state, err
}

func (s *Store) SetReadState(state ReadState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.runner().Exec(`INSERT INTO index_state(key,value) VALUES('read_state',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw))
	return err
}

// AddReadIssue records a query-time failure without losing prior refresh issues.
func (s *Store) AddReadIssue(issue ReadIssue) error {
	return s.Atomic(func(tx *Store) error {
		state, err := tx.ReadState()
		if err != nil {
			return err
		}
		before := len(state.Issues)
		state.Add(issue)
		if len(state.Issues) != before || state.CheckedAt == 0 {
			state.CheckedAt = time.Now().UnixMilli()
		}
		return tx.SetReadState(state)
	})
}

type runner interface {
	Exec(string, ...any) (sql.Result, error)
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func (s *Store) runner() runner {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}

func (s *Store) atomic(fn func(*sql.Tx) error) error {
	if s.tx != nil {
		return fn(s.tx)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Atomic publishes parsed rows, their links, and the read status together. Parsing
// and filesystem I/O must happen before this callback, outside the DB transaction.
func (s *Store) Atomic(fn func(*Store) error) error {
	return s.atomic(func(tx *sql.Tx) error { return fn(&Store{db: s.db, tx: tx}) })
}

// ReadMetadata refuses an absent previously indexed sidecar. New files that have
// genuinely never had metadata can still use the normal creation path.
func (s *Store) ReadMetadata(id int64, path string) (note.Metadata, bool, error) {
	var expected int64
	err := s.runner().QueryRow(`SELECT meta_mtime FROM notes WHERE id=?`, id).Scan(&expected)
	if err != nil && !isNoRows(err) {
		return note.Metadata{}, false, err
	}
	meta, found, err := note.ReadMetadata(path)
	if err != nil {
		return meta, found, err
	}
	if expected != 0 && !found {
		return meta, false, fmt.Errorf("known metadata unavailable: %s", path)
	}
	return meta, found, nil
}

func (s *Store) RequireMetadata(id int64, path string) error {
	_, _, err := s.ReadMetadata(id, path)
	return err
}

// FileReadIssue keeps retry observations identical for indexing and query paths.
func FileReadIssue(id int64, path, metaPath, reason string, cached bool) ReadIssue {
	stamp := func(p string) int64 {
		info, err := os.Stat(p)
		if err != nil {
			return 0
		}
		return info.ModTime().Unix()
	}
	return ReadIssue{NoteID: id, Path: path, Reason: reason, Cached: cached, BodyMtime: stamp(path), MetaMtime: stamp(metaPath)}
}
