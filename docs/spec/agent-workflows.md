# Agent Workflow Specification

This document is a tool-neutral guide for agents that use track through the CLI.

## CLI Contract

All commands except `version` print one compact JSON object on stdout. Failures print `{"error":"..."}` and exit 1. Agents should parse JSON instead of scraping human text.

The vault is required in the platform user config file (`config.yml`, with `vault_dir`). Typical locations are `~/.config/track/config.yml` on XDG-style systems and `~/Library/Application Support/track/config.yml` on macOS. Environment overrides such as `TRACK_VAULT` and `TRACK_CACHE_DIR` are for tests and one-off commands. The SQLite index is a rebuildable cache; authoritative per-note metadata lives under `.track/notes/` and must be backed up with note bodies.

## Title Model

The sidecar `metadata.title` is authoritative. A note body's H1 headings are ordinary Markdown content. Creating a note writes the title to the sidecar, and renaming a note must use `track rename` or LSP rename so backlinks and rename history are updated.

Do not infer a note title from the body. Do not edit a body H1 to rename a note.

## Target Selection

Use titles for user-facing workflows and ids/paths for exact targets:

- `track new --title X`: strict create; fails if `X` already exists.
- `track open --title X`: create-or-open; safe to repeat.
- `track append (--id N | --title X | --path P)`: add body text and/or tags to an existing note.
- `track rename (--id N | --title X | --path P) --to Y`: change the sidecar title and rewrite backlinks.
- `track backlinks` and `track graph`: inspect incoming links and local graph around a target.

## Links

Use explicit wiki links:

- `[[title]]`: link to the note titled `title`.
- `[[title|display]]`: link to `title` while displaying `display`.
- `[[note#heading]]`, `[[note##heading]]`: link to the first matching H1/H2 heading in `note`.

Use `track resolve --term X` to check whether a title exists before relying on a link.

## Common Workflows

Create an AI-generated note from complete Markdown, preserving a leading H1:

```sh
cat article.md | track new --title "Article Title" --ai
```

Use `--ai` for initial AI-generated drafts. It adds the reserved `generated-by-ai` tag; it is provenance, not a quality marker.

Search by text or tag:

```sh
track search --query "distributed systems"
track search --query "#zettel"
```

Read a full portable Markdown rendering:

```sh
track export --id 1781314534000
```

Typical creation loop:

1. `track open --title X` for an idempotent target.
2. Write body text containing explicit `[[...]]` links.
3. `track resolve --term LinkedTitle` to verify important links.
4. `track search --query X` to rediscover notes.
5. `track rename --title Old --to New` when a title changes.

## Maintenance

`track reindex --full` rebuilds the cache index from the on-disk notes and sidecars; it reconciles deletions silently.

`track doctor` is a read-only health check that never changes files. It reports vault/sidecar divergence — the kind a cloud sync (e.g. OneDrive) can introduce — as a JSON `issues` array, with `ok: true` when clean. Issue kinds: `missing_sidecar`, `orphan_sidecar`, `stray_file` (e.g. a conflict copy that breaks the `<id>.md` naming rule), `unreadable_sidecar`, and `duplicate_title`. Finding issues is not an error, so it still exits 0; only real failures use the `{"error":...}`/exit 1 contract. Run it before a `reindex --full` if you suspect a partially synced vault, so an orphan sidecar is not mistaken for a delete.
