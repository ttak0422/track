# Roadmap Discussion TODO

This document tracks candidate features found by comparing track with obsidian.nvim and major Org Mode workflows.
Each item starts as undecided. Before implementation, decide whether track should support it, then record the intended design.

## Decision States

- `TBD`: needs discussion.
- `Adopt`: should be implemented.
- `Defer`: useful, but not soon.
- `Reject`: outside track's intended scope.
- `Done`: shipped; remaining follow-ups are noted inline.

## Rejected Scope (Decided)

The following directions are intentionally out of scope. The matching table rows are
marked `Reject`.

- Workspaces: no multi-workspace or dynamic-workspace concept. track uses a single
  explicit vault only.
- Obsidian app integration / sync / bookmarks: track does not aim for Obsidian
  compatibility, so there is no app linking, sync, or `.obsidian/bookmarks.json` support.
- Frontmatter / property compatibility: track does not adopt YAML frontmatter, so there
  is nothing to reconcile against Obsidian/Org property models. Metadata stays in
  sidecar files.
- Note IDs: managed by a single fixed unique rule, not user-configurable slugs or
  policies.
- Placement: files live in fixed kind directories (`note/`, `journal/`, `template/`);
  paths are derived from kind plus id, not stored in the cache.

## Recently Shipped

Items below have landed since this roadmap was drafted. The matching table rows are
marked `Done` with any remaining follow-up called out.

- Scoped search picker with preview (Telescope): `search_title` and `search_body`
  extensions backed by `track search --scope title|body`. Title search lists just the
  title; body search lists the matched line and positions the previewer and cursor on
  it. Remaining: create-on-empty, optional ripgrep-backed body search.
- Babel LSP completions: block-header argument and multi-token `:results` completion
  served over LSP (trigger characters `[`, `:`, space), plus running a block directly
  from the editor buffer using the unsaved buffer body.

## Discussion Template

For each item, answer:

- Need: what user workflow does this unlock?
- Scope: CLI, Go engine, LSP, Neovim frontend, or docs only?
- Storage/index impact: does it change sidecar metadata or SQLite schema?
- UX: command, LSP request/action, completion source, quickfix, picker, or virtual text?
- Compatibility: does it follow Obsidian, Org, Markdown, or track-native semantics?
- First slice: smallest useful implementation.

## Obsidian.nvim Parity Candidates

| Area | Candidate | State | Notes / likely implementation |
| --- | --- | --- | --- |
| Workspace | Multiple workspaces and workspace-specific overrides | Reject | Decided out of scope. Single explicit `TRACK_VAULT` only; no multi-workspace support. |
| Workspace | Dynamic workspace for markdown outside a vault | Reject | Decided out of scope. No workspace concept beyond the single explicit vault. |
| Picker UX | Quick switch note picker | TBD | Engine already has notes/search primitives. Neovim can use quickfix first, picker adapters later. |
| Picker UX | Dailies picker | TBD | CLI has journal open by offset, but no list/range command. Add store/query or filesystem scan. |
| Picker UX | Search picker with preview and create-on-empty | Done | Shipped: Telescope `search_title`/`search_body` pickers with file preview; body search jumps to the matched line via `search --scope`. Remaining: create-on-empty, optional ripgrep-backed body search. |
| Obsidian app | Open note in Obsidian app / advanced URI | Reject | Decided out of scope. track does not target Obsidian compatibility, so no app integration. |
| Sync | Obsidian Sync / obsidian-headless integration | Reject | Decided out of scope. No app sync/integration. |
| Bookmarks | Read `.obsidian/bookmarks.json` | Reject | Decided out of scope. No bookmarks feature. |
| Metadata | YAML frontmatter/properties compatibility | Reject | Decided out of scope. track does not adopt frontmatter, so there is no property-compatibility concern. |
| Metadata | Property editing UI/code action | TBD | Sidecar-native property editor may be useful even without frontmatter. |
| Tags | Inline `#tag` parsing and indexing | TBD | Current tags are sidecar-only. Requires parser/index changes and completion/rendering decisions. |
| Tags | Tag completion and tag references | TBD | Depends on tag indexing. Could use LSP completion after `#` and `textDocument/references`. |
| Note creation | Configurable note IDs/slugs | Reject | Decided out of scope. Note IDs are managed by a single fixed unique rule, not user-configurable. |
| Note creation | Unique note command and unique link insertion | Reject | Decided out of scope. IDs follow one unique rule by default; no configurable uniqueness policy. |
| Note creation | Typed file directories | Done | Files live under fixed kind directories (`note/`, `journal/`, `template/`) so path = kind + id. |
| Templates | Insert template into current note | TBD | Neovim command can read template files; engine support needed for substitutions if shared. |
| Templates | Create note from template | TBD | Extend `new` / LSP create-note command with template selection or name. |
| Templates | Template substitutions | TBD | Track-native variables: id, title, date, time, path. Custom Lua substitutions are Neovim-specific. |
| Links | Markdown link support `[label](target.md)` | TBD | Parser/LSP/index work. Decide whether track remains wiki-link-only. |
| Links | Heading/block links | Done | Shipped heading anchors: `[[note#foo]]`/`[[note##bar]]` where the `#` count is the Markdown heading level; first matching heading wins (ADR 0009). Definition jumps to the heading and completion offers headings after `#`. Block-level anchors (Obsidian `#^`) remain out of scope. |
| Links | URI and attachment links | TBD | Decide whether to delegate to `gx`/`vim.ui.open` or integrate into LSP definition. |
| Links | Configurable link style/format | TBD | Needed if supporting path-based links or Obsidian compatibility. |
| Refactor | Rename note and update links | TBD | High-value. Implement via LSP rename + workspace edits; engine must rewrite note bodies safely. |
| Refactor | React to file rename and update references | TBD | LSP workspace file-operation support or Neovim autocmd integration. |
| Visual actions | Link selection to existing note | TBD | LSP code action or Neovim command; needs text edit only. |
| Visual actions | Create note from selection and link it | TBD | Similar to current unresolved-link create, but range-based. |
| Visual actions | Extract selection to new note | TBD | Needs source edit and new note creation in one operation. |
| Checkboxes | Toggle/cycle checkbox state | TBD | Markdown-only editor action; likely Neovim-first. |
| Checkboxes | Create checkbox from plain list/paragraph | TBD | Pair with toggle action if adopted. |
| Navigation | Current-note links list | TBD | Similar to backlinks, but outgoing occurrences. LSP custom request or document symbols. |
| Navigation | Table of contents / document symbols | TBD | Implement `textDocument/documentSymbol` for Markdown headings. |
| Navigation | Workspace symbols | TBD | Implement `workspace/symbol` over note titles, aliases, maybe headings. |
| Attachments | Paste image from clipboard | TBD | Neovim-only command plus attachment storage policy. |
| Attachments | Attachment file management/opening | TBD | Requires attachment path policy and link/open behavior. |
| Status | Footer/statusline data | TBD | Backlink count, word count, metadata count. Could expose Lua helper and no UI opinion. |
| Health | `:checkhealth track` | TBD | Useful low-cost Neovim diagnostic for binaries, vault, index, LSP. |
| Help | In-plugin help/search | TBD | Lower priority. README/docs may be enough for now. |
| Smart action | Context-aware `<CR>` action | TBD | Current `<CR>` follows links only. Decide whether to include checkboxes/tags/headings. |
| LSP | Hover | TBD | Could show target note title/path/backlink count or unresolved create hint. |
| LSP | Diagnostics | TBD | Unresolved links, duplicate aliases/titles, stale metadata. |
| LSP | Code action resolve | TBD | Only needed if actions become expensive to compute. |
| LSP | Document highlight | TBD | Highlight same link target or references in current buffer. |
| LSP | Folding range | TBD | Mostly Markdown heading support; may defer to Treesitter. |

