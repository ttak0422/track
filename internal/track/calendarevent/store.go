package calendarevent

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

type record struct {
	Event
	OperationID string `json:"last_operation_id"`
	RequestHash string `json:"last_request_hash"`
}

func requestHash(req WriteRequest) string {
	raw, _ := json.Marshal(req)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func syncDir(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Open each directory relative to an anchored vault root. Reject links even within the vault.
func eventRoot(cfg *config.Config, create bool) (*os.Root, error) {
	root, err := os.OpenRoot(cfg.VaultDir)
	if err != nil {
		return nil, err
	}
	for _, part := range []string{"calendar", "events"} {
		info, err := root.Lstat(part)
		if errors.Is(err, os.ErrNotExist) && create {
			err = root.Mkdir(part, 0o755)
			if err != nil && !errors.Is(err, os.ErrExist) {
				root.Close()
				return nil, err
			}
			info, err = root.Lstat(part)
		}
		if err != nil {
			root.Close()
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, fmt.Errorf("calendar %s must be a real directory", part)
		}
		if create {
			if err := syncDir(root); err != nil {
				root.Close()
				return nil, err
			}
		}
		next, err := root.OpenRoot(part)
		root.Close()
		if err != nil {
			return nil, err
		}
		root = next
	}
	return root, nil
}

func read(root *os.Root, id string) (record, error) {
	var r record
	// Check the opened inode without waiting for a FIFO's writer. Lstat before
	// opening would leave a replacement race between the check and the open.
	file, err := root.OpenFile(id+".json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return r, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRecordBytes {
		return r, fmt.Errorf("%w: %s is not a bounded regular file", ErrCorrupt, id)
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxRecordBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return r, fmt.Errorf("%w: %s: %v", ErrCorrupt, id, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return r, fmt.Errorf("%w: %s has trailing JSON", ErrCorrupt, id)
	}
	fields, err := Normalize(r.Fields)
	_, timeErr := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil || fields != r.Fields || r.ID != id || r.Version != Version || r.Revision <= 0 ||
		!validID(r.OperationID) || len(r.RequestHash) != 64 || timeErr != nil {
		return r, fmt.Errorf("%w: %s has invalid identity, revision or fields", ErrCorrupt, id)
	}
	if _, err := hex.DecodeString(r.RequestHash); err != nil {
		return r, fmt.Errorf("%w: invalid request fingerprint", ErrCorrupt)
	}
	if r.RequestHash != requestHash(WriteRequest{Fields: r.Fields, ExpectedRevision: r.Revision - 1, OperationID: r.OperationID}) {
		return r, fmt.Errorf("%w: %s fields do not match the saved revision fingerprint", ErrCorrupt, id)
	}
	return r, nil
}

func Get(cfg *config.Config, id string) (Event, error) {
	if !validID(id) {
		return Event{}, invalid("id must be a canonical UUID")
	}
	root, err := eventRoot(cfg, false)
	if errors.Is(err, os.ErrNotExist) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, err
	}
	defer root.Close()
	r, err := read(root, id)
	return r.Event, err
}

func List(cfg *config.Config, includeDeleted bool) ([]Event, error) {
	result := []Event{}
	root, err := eventRoot(cfg, false)
	if errors.Is(err, os.ErrNotExist) {
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
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validID(id) {
			return nil, fmt.Errorf("%w: invalid event filename %q", ErrCorrupt, entry.Name())
		}
		r, err := read(root, id)
		if err != nil {
			return nil, err
		}
		if !r.Deleted || includeDeleted {
			result = append(result, r.Event)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Start != result[j].Start {
			return result[i].Start < result[j].Start
		}
		if result[i].Title != result[j].Title {
			return result[i].Title < result[j].Title
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func Save(cfg *config.Config, id string, req WriteRequest) (Event, error) {
	if !validID(id) || !validID(req.OperationID) {
		return Event{}, invalid("id and operation_id must be canonical UUIDs")
	}
	if req.ExpectedRevision < 0 {
		return Event{}, invalid("expected_revision must be non-negative")
	}
	fields, err := Normalize(req.Fields)
	if err != nil {
		return Event{}, err
	}
	req.Fields = fields
	rawRequest, _ := json.Marshal(req)
	if len(rawRequest) > MaxRequestBytes {
		return Event{}, invalid("event request exceeds %d bytes", MaxRequestBytes)
	}
	fingerprint := requestHash(req)
	root, err := eventRoot(cfg, true)
	if err != nil {
		return Event{}, err
	}
	defer root.Close()
	lock, err := root.OpenFile(".save.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return Event{}, err
	}
	defer lock.Close()
	if info, err := lock.Stat(); err != nil || !info.Mode().IsRegular() {
		return Event{}, errors.New("event lock is not a regular file")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Event{}, fmt.Errorf("lock event saves: %w", err)
	}
	current, readErr := read(root, id)
	if readErr != nil && !errors.Is(readErr, ErrNotFound) {
		return Event{}, readErr
	}
	if readErr == nil {
		if current.OperationID == req.OperationID {
			if current.RequestHash == fingerprint {
				if err := syncDir(root); err != nil {
					return Event{}, err
				}
				return current.Event, nil
			}
			return Event{}, fmt.Errorf("%w: operation_id was already used with different input", ErrConflict)
		}
		if current.Revision != req.ExpectedRevision {
			return Event{}, fmt.Errorf("%w: revision is %d; reload before saving", ErrConflict, current.Revision)
		}
		if current.Revision == math.MaxInt64 {
			return Event{}, fmt.Errorf("%w: revision exhausted", ErrConflict)
		}
		if current.Deleted || fields.Deleted {
			before, after := current.Fields, fields
			before.Deleted, after.Deleted = false, false
			if before != after || current.Deleted == fields.Deleted {
				return Event{}, fmt.Errorf("%w: deletion/restoration must preserve the saved event; restore before editing", ErrConflict)
			}
		}
	} else {
		if req.ExpectedRevision != 0 {
			return Event{}, ErrNotFound
		}
		if fields.Deleted {
			return Event{}, invalid("a new event cannot be deleted")
		}
	}
	r := record{Event: Event{Version: Version, ID: id, Revision: current.Revision + 1, Fields: fields, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, OperationID: req.OperationID, RequestHash: fingerprint}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return Event{}, err
	}
	raw = append(raw, '\n')
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return Event{}, err
	}
	name := ".pending-" + hex.EncodeToString(token[:])
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return Event{}, err
	}
	defer root.Remove(name)
	if _, err = io.Copy(file, bytes.NewReader(raw)); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Event{}, err
	}
	if err := root.Rename(name, id+".json"); err != nil {
		return Event{}, err
	}
	if err := syncDir(root); err != nil {
		return Event{}, err
	}
	return r.Event, nil
}
