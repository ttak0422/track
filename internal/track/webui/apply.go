package webui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/request"
)

// writeFileAtomic writes a note file via a same-directory temp file and rename, so a crash mid-apply
// never leaves a torn note body: the target either has the old content or the new one. The temp file
// is hidden (dot prefix) so a directory scan of the note directory never treats it as a note. It
// mirrors the request store's atomic write of the request file — the two durable writes of an update
// apply use the same replacement discipline.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename moved it
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// applyUpdate applies one update request's proposed body to its target note. It runs inside the
// vault write lock (v.write) — the same lock a normal web save (putNote) and every other vault write
// uses — so the target note is re-read and compared against the send-time ETag right before the
// replacement, and an update apply and a web save can never interleave: for a given ETag exactly one
// winner replaces the file and every other candidate settles to conflict.
//
// The durable ordering is the one the request spec fixes:
//
//   - the result transition already persisted the applying state (with the proposed body) before this
//     function runs, so a crash before the replacement is recovered by comparing the note against the
//     proposal and the send-time original;
//   - the note file is replaced atomically (temp+rename) while the request still reads as applying;
//   - the completed state — the before/after body and ETag plus the change rationale — is persisted
//     after the replacement, again atomically, so a crash between the two writes is recovered by
//     reading the note's current body;
//   - the index refresh and the SSE notification go through the existing paths (index.New(...).One
//     under the same lock, s.events.broadcastChange), exactly like a normal save.
//
// Outcomes, reported through the settled request rather than an error when they are normal state
// transitions:
//
//   - the target changed or vanished since the request was sent: conflict — nothing was applied, the
//     recorded answer and proposed body are kept, and the reason is preserved;
//   - the target cannot be read or the replacement fails: failed, so the user can retry;
//   - success: completed with the before/after record; an index refresh failure is logged and
//     self-heals on the next read refresh, because the durable files already agree.
//
// A transition persist failure (e.g. a full requests directory) leaves the request applying and is
// returned as an error — startup recovery resolves it by the same rules.
func (s *Server) applyUpdate(v *vaultView, st *request.Store, id string, now time.Time) (request.Request, error) {
	var settled request.Request
	err := v.write(func() error {
		req, err := st.Get(id)
		if err != nil {
			return err
		}
		if req.Status != request.StatusApplying {
			// A concurrent path settled it (a replay raced in, or recovery ran first): nothing to apply.
			settled = req
			return nil
		}
		target := req.UpdateTarget
		if target == nil || req.Intent != request.IntentUpdate || req.Result == nil || req.Result.ProposedBody == "" {
			tr, cerr := st.Conflict(id, "update request is missing its target or proposed body", now)
			settled = tr.Request
			return cerr
		}
		// Re-read the note in this vault, never trusting the client's stored body: the vault view is
		// the routing boundary, so a same-numbered note in another vault can never be touched.
		path := v.cfg.PathForKind(target.FileKind, target.NoteID)
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				tr, cerr := st.Conflict(id,
					fmt.Sprintf("update target note %d no longer exists", target.NoteID), now)
				settled = tr.Request
				return cerr
			}
			tr, ferr := st.FailApply(id, fmt.Sprintf("read update target: %v", err), now)
			settled = tr.Request
			return ferr
		}
		currentETag := note.ContentETag(raw)
		if currentETag != target.ETag {
			// The note changed since the request was sent (a web save, another update, an external
			// edit). Never auto-merge and never force-overwrite: one winner already wrote it.
			tr, cerr := st.Conflict(id, "update target changed since the request was sent", now)
			settled = tr.Request
			return cerr
		}
		out := []byte(ensureTrailingNewline(req.Result.ProposedBody))
		if err := writeFileAtomic(path, out); err != nil {
			tr, ferr := st.FailApply(id, fmt.Sprintf("replace note: %v", err), now)
			settled = tr.Request
			return ferr
		}
		tr, cerr := st.CompleteApply(id, request.ApplyInput{
			BeforeBody: string(raw),
			BeforeETag: currentETag,
			AfterBody:  string(out),
			AfterETag:  note.ContentETag(out),
		}, now)
		settled = tr.Request
		if cerr != nil {
			return cerr
		}
		// The note and the request record already agree; the index is derived state and self-heals on
		// the next read refresh, so a failure here is reported but never un-settles the apply.
		if err := index.New(v.cfg, v.store).One(path); err != nil {
			fmt.Fprintf(os.Stderr, "track web: reindex after update apply failed: %v\n", err)
		}
		s.events.broadcastChange()
		return nil
	})
	if err != nil {
		return settled, err
	}
	return settled, nil
}

