# CLI

The `track` command is the source of truth for the vault. Every subcommand that changes content
reindexes immediately, so search and links stay current without a separate build step. Commands that
return data print single-line JSON.

up:: [[track]]

## Creating and editing notes

| Command | Purpose |
| --- | --- |
| `track new --title <t>` | Create a note (fails if the title exists). |
| `track open --title <t>` | Open the note with this title, creating it if absent. |
| `track append (--id N \| --title S) --body <s>` | Append body text and/or merge tags. |
| `track update (--id N \| --title S) --body <s>` | Replace body text and/or update tags. |
| `track journal [--offset <n>]` | Open or create a daily note. |

`--body` is read from piped stdin when the flag is omitted:

```sh
printf '本文 [[他ノート]]\n' | track open --title "メモ"
```

Anything that prints Markdown can therefore create notes — the [[Web clipper]] uses this to turn a
web page into a note in one pipeline:

```sh
track-fetch-web --note https://example.com/essay | track new --title "An essay"
```

## Capture, refile, archive

These three commands move text around by heading anchor. A target is written as `Note#Heading`, the
same grammar as a `[[Note#Heading]]` link; add more `#` to pin a heading level (`Note##Sub`). They are
safety-first: an anchor that matches more than one heading is refused rather than guessed, and a move
writes and verifies the destination before it touches the source, so a failed write never loses text.

| Command | Purpose |
| --- | --- |
| `track capture [--target "<note>#<heading>"] [--template <s>] --body <s>` | Append a (templated) entry under a heading. |
| `track refile --from "<note>#<heading>" --to "<note>#<heading>" [--line N]` | Move a heading subtree (or one list item) to another anchor. |
| `track archive "<note>#<heading>"` | Move a subtree into the archive note, stamped with its origin. |

**Capture** appends to a heading of a target note, creating the note when it is the configured inbox
and does not exist yet. With `--template`, the captured text fills the template's `{{ title }}`
placeholder, so a one-line note becomes a framed entry. Set the default inbox with `capture_inbox` in
`config.yml` (it defaults to `Inbox`).

```sh
# with a template like:  - [ ] {{ title }}
track capture --target "Projects#Inbox" --template task --body "ship the release"
```

Given this note:

```markdown
## Inbox

## Done
```

the capture packs a task under `Inbox`:

```markdown
## Inbox
- [ ] ship the release

## Done
```

**Refile** moves a whole heading subtree (nested headings travel with it) from one anchor to another.
The `--line N` variant moves a single list item at line `N` of the source note instead, carrying its
nested items. Text moves verbatim, so `[[links]]` inside it keep resolving; both notes are reindexed so
backlinks follow the move.

```sh
track refile --from "Notes#Draft" --to "Archive 2026#Kept"   # a subtree
track refile --from "Notes" --line 4 --to "Notes#Done"       # one list item
```

**Archive** moves a subtree into a dedicated archive note — per year by default (`archive_note:
"Archive {{year}}"` in `config.yml`) — and records where it came from, so provenance survives the move:

```markdown
## Done

*Archived from [[Projects]] on 2026-07-11.*

- [ ] review the draft
```

Archiving is a living move, distinct from `track rm`, which soft-deletes a whole note into the trash.

## Formatting notes

`track fmt` rewrites notes into a canonical Markdown style. It is the style counterpart to
`track doctor`: doctor reports breakage, `fmt` fixes style. The rule set is small and idempotent -
running it twice changes nothing the second time.

| Command | Purpose |
| --- | --- |
| `track fmt <path>...` | Format the given files or directories in place. |
| `track fmt --all` | Format every note and journal file in the vault. |
| `track fmt --check --all` | Write nothing; exit non-zero and list files that would change (for CI). |

The rules are: strip trailing whitespace, collapse runs of blank lines (except before headings) and
drop blank lines at the start and end, put two blank lines before and one after each heading,
normalize unordered-list bullets to `-`, and end the file with a single newline. Fenced code blocks
are never touched, so their contents stay exactly as written.

A note like this:

```md
# Notes
first line
* apple
+ pear



## Details
done
```

becomes:

```md
# Notes

first line
- apple
- pear


## Details

done
```

Code stays verbatim - the bullets and blank lines inside a fence are left alone:

````md
```text
*  this bullet is code, not a list


so these blank lines survive
```
````

## Finding notes

