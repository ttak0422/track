package timeline

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// Store opens only an existing vault. No API here creates a vault or touches notes.
type Store struct{ vault string }

func NewStore(vaultDir string) *Store { return &Store{vault: vaultDir} }

type document struct {
	SchemaVersion int     `json:"schema_version"`
	ProjectID     string  `json:"project_id"`
	Events        []Event `json:"events"`
}

func (s *Store) directory(create bool) (*os.Root, error) {
	vault, err := os.OpenRoot(s.vault)
	if err != nil {
		return nil, err
	}
	defer vault.Close()
	info, err := vault.Lstat(".track")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, invalid("vault metadata directory must not be a symlink")
	}
	if create {
		if err := vault.Mkdir(".track/timelines", 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	info, err = vault.Lstat(".track/timelines")
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, invalid("timeline directory must not be a symlink")
	}
	return vault.OpenRoot(".track/timelines")
}

func load(root *os.Root, id string) (Snapshot, error) {
	out := empty(id)
	f, err := safeOpen(root, id+".json", os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return out, err
	}
	if !st.Mode().IsRegular() {
		return out, invalid("timeline file must be regular")
	}
	if st.Size() > MaxDocumentBytes {
		return out, ErrLimit
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	if err != nil {
		return out, err
	}
	if len(raw) > MaxDocumentBytes {
		return out, ErrLimit
	}
	var doc document
	if err := decode(raw, &doc); err != nil {
		return out, err
	}
	if doc.SchemaVersion != SchemaVersion || doc.ProjectID != id || len(doc.Events) == 0 {
		return out, invalid("timeline schema, identity, or empty journal")
	}
	if len(doc.Events) > MaxEvents {
		return out, ErrLimit
	}
	seen := map[string]bool{}
	for _, event := range doc.Events {
		key := event.Source + "\x00" + event.EventID
		if seen[key] {
			return out, invalid("duplicate persisted event identity")
		}
		seen[key] = true
		if err := reduce(&out, event); err != nil {
			// A present journal with an invalid event is corruption, never absence.
			// Do not preserve ErrNotFound from reducer preconditions: Apply uses
			// that sentinel exclusively to initialize a missing file.
			return empty(id), invalid("timeline event %d: %v", event.Sequence, err)
		}
	}
	return out, nil
}

func (s *Store) Read(ctx context.Context, id string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if !validID(id) {
		return Snapshot{}, invalid("project id")
	}
	root, err := s.directory(false)
	if err != nil {
		return Snapshot{}, err
	}
	defer root.Close()
	out, err := load(root, id)
	if err == nil {
		err = ctx.Err()
	}
	return out, err
}

func (s *Store) List(ctx context.Context) ([]Summary, error) {
	result := []Summary{}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := s.directory(false)
	if errors.Is(err, ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	// Bound directory enumeration as well as documents; lock/temp files are included.
	entries, err := dir.ReadDir(4001)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 4000 {
		return nil, ErrLimit
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validID(id) {
			return nil, invalid("unexpected timeline filename")
		}
		snapshot, err := load(root, id)
		if err != nil {
			return nil, err
		}
		result = append(result, Summary{ProjectID: id, Title: snapshot.Title, NoteID: snapshot.NoteID, Revision: snapshot.Revision, Progress: snapshot.Progress})
	}
	return result, nil
}

// Apply serializes cross-process writers. An acknowledged retry never appends a
// second event, even if its expected_revision is now stale. Same identity with
// different content is an error rather than silently overriding recorded work.
func (s *Store) Apply(ctx context.Context, id string, request Request) (Snapshot, error) {
	if !validID(id) || !validID(request.Source) || !validID(request.EventID) {
		return Snapshot{}, invalid("project/source/event id")
	}
	payload, err := canonical(request.Payload)
	if err != nil {
		return Snapshot{}, err
	}
	request.Payload = payload
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	root, err := s.directory(true)
	if err != nil {
		return Snapshot{}, err
	}
	defer root.Close()
	lock, err := safeOpen(root, "."+id+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return Snapshot{}, err
	}
	defer lock.Close()
	st, err := lock.Stat()
	if err != nil {
		return Snapshot{}, err
	}
	if !st.Mode().IsRegular() {
		return Snapshot{}, invalid("lock must be a regular file")
	}
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := acquire(lockCtx, lock); err != nil {
		return Snapshot{}, err
	}
	defer release(lock)
	snapshot, err := load(root, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Snapshot{}, err
	}
	if errors.Is(err, ErrNotFound) {
		snapshot = empty(id)
	}
	for _, event := range snapshot.Events {
		if event.Source == request.Source && event.EventID == request.EventID {
			prior, err := canonical(event.Payload)
			if err != nil {
				return Snapshot{}, err
			}
			if event.Kind != request.Kind || !bytes.Equal(prior, payload) {
				return Snapshot{}, ErrDuplicate
			}
			return snapshot, nil
		}
	}
	if snapshot.Revision != request.ExpectedRevision {
		return Snapshot{}, ErrConflict
	}
	if len(snapshot.Events) >= MaxEvents {
		return Snapshot{}, ErrLimit
	}
	event := Event{Sequence: snapshot.Revision + 1, At: time.Now().UTC().Format(time.RFC3339Nano), Source: request.Source, EventID: request.EventID, Kind: request.Kind, Payload: payload}
	if event.Source == IntervalProgressSource {
		if _, ok := intervalProgress(event); !ok {
			return Snapshot{}, invalid("interval progress requires a task discussion with matching progress-{result,blocker,next}-ID and printable one-line text (1–512 bytes)")
		}
	}

	if err := reduce(&snapshot, event); err != nil {
		return Snapshot{}, err
	}
	raw, err := json.Marshal(document{SchemaVersion: SchemaVersion, ProjectID: id, Events: snapshot.Events})
	if err != nil {
		return Snapshot{}, err
	}
	if len(raw) > MaxDocumentBytes {
		return Snapshot{}, ErrLimit
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := save(root, id, raw); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func save(root *os.Root, id string, raw []byte) error {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temp := "." + id + "-" + hex.EncodeToString(random[:]) + ".tmp"
	f, err := safeOpen(root, temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = root.Rename(temp, id+".json"); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
