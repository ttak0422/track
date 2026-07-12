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
- `track capture [--target "<note>#<heading>"] [--template S] --body S`: append a (templated) entry under a heading anchor. `--target` defaults to the configured `capture_inbox` (created on first use); with `--template`, the captured text fills the template's `{{ title }}`. Prefer this over `track append` when the entry belongs under a specific heading.
- `track refile --from "<note>#<heading>" --to "<note>#<heading>" [--line N]`: move a heading subtree (nested headings included) to another anchor; `--line N` moves the single list item at line `N` of the source note instead. Text moves verbatim so `[[links]]` keep resolving; both notes reindex. An ambiguous anchor is refused, and the destination write is verified before the source is stripped.
- `track archive "<note>#<heading>"`: move a subtree into the configured `archive_note` (per-year via `{{year}}` by default), inserting `*Archived from [[Source]] on <date>.*` under its heading. A living move, distinct from `track rm` (whole-note soft delete).
- `track asset import <file>`: copy a local file into the vault's single `assets/` directory and return the `assets/<file>` reference to embed (`![alt](assets/<file>)`). `track asset dir [--ensure]` reports the assets directory.
- `track rename (--id N | --title X | --path P) --to Y`: change the sidecar title and rewrite backlinks.
- `track meta (--id N | --title X | --path P) [--description S] [--image assets/F] [--set key=value ...] [--unset key ...] [--edit (FILE|-)]`: print a note's metadata (including its typed properties under `props` and its editable YAML document under `doc`), or set it: the page summary (published as `og:description`), the cover image (`og:image`; must be an existing vault asset), and typed key-value properties (`--set`/`--unset`; a comma-separated value makes a list). An explicitly empty description/image clears that field. Property values are typed from their text — boolean, number, `YYYY-MM-DD` date, `[[link]]`, else string — and validated against the optional `properties:` schema in `config.yml`; schema violations also surface in `track doctor`.
- `track meta ... --edit (FILE|-)`: apply a full metadata document — the `doc` YAML with keys `title`, `tags`, `description`, `image`, `props` — from a file or stdin, validated as a whole before anything is written (a rejected document changes nothing). A changed title is a rename with `track rename` semantics: uniqueness against the index, backlink rewrite, rename history; an empty `title:` leaves the title unchanged. Empty fields render as bare `key:` lines (both bare and `[]`/`{}` forms parse). `--edit` cannot be combined with the field flags. This is the round-trip the editor frontends use: read `doc`, edit it, pipe it back.
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

Search by text or tag. Title text is matched by term: space-separated words are ANDed, an uppercase
`OR` separates alternatives (`a b OR c` is `(a AND b) OR c`), and a lowercase `and`/`or` stays a
literal term. `#tag` filters by tag and combines with the text (AND):

```sh
track search --query "distributed systems"     # titles containing both words
track search --query "graph OR chart"          # either word
track search --query "#zettel graph"           # tagged #zettel and titled …graph…
```

List notes without a query — `{"notes":[{"note_id":…,"title":…,"tags":…}, …]}`, newest first, journals
excluded. `--untagged` is the pull side of a tag-curation pass: it returns exactly the notes that still
carry no tags, so a sweep can fetch them and add tags with `track append --tag`:

