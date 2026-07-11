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

## Finding notes

| Command | Purpose |
| --- | --- |
| `track search --query <s>` | Search notes; `--query '#tag'` filters by tag. |
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

Point `track` at it with the `embedder` key in `config.yml` (or the `TRACK_EMBEDDER` environment
variable, which wins):

```yaml
embedder: my-embedder --model all-minilm
```

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
