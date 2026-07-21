# 0038. Block anchors and the "up" relation hierarchy

Status: Accepted

## Context

Links resolve to a note (ADR 0008) or a heading section (ADR 0009); transclusion (ADR 0031) embeds
the same targets. Two navigation needs remained: pointing at a single paragraph or list item, and
walking a deliberate tree over the flat link graph. Prior art: Obsidian's `^block-id` references,
and the `up`/`parent` note convention common in Obsidian vaults and org-mode hierarchies.

Typed properties (ADR 0032) already index `key:: value` fields with link-typed values, so a
hierarchy relation can be plain note data rather than a second metadata mechanism.

## Decision

- **Manual block anchors, engine-parsed.** A trailing `^id` (letter/digit, then letters, digits,
  `-`, `_`) on a content line marks the paragraph or list item it ends. `[[Note#^id]]` parses in
  the `link` package alongside heading anchors (`Ref.BlockID`); ids are never auto-generated,
  matching the explicit-link philosophy. First marker wins on duplicate ids, mirroring heading
  resolution.
- **Block extent is line-based and blank-line-bounded.** For a list item: the item line plus its
  more-indented continuation lines. Otherwise: the contiguous run of non-blank lines around the
  marker. This keeps `link.FindBlock` store-free and cheap, at the cost of requiring blank lines
  between a marked block and an unspaced neighbour.
- **Transclusion reuses the include machinery.** `![[Note#^id]]` extracts just the block (marker
  stripped) through `link.Extract`, with the same no-silent-fallback rule as heading includes.
- **Web navigation is hash-based.** The renderer strips the marker and anchors the block as
  `id="block-<id>"`; a block link resolves by its note key and carries the anchor as the URL hash;
  the reader scrolls to and highlights it. The static export inherits all of it, so deep links into
  published pages land on the block.
- **Hierarchy is one conventional property: `up`.** `up:: [[Parent]]` (or the sidecar equivalent)
  is read from the existing property index; only link-typed values count. The engine derives the
  ancestor trail (first parent wins, cycle-safe) and the children list — `store.Trail`/`ChildNotes`
  live, an equivalent walk in the static bundle — served with the note API, printed by `track nav`,
  and rendered as breadcrumbs plus a children section in the note view.
- **On a published directory site the parent is site config, not body text.** A page's place in the
  hierarchy is note-level metadata like its icon and tags, so it lives in the `site.yml` `pages` map
  (`up: <page>`, by file base name or page title — ADR 0049), and an inline `up::` field in a
  directory page stays a plain prose property with no special lifting (ADR 0032). An `up` resolving
  to no page in the set, or to the page itself, is a build error like every other silent no-op in
  that config.

## Consequences

- Fine-grained references stay portable Markdown: a marker is inert text to any other renderer.
- No new index tables: block anchors resolve from note text at use time; hierarchy queries ride the
  `props` table.
- A marked block moved across notes breaks its links, like a renamed heading; doctor-side checks
  can be added later if this bites.