## Org Mode Parity Candidates

| Area | Candidate | State | Notes / likely implementation |
| --- | --- | --- | --- |
| Structure | Heading hierarchy and structure editing | TBD | Track is Markdown-first; could add document symbols and heading navigation without full Org editing. |
| Structure | Visibility cycling / sparse tree | TBD | Likely Neovim UI feature; overlaps with folding and search. |
| TODO | TODO/DONE workflow | TBD | Requires syntax decision: Markdown task states, sidecar state, or Org-style keywords. |
| TODO | Priorities and progress cookies | TBD | Depends on TODO/task model. |
| Agenda | Agenda views | TBD | Large feature. Requires scheduled/deadline parsing and query UI. |
| Dates | Scheduled/deadline timestamps | TBD | Decide Markdown-compatible timestamp syntax and index schema. |
| Capture | Capture templates | TBD | Could be template-backed note/section append commands. |
| Refile | Refile/move subtree or section | TBD | Requires structural parser and edit operations. |
| Archive | Archive completed items/sections | TBD | Depends on TODO and structure model. |
| Clocking | Clock in/out and clock reports | TBD | Separate time-tracking domain. Likely defer unless requested. |
| Properties | Property drawers / inheritance | TBD | Sidecar metadata is current property model; inheritance would be new. |
| Columns | Column view | TBD | Depends on properties/TODO. Likely defer. |
| Tables | Org table editing | TBD | Could rely on external Markdown table plugins instead. |
| Tables | Spreadsheet formulas | TBD | Large feature; likely reject/defer. |
| Export | HTML/PDF/LaTeX/etc. export | TBD | Track currently has no exporter. Decide whether external tools cover this. |
| Publish | Publishing projects | TBD | Depends on export. Likely defer. |
| Markup | Footnotes/citations/macros/includes | TBD | Mostly parser/export concerns; decide if track should understand them. |
| Links | Rich Org link types and custom IDs | TBD | Some overlap with Obsidian link expansion. |
| Babel | Inline source and inline calls | TBD | Deferred in `spec/babel.md`. |
| Babel | Named block calls and dependency graph | TBD | Needed for literate workflows beyond single-block execution. |
| Babel | Sessions | TBD | Requires long-lived interpreter lifecycle and cleanup policy. |
| Babel | Noweb expansion | TBD | Requires named block registry and expansion phase. |
| Babel | Tangling | TBD | Requires safe output path policy and write permissions. |
| Babel | Typed table/list/value results | TBD | Requires result coercion and rendering model. |
| Babel | File/graphics results | TBD | Depends on attachment/artifact storage policy. |
| Babel | Export integration | TBD | Depends on exporter. |

## Suggested Discussion Order

1. Refactor/navigation essentials: rename links, outgoing links list, document/workspace symbols, diagnostics.
2. Metadata and tags: sidecar editing UI, inline tag decision.
3. Creation workflows: templates, visual extract/link.
4. Attachments and images.
5. Org-like task/agenda features.
6. Larger compatibility areas: export/publish, full Babel.