```sh
track notes                                    # every note, newest first
track notes --untagged --limit 20              # the next 20 notes that need tags
track append --title "Draft" --tag zettel      # merge a tag into an existing note
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

## Generations and Deletion

`track gen` gives an agent run a non-destructive review boundary without git (ADR 0025). The model
is a git release: a generation is an immutable save point, the working vault is a disposable
working tree.

- `track gen increment [--label S]`: save the working vault as a generation. Returns
  `{"gen":N,"changed":true}`, or `changed: false` when nothing diverged from the cursor generation.
  Generations past the cursor are dropped (`dropped`), the oldest beyond `gen_keep` are pruned
  (`pruned`). `--label` tags the new generation (e.g. to mark a dream save point) and shows up in
  `gen list`; it is dropped when nothing changed and no generation is cut.
- `track gen undo` / `track gen redo`: move the cursor one generation back/forward and restore the
  vault to it, then rebuild the index (search stays consistent). `undo` at the head auto-saves
  unsaved changes as a generation first and reports it as `saved`; everywhere else unsaved changes
  are discarded, so save with `increment` before moving.
- `track gen list`: `{"generations":[{"gen":1,"created":"...","label":"...","notes":12}],"cursor":1,"dirty":false}`.
  Check `dirty` before a cursor move to know whether anything unsaved is at stake; `label` is present
  only for generations that carried one.
- `track gen status`: the file-level detail behind `dirty` — which snapshot files the working vault
  added, changed, or deleted relative to the cursor generation, git-status style:
  `{"cursor":1,"dirty":true,"added":["note/..."],"changed":["note/..."],"deleted":[]}`. Paths are
  vault-relative (a body edit shows `note/<id>.md`, a metadata-only change its `.track/notes/<id>.yaml`
  sidecar). This is the machine-readable basis for a dream report, so the changed set no longer
  depends on the agent's self-report.
- `track gen peek [--gen N] (--id N | --title X | --path P)`: print a note's Markdown as of a
  generation (default: the cursor generation) to stdout, like `export`. The cursor does not move.
  A deleted note no longer resolves by title; peek it by `--id`. Selective revert is peek + diff +
  `track update` with only the parts to restore.

`track rm (--id N | --title X | --path P)` soft-deletes a note: the file and its sidecar move to
`.track/trash/` (never emptied by track) and the index is rebuilt. Backlinks pointing at the
removed title simply stop resolving.

A bulk rewrite (e.g. a memory-consolidation / dream skill) should bracket itself with generations:

```sh
track gen increment    # seal the pre-run state
# ... rename / update / rm notes ...
track gen increment    # approve the result as the new head
# or
track gen undo         # reject; the rejected output survives as the auto-saved
                       # generation and redo revisits it
```

Snapshots cover note bodies, journals, and sidecar metadata only — `assets/` and `data/` are
excluded, so an undo never restores or removes attachments.

## Maintenance

`track graph --orphans` reports vault-wide link-graph hygiene in one call (self-healing the index first): `orphans` are notes (journals excluded) with no inbound `[[link]]`, hence undiscoverable by navigation; `dangling_prefixes` are notes whose title `foo / bar` names a parent scope `foo` that no note owns. It replaces per-note `backlinks` probing when a dream/consolidation pass sweeps the whole vault for reconnection candidates.

`track reindex --full` rebuilds the cache index from the on-disk notes and sidecars; it reconciles deletions silently.

`track doctor` is a read-only health check that never changes files. It reports vault/sidecar divergence — the kind a cloud sync (e.g. OneDrive) can introduce — as a JSON `issues` array, with `ok: true` when clean. Issue kinds: `missing_sidecar`, `orphan_sidecar`, `stray_file` (e.g. a conflict copy that breaks the `<id>.md` naming rule), `unreadable_sidecar`, and `duplicate_title`. Finding issues is not an error, so it still exits 0; only real failures use the `{"error":...}`/exit 1 contract. Run it before a `reindex --full` if you suspect a partially synced vault, so an orphan sidecar is not mistaken for a delete.

`track doctor --fix` repairs that divergence by auto-numbered restore, then reindexes: it writes a fresh `Untitled N` sidecar for a `missing_sidecar`, recreates an empty markdown for an `orphan_sidecar`, keeps the lowest id and renumbers the rest for a `duplicate_title`, and imports a `stray_file` as a new note with a fresh id and title. An `unreadable_sidecar` is reported under `skipped` rather than guessed at. The response carries `changed`, `fixed`, and `skipped`.

`track refresh-all` runs this maintenance pipeline in one idempotent pass — a full `reindex --full` followed by a read-only `doctor` report — for unattended scheduling (cron/launchd). It edits no notes, so repeated runs converge and a `doctor` finding is not a failure; only real errors use the `{"error":...}`/exit 1 contract. The response nests both stages:

```json
{"reindex":{"indexed":42,"deleted":0,"links":17},"doctor":{"scanned":42,"issues":[],"ok":true},"took_ms":12}
```

Schedule it to keep the index in step with a vault a cloud sync edits behind track's back. A crontab line every 15 minutes:

```cron
*/15 * * * * /usr/local/bin/track refresh-all >/dev/null 2>&1
```

Or a launchd agent at `~/Library/LaunchAgents/dev.track.refresh.plist` (`launchctl load` it once):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>            <string>dev.track.refresh</string>
  <key>ProgramArguments</key> <array>
    <string>/usr/local/bin/track</string>
    <string>refresh-all</string>
  </array>
  <key>StartInterval</key>    <integer>900</integer>
  <key>RunAtLoad</key>        <true/>
</dict>
</plist>
```

Point the schedule at the right vault with `TRACK_VAULT` (or a `TRACK_CONFIG` file) in the job's environment, since cron and launchd run with a bare environment.