| Command | Purpose |
| --- | --- |
| `track search --query <s>` | Search notes; `--query '#tag'` filters by tag (hierarchical: `#a` matches `#a/b`). |
| `track search --scope body --query <s>` | Full-text search of note bodies, ranked by relevance. |
| `track query '<expr>'` | Run a table [[Query]] over notes; `--saved <name>` runs a named one. |
| `track notes` | List every note, newest first. |
| `track notes --untagged` | List only the notes that still carry no tags. |
| `track resolve --term <s>` | Resolve a keyword to a note. |
| `track backlinks --id N` | List notes that link to a note. |
| `track graph --id N` | Show a local link graph. |
| `track graph --orphans` | List notes with no inbound link and notes whose title names a missing parent scope. |
| `track similar --id N [--limit K]` | List notes semantically closest to a note (needs an embedder — see Related notes below). |

## Related notes

`track similar --id N` ranks the notes whose meaning is closest to note `N`, most similar first. Unlike
[[Linking notes]], nothing has to be linked by hand: the ranking comes from an embedding of each note's
text. `--limit K` caps the result count (default 10).

The engine never runs a model itself. Instead it shells out to an **embedder command** you configure —
the same split as the `track-fetch-*` tools. Your command reads a note's text on stdin and prints a JSON
array of floats on stdout:

```sh
$ printf 'my note text' | my-embedder
[0.0123, -0.0456, 0.0789, ...]
```

Point `track` at it with the `embedder` key in `config.yml`, in either of two forms:

```yaml
# Scalar: split on whitespace. There is no shell quoting, so no argument may contain a space.
embedder: my-embedder --model all-minilm

# Sequence: used verbatim as argv — the way to pass an argument that contains spaces.
embedder: [my-embedder, --model, "mini lm"]
```

The `TRACK_EMBEDDER` environment variable overrides the config value entirely; an environment variable
cannot carry an array, so it is always whitespace-split like the scalar form.

Any local embedding tool works. A minimal `my-embedder` can be a few lines of Python around a
sentence-transformers model, or a shell wrapper around `llama.cpp`'s embedding output — whatever emits a
float array. Because the heavy lifting lives in your command, the model, its size, and its licence are
entirely your choice, and no note text leaves your machine unless your embedder sends it.

Vectors are cached in the index database, keyed by note plus a content hash, so a note is embedded once
and only re-embedded when its text (or the embedder command) changes. The first `track similar` after a
lot of edits pays to embed the changed notes; later calls are just a cosine scan.

With no embedder configured, `track similar` does not fail — it returns a short message explaining how to
set one up and exits cleanly, and nothing else in `track` is affected:

```json
{"embedder": false, "message": "no embedder configured. Set `embedder` in config.yml ..."}
```

`--scope` selects `title`, `body`, or `all` (the default: titles first, then bodies). Body search
uses a full-text index that stays in step with the vault automatically — see [[Searching notes]] for
ranking, code-block matches, and CJK behavior.

Terms combine the same way in title and body search: space-separated terms are an implicit **AND**,
and an uppercase **OR** separates alternatives — `kubernetes OR postgres` returns notes matching
either, and `deploy staging OR rollback` reads as `(deploy AND staging) OR rollback`. A lowercase
`or` stays an ordinary search word.

## Curating tags

`track notes --untagged` is the pull side of a tagging pass: it returns exactly the notes that still
need tags, so a person — or an automated helper — can work through them one at a time. Add tags to an
existing note with `track append`, which merges them into the sidecar without touching the body:

```sh
track notes --untagged                       # {"notes":[{"note_id":…,"title":"Draft","tags":null}, …]}
track append --title "Draft" --tag zettel    # merge a tag; the note drops off the untagged list
```

Journals are date-titled and have their own [[Web workspace]] surfaces, so they never appear in the
note listing.

## Maintenance

`track refresh-all` runs the whole maintenance pipeline in one idempotent pass: a full reindex followed
by a read-only `doctor` health report. It changes no notes, so it is safe to run unattended on a
schedule (cron or launchd) to keep the index in step with a vault that a cloud sync edits behind
track's back.

```sh
track refresh-all
# {"reindex":{"indexed":42,"deleted":0,"links":17},"doctor":{"scanned":42,"issues":[],"ok":true},"took_ms":12}
```

A crontab line that refreshes every 15 minutes:

```cron
*/15 * * * * /usr/local/bin/track refresh-all >/dev/null 2>&1
```

## Publishing

`track export` writes a single note out as Markdown. `track export-site` builds a whole static HTML
site — see [[Web workspace]] for the rendered reading experience it mirrors.

```sh
track export-site --src docs/help --out ./site
```

`--src` publishes a plain Markdown directory (this help site is built that way); without it, the site is
built from vault notes and `--root <id>` is the landing note. The directory names its entry page and its
icons in a `site.yml` of its own — see [[Home dashboard]]. `--base-url`, `--out`, and `--frontend` are
per-deployment build flags, not part of that config.

`track render` turns a declarative View Spec into a chart or article — see [[Visualization]].

```sh
track render --spec chart.json --out chart.svg --renderer svg
```
