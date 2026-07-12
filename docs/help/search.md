# Searching notes

`track search` looks through the vault two ways, chosen with `--scope`:

| Scope | What it matches |
| --- | --- |
| `title` | Note titles and `#tag` filters, ranked by how closely the title matches. |
| `body` | The full text of every note body, ranked by relevance. |
| `all` (default) | Title hits first, then body hits that were not already found. |

icon:: 🔍

Back to [[track]]. The [[CLI]] page lists the surrounding commands.

## Title and tag search

A plain query matches titles with implicit-AND terms, an uppercase `OR` between
alternatives, and a `#tag` prefix to filter by tag:

```sh
track search --query "graph OR web"
track search --query "#zettel workspace"
```

## Full-text body search

`--scope body` runs against a full-text index kept in the SQLite cache, so it
does not re-read every file on each query. Terms combine like they do for
titles: space-separated terms are an implicit AND — a note matches only when its
body contains all of them — and an uppercase `OR` separates alternatives, so
`containerd OR podman` matches either. Results are ranked by relevance (denser,
more focused matches first). Each hit still reports the first matching line and a
snippet, so a picker can jump straight to it:

```sh
track search --scope body --query "containerd runtime"
track search --scope body --query "containerd OR podman"
```

```json
{"results":[{"note_id":900,"title":"Deploy notes","line":3,"snippet":"We run containerd as the runtime."}]}
```

The index stores the body verbatim, so text inside fenced code blocks is
searchable too:

```sh
track search --scope body --query "nginx"   # finds a note that only mentions nginx in a YAML block
```

Because the body index is part of the same rebuildable cache as titles and
links, an edit made outside track — a save from your editor, or a change pulled
in by file sync — is reconciled automatically the next time you search. You
never run a separate "rebuild search" step.

## CJK and short queries

The body index tokenizes on character trigrams, so it matches substrings and
works for scripts without spaces, such as Japanese or Chinese:

```sh
track search --scope body --query "テスト"
```

A query term shorter than three characters — a two-letter word, or a
two-character word like 世界 — cannot form a trigram. Those queries fall back to
a direct file scan, so they keep working; only very short queries pay that cost,
and the common case stays on the fast index.
