package webui

import (
	"os"

	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// resolveLink interprets a body reference in its source vault's syntax, then looks
// it up in the addressed vault. The target's mode must not reinterpret the key.
func (s *Server) resolveLink(source *vaultView, key string) (*vaultView, store.NoteRef, bool, error) {
	if err := source.cfg.ValidateLinkFormat(); err != nil {
		return nil, store.NoteRef{}, false, err
	}
	target := source
	if _, explicit := link.ExplicitID(key, source.cfg.LinkFormat); !explicit {
		if name, local, qualified := link.SplitVaultRef(key, func(name string) bool {
			_, ok := source.cfg.Vaults[name]
			return ok
		}); qualified {
			var err error
			target, err = s.viewByName(name)
			if err != nil {
				return nil, store.NoteRef{}, false, err
			}
			key = local
		}
	}
	if err := target.cfg.ValidateExplicitTarget(key, source.cfg.LinkFormat); err != nil {
		return nil, store.NoteRef{}, false, err
	}
	s.refresh(target)
	ref, found, err := target.store.ResolveLink(key, source.cfg.LinkFormat)
	if found {
		ref.Vault = target.label
		ref.Path = target.cfg.PathForKind(ref.FileKind, ref.NoteID)
	}
	return target, ref, found, err
}

// A cross-vault excerpt has its own asset/link/task scope. A pointer preserves an
// explicit empty launch-vault label when the host belongs to another vault.
type resolvedInclude struct {
	link.ResolvedInclude
	Vault *string `json:"vault,omitempty"`
}

func (s *Server) resolveIncludes(source *vaultView, body string) []resolvedInclude {
	targets := map[string]*vaultView{}
	includes := link.ResolveIncludes(body, func(key string) (int64, string, string, string, bool) {
		target, ref, found, err := s.resolveLink(source, key)
		if err != nil || !found {
			return 0, "", "", "", false
		}
		raw, err := os.ReadFile(target.cfg.PathForKind(ref.FileKind, ref.NoteID))
		if err != nil {
			return 0, "", "", "", false
		}
		body, _, _ := note.SplitLegacyFootmatter(string(raw))
		markdown, err := export.WebBody(body)
		if err != nil {
			return 0, "", "", "", false
		}
		targets[key] = target
		return ref.NoteID, ref.FileKind, markdown, note.ContentETag(raw), true
	})
	out := make([]resolvedInclude, 0, len(includes))
	for _, inc := range includes {
		res := resolvedInclude{ResolvedInclude: inc}
		if target := targets[inc.Title]; target != nil && target != source {
			label := target.label
			res.Vault = &label
		}
		out = append(out, res)
	}
	return out
}
