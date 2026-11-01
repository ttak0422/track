package webui

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/vaultfile"
)

type readProgress struct {
	mu         sync.Mutex
	generation uint64
	running    bool
	cancel     context.CancelFunc
	last       time.Time
	state      store.ReadState
	notes      map[int64]*note.Note
	versions   map[int64]*vaultfile.NoteRead
	bytes      int
	home       string
}

const maxProgressBytes = 32 << 20
const asyncRefreshInterval = 5 * time.Second

func (s *Server) refreshAsync(v *vaultView) {
	s.viewsMu.Lock()
	closed := s.closed
	s.viewsMu.Unlock()
	if closed {
		return
	}
	if !v.mutationGate().TryLock() {
		return
	}
	defer v.mutationGate().Unlock()
	p := &v.progress
	p.mu.Lock()
	interval := asyncRefreshInterval
	if p.state.Partial {
		interval = 30 * time.Second
	}
	if p.running || time.Since(p.last) < interval {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.last = time.Now()
	p.generation++
	generation := p.generation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	p.cancel = cancel
	p.state.Add(store.ReadIssue{Path: v.cfg.VaultDir, Reason: "read pass in progress"})
	p.notes = map[int64]*note.Note{}
	p.bytes = 0
	previous := make(map[int64]*vaultfile.NoteRead, len(p.versions))
	for id, version := range p.versions {
		previous[id] = version
	}
	p.mu.Unlock()
	go func() {
		defer cancel()
		ix := index.New(v.cfg, v.store).WithPrevious(previous)
		snap, err := ix.Stage(ctx, func(n *note.Note) {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.generation != generation || ctx.Err() != nil {
				return
			}
			if p.bytes+len(n.Body)+len(n.Snapshot.Metadata)+len(n.Tasks)*512 > maxProgressBytes || len(p.notes) >= 1024 {
				return
			}
			copy := *n
			copy.Meta.Days = append([]string(nil), n.Meta.Days...)
			p.notes[n.ID] = &copy
			if v.cfg.WebHome == n.Meta.Title || v.cfg.WebHome == strconv.FormatInt(n.ID, 10) {
				p.home = strconv.FormatInt(n.ID, 10)
			}
			p.bytes += len(n.Body) + len(n.Snapshot.Metadata) + len(n.Tasks)*512
			s.events.broadcastChange()
		})
		if err == nil && v.mutationGate().TryLock() {
			p.mu.Lock()
			valid := p.generation == generation
			p.mu.Unlock()
			s.viewsMu.Lock()
			valid = valid && !s.closed
			s.viewsMu.Unlock()
			if valid {
				commitCtx, cancelCommit := context.WithTimeout(context.Background(), index.StageBudget)
				err = ix.ApplyActivity(commitCtx, snap)
				cancelCommit()
				if err == nil {
					err = ix.Publish(snap)
				}
			} else {
				err = index.ErrSuperseded
			}
			v.mutationGate().Unlock()
		} else if err == nil {
			err = index.ErrSuperseded
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.generation != generation {
			return
		}
		p.running = false
		p.cancel = nil
		if err != nil {
			p.state.Add(store.ReadIssue{Path: v.cfg.VaultDir, Reason: err.Error()})
		} else {
			p.state = snap.State
			if p.versions == nil {
				p.versions = map[int64]*vaultfile.NoteRead{}
			}
			for _, n := range snap.Notes {
				if len(p.versions) < 100000 {
					p.versions[n.ID] = vaultfile.VersionOnly(n.Snapshot)
				}
			}
		}
		s.events.broadcastChange()
	}()
}
func (v *vaultView) invalidateRead() {
	p := &v.progress
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generation++
	if p.cancel != nil {
		p.cancel()
	}
	p.running = false
	p.cancel = nil
	p.notes = nil
	p.versions = nil
	p.bytes = 0
}
func (v *vaultView) asyncState() store.ReadState {
	p := &v.progress
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.state
	out.Issues = append([]store.ReadIssue(nil), out.Issues...)
	return out
}
func (s *Server) enableAsync() { s.asyncReads = true; s.active.async = true }
func (s *Server) asyncMutation(v *vaultView, w http.ResponseWriter, r *http.Request) bool {
	if !v.mutationGate().TryLock() {
		writeError(w, errors.New("a storage mutation is still pending; retry after it finishes"), http.StatusConflict)
		return false
	}
	s.viewsMu.Lock()
	closed := s.closed
	s.viewsMu.Unlock()
	if closed {
		v.mutationGate().Unlock()
		writeError(w, errors.New("storage generation is closed"), http.StatusConflict)
		return false
	}
	if v.cfg.Provisional {
		v.mutationGate().Unlock()
		writeError(w, errors.New("configuration is still being verified; workspace is read-only"), http.StatusConflict)
		return false
	}
	initialized, err := v.store.Initialized()
	state, stateErr := v.store.ReadState()
	if err != nil || stateErr != nil || !initialized || state.Partial {
		v.mutationGate().Unlock()
		writeError(w, errors.New("index is not fully verified; workspace is read-only"), http.StatusConflict)
		return false
	}
	v.invalidateRead()
	if err := v.store.InvalidateSnapshot(); err != nil {
		v.mutationGate().Unlock()
		writeError(w, err, http.StatusInternalServerError)
		return false
	}
	return true
}
