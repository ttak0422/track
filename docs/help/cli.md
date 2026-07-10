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

## Finding notes

| Command | Purpose |
| --- | --- |
| `track search --query <s>` | Search notes; `--query '#tag'` filters by tag. |
| `track resolve --term <s>` | Resolve a keyword to a note. |
| `track backlinks --id N` | List notes that link to a note. |
| `track graph --id N` | Show a local link graph. |
| `track graph --orphans` | List notes with no inbound link and notes whose title names a missing parent scope. |

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
