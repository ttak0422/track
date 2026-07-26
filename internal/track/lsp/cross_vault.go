package lsp

import (
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/vaultref"
	protocol "typefox.dev/lsp"
)

// crossVaultState classifies how a [[vault:title]] reference resolved. Ordinary keys (no
// registered-vault prefix) are notQualified and follow the local resolution path.
type crossVaultState int

const (
	notQualified crossVaultState = iota
	crossResolved
	crossMissing     // the vault was reachable but holds no such title
	crossUnavailable // the vault itself could not be consulted (unmounted, no index)
)

// resolveQualified resolves a possible [[vault:title]] key through the registry gate. It must run
// BEFORE the local keyword dictionary everywhere, mirroring the indexer: a registered name always
// reads as a qualifier, even when a local title happens to carry the same prefix (doctor lints
// those as shadowed). detail carries the error text for crossUnavailable.
func (s *Server) resolveQualified(key string) (res vaultref.Resolved, vault string, detail string, state crossVaultState) {
	vault, title, ok := link.SplitVaultRef(key, s.xv.IsVault)
	if !ok {
		return vaultref.Resolved{}, "", "", notQualified
	}
	resolved, found, err := s.xv.Resolve(vault, title)
	if err != nil {
		return vaultref.Resolved{}, vault, err.Error(), crossUnavailable
	}
	if !found {
		return vaultref.Resolved{}, vault, "", crossMissing
	}
	return resolved, vault, "", crossResolved
}

// splitVaultPrefix splits an in-progress completion target "name:partial" on the registry gate.
// Unlike link.SplitVaultRef it accepts an empty remainder, so "[[work:" already opts into the work
// vault's dictionary.
func splitVaultPrefix(target string, isVault func(string) bool) (vault, partial string, ok bool) {
	i := strings.IndexByte(target, ':')
	if i <= 0 || !isVault(target[:i]) {
		return "", "", false
	}
	return target[:i], strings.TrimSpace(target[i+1:]), true
}

// crossVaultCompletion offers the target vault's titles as full "vault:title" insertions — the
// opt-in dictionary: other vaults' titles never appear until the user types a registered prefix.
// An unavailable vault contributes nothing; diagnostics report it on the written reference.
func (s *Server) crossVaultCompletion(ctx openLinkContext, vault string) []completionItem {
	kws, err := s.xv.Keywords(vault)
	if err != nil {
		return []completionItem{}
	}
	items := make([]completionItem, 0, len(kws))
	for _, kw := range kws {
		target := vault + ":" + kw.Term
		items = append(items, completionItem{
			Label:      target,
			Kind:       protocol.ReferenceCompletion,
			Detail:     "vault " + vault,
			InsertText: target,
			FilterText: target,
			TextEdit:   completionTextEdit(ctx, target),
		})
	}
	return items
}