// recoverApplying resolves every update request a previous server left in the applying state (a
// crash between the durable steps of an apply). It runs once, on the first access of a vault's
// request store, and settles each request by re-reading the target note inside the same vault write
// lock the live apply uses — so recovery and any concurrent write obey the same one-winner rule.
func (s *Server) recoverApplying(v *vaultView, st *request.Store) {
	const page = 100
	var cursor string
	for {
		reqs, next, err := st.List(page, cursor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "track web: recover applying requests for vault %q: %v\n", v.name, err)
			return
		}
		for _, req := range reqs {
			if req.Status != request.StatusApplying {
				continue
			}
			if _, err := s.resolveApplying(v, st, req); err != nil {
				fmt.Fprintf(os.Stderr, "track web: recover request %s: %v\n", req.ID, err)
			}
		}
		if next == "" {
			return
		}
		cursor = next
	}
}

// resolveApplying settles one applying request from the note's current body, implementing the
// recovery rule of docs/spec/live-agent-requests.md:
//
//   - current body equals the proposal: a previous server replaced the note and died before
//     recording completion — settle as completed and refresh the index (no second write);
//   - current body equals the send-time original: the apply never replaced the note — resume the
//     ordinary lock-protected apply;
//   - anything else: the note moved to neither while the apply was interrupted — nothing was
//     overwritten, and the conflict keeps the answer and the proposal.
//
// A note that vanished resolves to conflict, and a vault that cannot be read is reported and left
// for the next recovery rather than guessed at.
func (s *Server) resolveApplying(v *vaultView, st *request.Store, req request.Request) (request.Request, error) {
	now := time.Now()
	target := req.UpdateTarget
	if target == nil || req.Intent != request.IntentUpdate || req.Result == nil {
		tr, err := st.Conflict(req.ID, "update request is missing its target or proposed body", now)
		return tr.Request, err
	}
	path := v.cfg.PathForKind(target.FileKind, target.NoteID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			tr, cerr := st.Conflict(req.ID, fmt.Sprintf("update target note %d no longer exists", target.NoteID), now)
			return tr.Request, cerr
		}
		return request.Request{}, fmt.Errorf("read update target: %w", err)
	}
	current := string(raw)
	proposed := ensureTrailingNewline(req.Result.ProposedBody)
	switch {
	case current == proposed:
		// Already applied; only the completion record and the index are missing. The before values
		// come from the send-time target, the after values from what the note actually holds.
		var settled request.Request
		err := v.write(func() error {
			tr, cerr := st.CompleteApply(req.ID, request.ApplyInput{
				BeforeBody: target.Body,
				BeforeETag: target.ETag,
				AfterBody:  current,
				AfterETag:  note.ContentETag(raw),
			}, now)
			settled = tr.Request
			if cerr != nil {
				return cerr
			}
			if err := index.New(v.cfg, v.store).One(path); err != nil {
				fmt.Fprintf(os.Stderr, "track web: reindex after apply recovery failed: %v\n", err)
			}
			return nil
		})
		return settled, err
	case current == target.Body:
		// The apply never reached the replacement: resume it through the live path.
		return s.applyUpdate(v, st, req.ID, now)
	default:
		// The note holds neither the original nor the proposal: an unrelated edit won the race. The
		// conflict preserves the answer, the proposal, and the reason; nothing was overwritten.
		tr, cerr := st.Conflict(req.ID, "update target changed while the request was interrupted", now)
		return tr.Request, cerr
	}
}
