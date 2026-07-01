# Agent Workflow Specification

This document is a tool-neutral guide for agents that use track through the CLI.

## CLI Contract

All commands except `version` print one compact JSON object on stdout. Failures print `{"error":"..."}` and exit 1. Agents should parse JSON instead of scraping human text.

The vault comes from the platform user config file (`config.yml`, with `vault_dir`), defaulting to `$HOME/track` when unset (ADR 0015). Typical config locations are `~/.config/track/config.yml` on XDG-style systems and `~/Library/Application Support/track/config.yml` on macOS. Environment overrides such as `TRACK_VAULT` and `TRACK_CACHE_DIR` are for tests and one-off commands. The SQLite index is a rebuildable cache; authoritative per-note metadata lives under `.track/notes/` and must be backed up with note bodies.

## Title Model

The sidecar `metadata.title` is authoritative. A note body's H1 headings are ordinary Markdown content. Creating a note writes the title to the sidecar, and renaming a note must use `track rename` or LSP rename so backlinks and rename history are updated.

Do not infer a note title from the body. Do not edit a body H1 to rename a note.

## Target Selection

Use titles for user-facing workflows and ids/paths for exact targets:

- `track new --title X`: strict create; fails if `X` already exists.
- `track open --title X`: create-or-open; safe to repeat.
- `track append (--id N | --title X | --path P)`: add body text and/or tags to an existing note.
- `track update (--id N | --title X | --path P)`: replace body text and/or update tags on an existing note. Use `--clear-tags` before `--tag` when the existing tag set should be replaced.
- `track toggle (--id N | --title X | --path P) --line N`: flip a single task checkbox by line number, leaving surrounding text untouched. `--state check|uncheck` forces a result idempotently. Prefer this over hand-editing `- [ ]`/`- [x]` lines.
- `track asset import <file>`: copy a local file into the vault's single `assets/` directory and return the `assets/<file>` reference to embed (`![alt](assets/<file>)`). `track asset dir [--ensure]` reports the assets directory.
- `track rename (--id N | --title X | --path P) --to Y`: change the sidecar title and rewrite backlinks.
- `track backlinks` and `track graph`: inspect incoming links and local graph around a target.
- `track agenda [--date YYYY-MM-DD]`: list the notes created or updated on a calendar day (default today), for "what was worked on that day" lookups. Activity days are recorded per note in the sidecar and cover both CLI mutations and direct editor edits.

Creating or editing a note also ensures that day's journal exists (it is the day's aggregation hub); the journal itself is excluded from activity. An explicit `track journal --body/--template` therefore only applies before the day's first note edit — afterward the journal already exists, so add to it with `track append --id <yyyyMMdd>`.

## Links

Use explicit wiki links:

- `[[title]]`: link to the note titled `title`.
- `[[title|display]]`: link to `title` while displaying `display`.
- `[[note#heading]]`, `[[note##heading]]`: link to the first matching H1/H2 heading in `note`.

Use `track resolve --term X` to check whether a title exists before relying on a link.

## Common Workflows

Create a note from complete Markdown, preserving a leading H1:

```sh
cat article.md | track new --title "Article Title"
```

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

`track doctor --fix` repairs that divergence by auto-numbered restore, then reindexes: it writes a fresh `Untitled N` sidecar for a `missing_sidecar`, recreates an empty markdown for an `orphan_sidecar`, keeps the lowest id and renumbers the rest for a `duplicate_title`, and imports a `stray_file` as a new note with a fresh id and title. An `unreadable_sidecar` is reported under `skipped` rather than guessed at. The response carries `changed`, `fixed`, and `skipped`.
