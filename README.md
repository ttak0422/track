# track

A journal + Zettelkasten note tool: a Go CLI/LSP engine with a SQLite index, plus a thin Neovim frontend.

The Go engine is the source of truth — it parses notes, maintains the index, and resolves links.
The CLI exposes scriptable commands, and `track-lsp` exposes interactive editor navigation.
The engine lives in reusable `internal/track/*` packages so a future LSP server can reuse it directly.

## Concepts

- **Notes** are markdown files named `{unix-timestamp}.md` in a vault directory.
- **Metadata**: note metadata is stored outside the markdown file, under `.track/notes/{id}.yaml`. The file is versioned so future metadata shape changes can be handled explicitly:

  ```markdown
  # リンク

  本文 ...
  ```

  ```yaml
  version: 1
  title: リンク
  aliases: [link, TEST]
  tags: [zettel]
  created: 2026-05-24
  ```

- **Links** are explicit, written `[[title or alias]]`, with optional Obsidian-style `[[target|display]]` aliases. Resolved links are highlighted and followable; links to notes that don't exist yet are highlighted distinctly. Completion offers titles and aliases as you type inside `[[`. Exact-match resolution works for Japanese without word boundaries. See [docs/spec/links.md](docs/spec/links.md).
- **Journal**: each day maps to a stable `yyyyMMdd` note, so opening "today" is idempotent. Journal notes are stored separately under `journal/` and named `yyyyMMdd.md`, so lexical file order follows day order.

## Layout

```
cmd/track/main.go        # thin CLI entry point
internal/cli/            # argument routing -> engine -> JSON
internal/track/          # engine (config, note metadata, store, index, link, lsp)
lua/track/               # Neovim frontend (config, client, lsp, follow, ...)
nix/apps/                # `nix run .#test-nvim` launcher
flake.nix                # Go CLI + Vim plugin packaging
```

## CLI

All commands except `version` print a single line of JSON; errors are `{"error":...}` with exit code 1.
The vault must be set explicitly with `$TRACK_VAULT`; the index db defaults to `<vault>/.track/index.db`.

```sh
track new --title <t> [--id <unix>]   # create a note
track journal [--offset <n>]          # open/create a daily note (0=today)
track reindex [--full]                # rebuild the index
track keywords                        # dump the link keyword dictionary
track resolve --term <s>              # resolve a keyword to a note
track search --query <s> [--limit N]  # search notes
track backlinks (--id N | --path P)   # list backlinks
track babel exec (--id N | --path P) [--name S|--ordinal N] [--yes]
                                      # run a fenced source block (see docs/spec/babel.md)
track dump                            # placeholder state
track version                         # print the version
```

## LSP

`track-lsp` is a Go Language Server Protocol frontend over the same engine packages.
It currently provides:

- `textDocument/documentLink`: returns ranges for resolved `[[...]]` links.
- `textDocument/definition`: jumps from the `[[...]]` under the cursor to the target note.
- `textDocument/completion`: offers titles and aliases inside an open `[[`, triggered on `[`.

The server uses UTF-8 positions and reads the same `$TRACK_VAULT` configuration as the CLI.

## Neovim

```lua
require("track").setup({
  -- vault_dir is required unless TRACK_VAULT is already set
  vault_dir = "/path/to/vault",
})
```

Commands:

```vim
:TrackNew [title]   " create a note (visual selection / args / prompt-with-cword)
:TrackFollow        " follow the [[...]] link under the cursor (also mapped to <CR>)
:TrackToday         " open today's journal note
:TrackYesterday
:TrackTomorrow
:TrackJournal [n]   " journal note at day offset n
:TrackKeywords      " list the link keyword dictionary
:TrackDump          " diagnostic state dump
```

In a vault buffer, resolved `[[...]]` links are underlined (`TrackLink` highlight group) and unresolved ones use `TrackLinkUnresolved`; press `<CR>` on a link to jump to its note. By default the `[[ ]]` brackets are concealed so links read as plain text (`[[Go|ゴー]]` shows `ゴー`); in normal mode the link under the cursor is shown raw (anti-conceal) while other links stay concealed, and while typing the whole cursor line is shown raw so the completion popup stays aligned. `conceal = false` keeps brackets visible. This is backed by `track-lsp`.

Completion of titles and aliases inside `[[` is served over LSP. The plugin merges [`cmp-nvim-lsp`](https://github.com/hrsh7th/cmp-nvim-lsp) capabilities when nvim-cmp is installed, so candidates surface through your existing nvim-cmp setup (add `{ name = "nvim_lsp" }` to its sources). The completion source is UI-independent, so other clients work too.

## Development

```sh
nix develop              # Go on PATH
go test ./...            # run the engine + CLI tests
go build ./cmd/track ./cmd/track-lsp  # build the Go binaries

nix build .#track-cli    # build the Go CLI and LSP binaries
nix build .#track        # build the Neovim plugin (references the CLI)
nix run .#test-nvim      # launch Neovim with a test vault under /tmp
```

The Nix-built Neovim plugin embeds the store paths of the matching `track` and `track-lsp` binaries, so Nix users do not need to add them to `$PATH` manually.

## License

MIT
