# ADR 0081: Readable editor paths over stable note IDs

## Status

Proposed (2026-10-24). Design only; no implementation or supported alias mode yet.
Acceptance requires the compatibility and data-safety spike below.

## Context

Neovim's ordinary LSP reference lists show filenames. Track's stable numeric filenames
therefore hide the subject of a result even though the index knows its title. Opening a
title-prefixed symbolic link is attractive: keep the authoritative files and existing LSP
workflow, but make paths readable without building another language server or picker.

The desired contract separates presentation from durable references: Neovim/Telescope
shows title-based editor paths, while links in a note body identify another Track note
by its stable ID, optionally displaying a title. Here "another note" includes same-vault
and cross-vault Track notes; it does not mean an external HTTP(S) URL.

Track already has a Go `track-lsp` (ADR 0007). This proposal extends its path boundary
and proposes ID-based link targets through the existing engine and standard LSP, not a
replacement language server. Symlinks alone are insufficient: today the
server returns canonical ID-file URIs, extracts IDs from incoming basenames, and keys
unsaved documents by the exact URI. A symlink can pass vault membership checks while
still failing ID parsing or bypassing an unsaved buffer under its other URI.

### Current implementation baseline

Inspected main at `70b11513f03d748e7736752b584c22931b59f492`:

- `internal/track/config/config.go`: `PathForKind` derives canonical paths; `KindFromPath`
  canonicalizes symlinks and requires a numeric filename. `DisplayPathForKind` preserves
  a symlinked vault prefix, not a readable note basename. Cache identity derives from the
  canonical vault path (ADRs 0061 and 0064).
- `internal/track/note/note.go`: `ParseFile` calls numeric `IDFromPath` before reading
  the body. Sidecar `.track/notes/<id>.yaml` supplies the title, not a Markdown H1
  (ADR 0013). `note/<id>.md` and `journal/<id>.md` remain authoritative.
- `internal/track/index/index.go`: top-level `note/` and `journal/` scans include matching
  extensions, then reject nonnumeric basenames. Title aliases in these directories
  would be skipped and warned about, not transparently become indexed notes.
- `internal/track/lsp/link_features.go`: references use backlink URIs derived through
  `notePath`; definitions, document links, anchors and outgoing links use those paths too.
  Custom backlink results already include a title, but standard `Location` does not.
- `internal/track/lsp/server.go`: `inVault` canonicalizes paths, but `docs` and
  `documentText` use raw URIs; `didSave` sends the supplied path to `Indexer.One`.
- `internal/track/lsp/rename.go` distinguishes open/closed backlinks by canonical URI:
  open ones receive workspace edits, closed ones are written on disk. Alias buffers
  must not be mistaken for closed files. CLI/meta rename lives separately in
  `internal/track/rename/rename.go` and preserves note IDs while rewriting title links.
- `internal/cli/note_commands.go`: `open` is create-or-open by title, returns JSON with
  the canonical `path`, and does not launch an editor. Many commands and parent-title
  lookup parse IDs from paths; changing one opener is not sufficient.
- `lua/track/util.lua` resolves a buffer's real path to identify its vault; `lsp.lua`
  starts one client per canonical vault. `watch.lua` polls agenda, but its save hook
  extracts an ID from the visible basename. Web `watch.go` watches canonical
  note/journal/data directories; it does not maintain an alias manifest or watch titles.
- Current body links are title-based, not ID-based: `link.Ref.Text` is a resolution key;
  the index/LSP keyword dictionary maps titles, and `vaultref.Resolve(vault, title)` calls
  `store.ResolveTerm`. A numeric `[[123]]` currently names the title `123`, not note ID
  123. Cross-vault `ext_links` stores `(vault name, title)` (ADR 0053). Existing IDs in
  graph rows, CLI `--id`, published slugs or file URIs do not establish body ID syntax.
- `internal/track/lsp/link_completion.go` inserts `kw.Term`; cross-vault completion
  inserts `vault:title`. `lua/track/telescope.lua` already renders title labels in its
  Track-specific pickers, but `path`/`filename` still use the returned canonical path.
- `internal/track/export/markdown.go` flattens wiki links, while `export/web.go` retains
  them for later rendering. Static `site/bundle.go` has title-based resolution and
  published slug mapping. ID targets require coordinated resolver/render/export work;
  retaining raw wiki syntax alone does not make the published site understand them.

## Decision

