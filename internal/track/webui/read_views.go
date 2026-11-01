package webui

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
)

var peerOpenSlots = make(chan struct{}, 4)

// Unavailable peers initialize independently. Failed or still-running opens keep
// their admission slot; an HTTP read never waits for another vault's filesystem.
func (s *Server) kickView(name string) {
	path, known := s.cfg.Vaults[name]
	if !known {
		return
	}
	s.viewsMu.Lock()
	if s.opening == nil {
		s.opening = map[string]bool{}
		s.openRetry = map[string]time.Time{}
	}
	if s.closed || s.opening[name] || time.Now().Before(s.openRetry[name]) || len(s.opening) >= 4 {
		s.viewsMu.Unlock()
		return
	}
	select {
	case peerOpenSlots <- struct{}{}:
	default:
		s.viewsMu.Unlock()
		return
	}
	s.opening[name] = true
	s.viewsMu.Unlock()
	go func() {
		defer func() {
			<-peerOpenSlots
			s.viewsMu.Lock()
			delete(s.opening, name)
			s.openRetry[name] = time.Now().Add(30 * time.Second)
			s.viewsMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), index.StageBudget)
		defer cancel()
		cfg, err := config.LoadBounded(ctx, path, false)
		if err != nil {
			return
		}
		st, err := store.Open(cfg.DBPath)
		if err != nil {
			return
		}
		v := &vaultView{name: name, label: name, cfg: cfg, store: st, async: true}
		s.viewsMu.Lock()
		if s.closed {
			s.viewsMu.Unlock()
			st.Close()
			return
		}
		s.views[name] = v
		s.viewsMu.Unlock()
		s.refreshAsync(v)
		s.events.broadcastChange()
		// Configuration validation can fail independently; the provisional view is readable only.
		strictCtx, stop := context.WithTimeout(context.Background(), index.StageBudget)
		defer stop()
		verified, err := config.LoadBounded(strictCtx, path, true)
		candidate := *cfg
		candidate.Provisional = false
		if verified != nil {
			candidate.VaultName = verified.VaultName
		}
		if err == nil && verified != nil && reflect.DeepEqual(candidate, *verified) {
			v.mutationGate().Lock()
			defer v.mutationGate().Unlock()
			replacement := &vaultView{name: name, label: name, cfg: verified, store: st, async: true, sharedGate: v.mutationGate()}
			v.invalidateRead()
			s.viewsMu.Lock()
			if !s.closed {
				s.views[name] = replacement
			}
			s.viewsMu.Unlock()
			s.refreshAsync(replacement)
		}
	}()
}
func (s *Server) readViews() ([]*vaultView, []vaultInfo) {
	views := []*vaultView{}
	gaps := []vaultInfo{}
	if s.active.store != nil {
		views = append(views, s.active)
	} else {
		gaps = append(gaps, vaultInfo{Name: s.active.name, Error: "primary cache is initializing"})
	}
	for _, name := range sortedVaultNames(s.cfg.Vaults) {
		if name == s.active.name {
			continue
		}
		v, ok := s.cachedView(name)
		if !ok {
			s.kickView(name)
			gaps = append(gaps, vaultInfo{Name: name, Path: s.cfg.Vaults[name], Error: "vault initialization is pending or unavailable"})
			continue
		}
		views = append(views, v)
	}
	return views, gaps
}

var errViewPending = errors.New("vault initialization is pending; other vaults remain available")
