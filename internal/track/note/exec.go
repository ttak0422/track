package note

import (
	"fmt"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/dispatch"
)

// AppendExecTransition appends one dispatch record to a note's sidecar execution log and persists
// the metadata — the execution counterpart of the task log append (appendTaskTransition). The log
// is append-only: a dispatch's state change (pending → dispatched → completed | failed) appends a
// new record, and an existing record is never rewritten. The latest record for a dispatch id is
// that dispatch's current state.
//
// The sidecar is the authoritative home of execution state (docs/spec/storage.md): the record
// lives under .track/notes/<id>.yaml, is backed up like note bodies and sidecars, and is never
// reconstructed from the note body. The SQLite index is a rebuildable cache and stores no part of
// it; an index rebuild reads the sidecar and leaves the log untouched.
//
// rec must already carry the dispatch id (empty is rejected — a record without one cannot be keyed)
// and the target note id, which must equal id so a record can never land in the wrong note's
// sidecar. Status must be one of the closed dispatch status set; like the flags closed set (ADR
// 0074) an unknown value is rejected at write time, so the sidecar stays a parseable contract. At
// is stamped from now; the caller supplies the state timestamps (dispatched_at, completed_at,
// last_heartbeat_at) the transition carries.
func AppendExecTransition(cfg *config.Config, id int64, rec dispatch.Record, now time.Time) (dispatch.Record, error) {
	if rec.ID == "" {
		return dispatch.Record{}, fmt.Errorf("dispatch record has no id")
	}
	if rec.Note != id {
		return dispatch.Record{}, fmt.Errorf("dispatch %q targets note %d, not note %d", rec.ID, rec.Note, id)
	}
	if !dispatch.ValidStatus(rec.Status) {
		names := make([]string, len(dispatch.Statuses()))
		for i, s := range dispatch.Statuses() {
			names[i] = string(s)
		}
		return dispatch.Record{}, fmt.Errorf("invalid dispatch status %q (want one of: %s)", rec.Status, strings.Join(names, ", "))
	}
	metaPath := cfg.MetadataPath(id)
	meta, found, err := ReadMetadata(metaPath)
	if err != nil {
		return dispatch.Record{}, fmt.Errorf("read metadata: %w", err)
	}
	if !found {
		meta = Metadata{Created: now.Format(cfg.DateFormat)}
	}
	rec.At = now.Format(time.RFC3339)
	meta.ExecLog = append(meta.ExecLog, rec)
	if err := WriteMetadata(metaPath, meta); err != nil {
		return dispatch.Record{}, fmt.Errorf("write metadata: %w", err)
	}
	return rec, nil
}