Explore an **opt-in, machine-local editor view outside every vault and sync root**,
with readable symlinks and a small shared identity/path adapter. Keep canonical paths
as the default until the spike passes. Do not rename master ID files, make editor aliases
authoritative, or change published slugs. Separately adopt ID-based targets for newly
generated inter-note links under an explicitly versioned/opt-in link contract. Preserve
existing title links during a staged migration; the desired ID target behavior is new,
not a description of current Track or a side effect of creating symlinks.

The first implementation target is **Neovim with Track's existing `track-lsp`**, on
explicitly tested local filesystems. There is no promise that an arbitrary Markdown LSP
preserves symlink URIs, follows them when scanning, or understands Track metadata.
No compatibility claim for Windows, OneDrive-backed targets, or other servers follows
from this ADR alone. Canonical editing remains the fallback everywhere.

### Identity, layout and naming

Identity is `(canonical vault identity, file kind, full note ID)`. Presentation is separate:

```text
<local-editor-view>/<vault-key>/note/<safe-title>--<full-id>.md
    -> <canonical-vault>/note/<full-id>.md
```

Use a machine-owned, nonsynced location, with owner-only access where supported. Do not
assume that an override of `TRACK_CACHE_DIR` is outside synchronization; validate or
require an explicit location. The view is reproducible, but must not be erased while
editors hold it open. A disposable cache lifetime is not a safe open-buffer lifetime.
Reuse the existing canonical vault/cache-key convention, and validate the manifest's
full vault path before reuse so a key collision cannot bind a different vault.

Keep Japanese and other readable Unicode rather than ASCII transliteration. The initial
implementation preserves supplied Unicode without an additional normalization dependency;
full-ID suffixes and actual filesystem collision checks handle equivalent spellings.
Replace separators, control/bidi formatting characters
and Windows-reserved punctuation; trim trailing dots/spaces, guard reserved device names,
and use `Untitled` if empty. Truncate only the title at a Unicode boundary, reserving the
full ID and extension under both byte/component and total-path budgets. Document the
exact algorithm/version before implementation; metadata titles are never rewritten.

Use the full numeric ID rather than a short title hash or shortened ID. The suffix is
unique within a vault/kind even when titles collide after sanitization, case folding,
normalization or truncation. Keep separate kind and vault directories. Check actual
filesystem collisions and manifest ownership; never overwrite an unexpected occupant.
The existing static-export `slug` metadata field is unrelated and must not be reused.

### Telescope and editor-visible paths

