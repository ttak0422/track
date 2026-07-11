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

## Formatting notes

`track fmt` rewrites notes into a canonical Markdown style. It is the style counterpart to
`track doctor`: doctor reports breakage, `fmt` fixes style. The rule set is small and idempotent —
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

Code stays verbatim — the bullets and blank lines inside a fence are left alone:

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
