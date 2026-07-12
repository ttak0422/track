# CLI

The `track` command is the source of truth for the vault. Every subcommand that changes content
reindexes immediately, so search and links stay current without a separate build step. Commands that
return data print single-line JSON.

Back to [[track]].

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
| `track search --query <s>` | Search notes; `--query '#tag'` filters by tag. |
| `track search --scope body --query <s>` | Full-text search of note bodies, ranked by relevance. |
| `track notes` | List every note, newest first. |
| `track notes --untagged` | List only the notes that still carry no tags. |
| `track resolve --term <s>` | Resolve a keyword to a note. |
| `track backlinks --id N` | List notes that link to a note. |
| `track graph --id N` | Show a local link graph. |
| `track graph --orphans` | List notes with no inbound link and notes whose title names a missing parent scope. |

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
track export-site --src docs/help --root index --out ./site
```

`track render` turns a declarative View Spec into a chart or article — see [[Visualization]].

```sh
track render --spec chart.json --out chart.svg --renderer svg
```