The actual editor filename is `<safe-title>--<full-id>.md`, not title alone. A picker
display formatter may hide the suffix and show the unmodified metadata title, but its
selected value, preview and open action must retain the alias path and stable identity.
Generic Telescope file pickers must enumerate the editor-view root (including file
symlinks under the selected finder's configuration), not the canonical vault. Track's
indexed pickers must receive/map `editor_path` consistently. LSP-backed pickers and
ordinary reference lists need alias URIs from `track-lsp`; changing Telescope's cwd
does not rewrite server results. Opening a title alias alone is not an end-to-end promise.

### Stable IDs in body links

New inter-note references must target an ID, never an editor alias filename or title.
Titles remain search/completion labels and optional visible link text. The logical target
is `(vault namespace, note ID)`; the existing per-vault ID index supplies the file kind.
This does not introduce kind-scoped duplicate IDs or globally unique IDs across vaults.

Proposed syntax for the compatibility spike, **not currently implemented**:

```markdown
[[id:1785024000000|Readable title]]
[[id:1785024000000##Design|Design section]]
[[id:1785024000000#^block-1|Relevant block]]
[[work:id:1785024000000|Other vault title]]
![[id:1785024000000|Embedded note]]
```

An unqualified ID resolves only in the current vault. A qualified ID resolves only in
the named registered vault; equal IDs in other vaults must not be fallback candidates.
Validate numeric range and canonical decimal spelling; report missing/unavailable targets
without silently treating an explicit ID as a title or creating a note. Vault names remain
the existing namespace mechanism: vault registry renaming/moving notes across vaults is
not made globally stable by this proposal and needs explicit reference maintenance.

Use the vault-owned configuration `link_format: id-v1` to opt into this grammar; absence
or `link_format: title` retains today's title-only interpretation. This is a proposed new
configuration key, not a currently available command. Never enable it automatically for
an existing vault. Source-vault format governs parsing; a referenced vault need not enable
ID syntax in its own bodies to be addressed by ID. `id:123` may already
be a literal title, and `id` may already be a registered vault name. Bare numeric title
links are also valid today. Do not add heuristic numeric lookup or silently give the new
prefix precedence over old content. Loading an opted-in vault must audit conflicting
ID-shaped titles and registry names, including changes arriving later through sync, and
refuse ambiguity with an actionable error. Initial support rejects those collisions
rather than guessing or silently rewriting them. Readers
without the new contract must not be told these examples already work.

The shared parser/resolver must distinguish legacy title targets from explicit ID targets
before anchor and label handling. Keep heading levels, block anchors, includes, self-link
rules and source ranges consistent across indexing, LSP, web and export. Heading/block
renames can still break their anchors; stable note IDs do not stabilize anchor text.
An explicit `|label` is authored text and is preserved on note-title rename; it may become
stale but must not retarget the link. Without a label, the renderer may resolve the current
title with an ID fallback. Completion should show/filter by title while inserting the ID
target and optional safely encoded label. The title's delimiter characters need a tested
encoding policy; do not concatenate arbitrary titles into wiki syntax.

Update all link-producing paths, not just LSP completion: picker insertion/copy-link,
create-note completion, web actions, templates and generated query/dashboard/journal or
other engine references. Create-by-title actions may retain their intent, but a persisted
reference produced after resolution/creation must use the returned ID. A cancelled or
failed creation must not leave a guessed ID. Title rename leaves ID target tokens intact;
legacy title-link rewriting remains compatibility behavior, distinct from ID navigation.
This ADR does not remove the current title-uniqueness checks as a side effect.

Cross-vault backlinks need a typed ID edge representation alongside legacy title edges;
the current `ext_links.title` column must not be overloaded with numeric strings. Plan
the rebuildable index schema change and source reindex together. The web/static resolver
must map permitted published note IDs to their existing published slugs, respect the
exported-note set, and avoid leaking local alias paths. Preserve Markdown export's existing
flattening contract unless separately changed; do not assume it already emits hyperlinks.
Ordinary external URLs, asset paths and non-Track Markdown links remain untouched.

Migration is explicit and reviewable, not performed on open/save/reindex or by this PR.
Support legacy title links first, then generate ID links in the opted-in contract. Any
conversion command must dry-run with a diff, preserve explicit labels, anchors and code
spans, pin source content versions, and convert only uniquely resolved targets in known
vaults. Preserve prior visible title text when replacing an unlabelled title target.
Ambiguous, unresolved, unavailable or grammar-conflicting links stay untouched with a
report; never select the current lowest-ID fallback as migration proof. Keep recoverable
originals and require explicit apply. Do not remove legacy read support or rewrite web
URLs as part of introducing editor aliases; any deprecation is a separate decision.

### Shared adapter and standard LSP behavior

Add one Go-owned conversion boundary rather than teaching every parser title filenames:

1. Resolve an approved view path through its manifest and symlink to the exact canonical
   note. Verify vault, kind and ID against the canonical layout. Never trust a numeric
   suffix alone, or grant arbitrary symlinks access to another vault.
2. Retain the original client URI for addressing its buffer; use stable identity for
   content, index access and open-document lookup. Canonicalization for identity must
   not accidentally canonicalize the URI displayed to the user.
3. In opt-in mode, emit one preferred URI per identity consistently for references,
   definitions, document links, diagnostics, workspace edits, code actions and navigation.
   For an open document its actual owning URI takes precedence; otherwise use the
   validated current alias. Keep the standard LSP methods and result structures.
4. Resolve `didSave` to the canonical path before indexing. Lookup unsaved content by
   identity before reading disk, including anchor and backlink lookups and rename edits.
   Track URI ownership and document versions; do not merge two independently edited
   buffers with last-notification-wins semantics.
5. Frontend open/jump paths must reuse an existing buffer for that identity. If a second
   canonical/alias URI is opened independently, refuse ambiguous mutations and report the
   conflict rather than writing either buffer behind the user's back. Reuse/diagnostic
   policy must also work across per-vault clients for cross-vault navigation.

Do not replace CLI `path` values globally. Preserve their canonical automation contract;
design an explicit editor-path request or opt-in `editor_path` result, with canonical
fallback and a clear reason on failure. All frontend `--path` consumers, metadata/title
helpers, save hooks, create-note actions and follow actions must pass through the adapter.
Keep `IDFromPath`, authoritative scans and database identity strict.

Third-party servers must never index both canonical and alias trees for the same notes.
An alias-only workspace is a possible experiment, not a guarantee: a server may ignore
links, resolve them to real paths, or interpret relative links differently. The Track
server continues indexing canonical files once and changes only its presentation boundary.
Track's vault-relative assets, includes and Babel/file references must retain their original
base; moving the visible parent directory must not silently change relative-path semantics.

### Save safety and lifecycle

Symlinks are references, not copies, but some save operations replace the reference itself.
Neovim's backup settings and external formatters must be tested, not assumed. A temp-file
plus rename over an alias can create a second body while leaving the master unchanged.
The supported editor mode must check before and after writes that the alias is still a
symlink to the expected canonical file. Unsafe save configurations must be rejected or
direct users to canonical editing. A post-save check detects damage but cannot by itself
make arbitrary external writers safe.

If an alias becomes a regular file, preserve it and report a recovery conflict; never
delete it during rebuild or automatically overwrite the canonical note. Do not implement
copy-back synchronization or use hardlinks as a substitute: atomic replacement also
breaks hardlink equivalence, and copy-back introduces a second source of truth.

For the bounded first implementation, select the view explicitly with machine-local
`TRACK_EDITOR_VIEW=<absolute nonsynced directory>`. Proposed `track editor-view` builds
it, and `track editor-path --path <path> [--check]` resolves/validates an editor path while
retaining canonical `path` in the result. These commands are not delivered by this ADR.

Keep generated alias names pinned indefinitely in the initial implementation, including
across editor restarts; no automatic retitling or cleanup is supported. This deliberately
means the filename can retain an old title after metadata rename. A metadata title
rename changes the canonical title/link graph immediately, but an open old alias remains
valid; show the current title separately if needed. Do not send `RenameFile` against the
master or blindly use an editor helper that resolves symlinks first. Create the next
preferred alias only in a future coordinated transition after affected buffers close.

Maintain a versioned manifest and serialized publication shared by all Track frontends.
If automatic cleanup/retitling is later introduced, require session leases; cleanup
runs only when no editor sessions own the view;
unknown/stale lease ownership requires explicit reconciliation, not a timeout-based guess.
Never garbage-collect an alias still referenced by an open buffer. Account for crashes,
multiple Neovim instances, external title changes, vault moves and missing sync targets.

Only remove stale entries which are manifest-owned symlinks whose target still matches
the recorded identity. Never recursively follow links during cleanup, delete unknown
files, or turn dangling aliases into new notes. Deletion/trash of a master must prevent an
old buffer from recreating it on save; report disappearance before writing. Regeneration
is idempotent and does not mutate note bodies, sidecars, trash or snapshots.

Treat titles and view entries as untrusted. Use path-component joins and proper file-URI
encoding, not shell interpolation or string concatenation. Reject traversal, escaping
targets, symlink loops, unapproved nested symlinks and manifest substitution. Revalidate
at mutation boundaries; document concurrent replacement/TOCTOU limits. A user-private
view is not a sandbox against another process running as the same user.

OneDrive does not support syncing symbolic links/junctions. Generate the view separately
on each machine; sync only the existing authoritative vault. Cloud-placeholder availability
and target replacement behavior remain explicit tests. Windows symlink permissions and
filesystem restrictions require their own support decision; do not request elevated
privileges or fall back to copying note bodies automatically.

## Alternatives

- **Aliases inside note/journal directories:** reject. They collide with strict scans,
  doctor expectations, syncing and other indexers. A separate in-vault subdirectory
  avoids today's flat scans but still ships machine-derived links and increases sync,
  snapshot and third-party indexing risks.
- **Title-aware quickfix/picker display only:** the cheapest fallback for the original
  reference-list problem. Keep canonical LSP URIs and decorate result labels from the
  existing title index or use existing title-bearing backlinks. This does not make buffer
  filenames readable everywhere, but avoids save and URI-identity hazards. It is a UI
  adapter over standard LSP, not a new language server. Prefer it if the alias spike
  needs a general filesystem virtualization or synchronization layer.
- **Rename canonical files to titles:** reject. It changes stable storage identity,
  adds title-renaming churn and defeats the constraint motivating this proposal.
- **Custom URI scheme, proxy LSP or virtual filesystem:** defer. These increase protocol,
  editor and filesystem integration cost beyond the intended small path adapter.

## Validation and rollout gates

No aliases are advertised as supported until a small disposable-vault spike proves:

1. Standard `vim.lsp.buf.references()` lists readable filenames and jumps to the correct
   range with Neovim + `track-lsp`; definitions, headings/block anchors, document links,
   hover, completion, diagnostics and rename workspace edits remain correct.
2. Alias-first and canonical-first opening, two buffers, two Neovim instances and unsaved
   backlink edits preserve one identity without stale disk reads, duplicate results or
   silent overwrite. Cross-vault equal IDs do not collide; missing aliases fall back safely.
3. Normal saves, backup/writebackup and backupcopy variants, external formatter atomic
   replacement, external canonical replacement, read-only files and crash recovery either
   preserve the master/link invariant or fail visibly without data loss. Check body bytes,
   symlink type/target, sidecar identity and reference results after every case.
4. Rename with open/dirty buffers, external title changes, delete/trash, reopen, stale
   leases and cleanup never orphan unsaved content or recreate a deleted master. Rebuild
   and export/snapshot/index operations never import generated aliases as notes.
5. Japanese/emoji, composed/decomposed Unicode, case-only names, duplicate sanitized
   titles, reserved names, long paths, `%`, `#`, spaces, traversal and hostile symlinks are
   covered. Relative assets/includes/Babel paths and watcher-triggered reindex still work.
6. Record exact Neovim, Track, OS/filesystem and save configuration versions. Run separate
   macOS and Linux checks before claiming them; Windows, network/sync filesystems and
   each third-party server stay unverified unless their own matrix passes.
7. In a separate ID-link fixture, title rename leaves ID targets and authored labels
   unchanged; numeric and `id:` legacy titles are not hijacked. Test equal IDs across
   vaults, unavailable namespaces, anchors/includes, completion/create insertion,
   backlinks, web/static published-slug mapping and Markdown flattening. Migration
   dry-runs preserve code/external URLs and refuse ambiguous or changed source content.

The first spike is deliberately smaller than the production requirements above: compare
a title-aware reference display adapter with a three-note alias URI/save fixture (one
target and two referencing notes). Do not build the lease framework or full security and
multi-instance machinery merely to run that experiment. First establish readable standard
reference results, unsaved-buffer identity and safe saves; then decide whether the lifecycle
cost is acceptable. The constraints above are release gates if aliases are pursued, not
an unconditional commitment to that architecture or implementation scope. The ID-link
contract is an independent engine change: prototype its grammar/resolver and migration
safety separately rather than bundling a whole link migration into the symlink spike.

If that decision is positive, implement in stages: identity/manifest tests first;
opt-in CLI/editor path resolution;
existing-server URI boundary and unsaved-buffer tests; Neovim integration and save guards;
then lifecycle/security/portability tests and English help. Existing test homes include
`internal/track/lsp/*_test.go`, `internal/track/config/config_test.go`,
`internal/track/index/*_test.go`, `internal/cli/*_test.go` and `scripts/e2e/nvim_backlinks.lua`.
Use repository Makefile/CI checks for the eventual implementation; this ADR does not
claim that those tests, the spike, or a platform compatibility matrix have been run.

## Consequences

Canonical storage, published URLs and the existing LSP protocol remain intact. Readable paths are
feasible as a bounded Track-owned editor feature, not as a zero-change symlink trick.
The cost is an identity adapter plus safe save/session lifecycle management. If those
costs exceed a small integration, choose title-aware reference display instead. ID-based
body targets additionally require coordinated engine, index, frontend and migration work;
they are not delivered by a filename change. No schema migration, runtime behavior or
help command is introduced by this documentation-only ADR.

## External evidence

- [LSP 3.17 Location](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#location): a location carries a URI and range, not a filename display title.
- [Neovim LSP utilities](https://github.com/neovim/neovim/blob/master/runtime/lua/vim/lsp/util.lua): `locations_to_items` derives quickfix filenames from returned URIs. Upstream main is moving; pin a version for the spike.
- [Neovim URI helpers](https://github.com/neovim/neovim/blob/master/runtime/lua/vim/uri.lua): buffer names feed file URI construction; this alone is not a buffer-identity guarantee.
- [Neovim backupcopy](https://neovim.io/doc/user/options/#'backupcopy'): copy/rename backup choices can preserve or break links; test actual configuration.
- [Marksman symlink issue 437](https://github.com/artempyanykh/marksman/issues/437): reported duplicate identity/update divergence illustrates why generic-server compatibility cannot be inferred.
- [OneDrive restrictions](https://support.microsoft.com/en-us/onedrive/restrictions-and-limitations-in-onedrive-and-sharepoint): symbolic links and junction points are not supported for synchronization.
