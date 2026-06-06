# track

> [!CAUTION]
>  Please note that this is currently in an experimental phase. Destructive changes may be apply.

A journal + Zettelkasten note tool: a Go CLI/LSP engine with a SQLite index, plus a thin Neovim frontend.

The Go engine is the source of truth — it parses notes, maintains the index, and resolves links.
The CLI exposes scriptable commands, and `track-lsp` exposes interactive editor navigation.
The engine lives in reusable `internal/track/*` packages so a future LSP server can reuse it directly.

## Concepts

- **Notes** are markdown files named `note/{id}.md` in a vault directory. Regular note ids are sortable time-derived ids: `Unix seconds * 1000 + same-second sequence`.
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

- **Links** are explicit, written `[[title or alias]]`, with optional Obsidian-style `[[target|display]]` aliases. A heading anchor jumps inside a note: `[[note#foo]]`, `[[note##bar]]`, … where the number of `#` is the Markdown heading level and the first matching heading wins. Resolved links are highlighted and followable; links to notes that don't exist yet are highlighted distinctly. Completion offers titles and aliases as you type inside `[[`, then headings once you type `#`. Exact-match resolution works for Japanese without word boundaries. See [docs/spec/links.md](docs/spec/links.md).
- **Journal**: each day maps to a stable `yyyyMMdd` note, so opening "today" is idempotent. Journal notes are stored as `journal/<yyyyMMdd>.md`.

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
The vault must be set explicitly with `$TRACK_VAULT`; the rebuildable index db defaults to the user cache directory under `track/`.
The Neovim frontend sets `TRACK_CACHE_DIR` to `vim.fn.stdpath("cache") .. "/track"`.

```sh
track new --title <t> [--id <id>] [--template <s>]
                                      # create a note (fails if the title exists)
track open --title <t> [--template <s>]
                                      # open the note with this title, creating it if absent
track journal [--offset <n>] [--template <s>]
                                      # open/create a daily note (0=today)
track reindex [--full]                # rebuild the index
track keywords                        # dump the link keyword dictionary
track resolve --term <s>              # resolve a keyword to a note
track search --query <s> [--scope all|title|body] [--limit N]
                                      # search notes
track backlinks (--id N | --path P)   # list backlinks
track template new --name <s> [--id N]
                                      # create a template under template/
track template open --name <s>        # open or create a template
track template list                   # list templates
track babel exec (--id N | --path P) [--name S|--ordinal N] [--yes]
                                      # run a fenced source block (see docs/spec/babel.md)
track babel restore (--id N | --path P)
                                      # list stored source block results
track dump                            # placeholder state
track version                         # print the version
```

Templates are stored under `template/`, are not indexed as notes, and currently support only safe built-in substitutions. See [docs/spec/templates.md](docs/spec/templates.md).

## LSP

`track-lsp` is a Go Language Server Protocol frontend over the same engine packages.
It currently provides:

- `textDocument/documentLink`: returns ranges for resolved `[[...]]` links.
- `textDocument/definition`: jumps from the `[[...]]` under the cursor to the target note, or to the matching heading line for a `[[note##heading]]` anchor.
- `textDocument/references`: lists backlinks to the current note or the link target under the cursor.
- `textDocument/completion`: offers titles and aliases inside an open `[[` — with each matching note's headings offered alongside it as full `note##heading` anchors — plus narrowed heading candidates after a `[[note#` anchor (more `#` selects a deeper heading level) and Babel fence info-string candidates, triggered on `[`, `#`, `:`, and space.
- `textDocument/codeAction`: creates a note from an unresolved `[[...]]` link.
- `track/backlinks`: returns notes and link locations that reference the current note.

The server uses UTF-8 positions and reads the same `$TRACK_VAULT` configuration as the CLI.
It only acts on track notes: a request is served only for a supported note file (`.md`) inside the vault, excluding `.track/`. Markdown opened elsewhere gets no links, completion, or actions, even if the editor attaches the server to it. The Neovim layer also attaches `track-lsp` only to markdown under the vault; other editor integrations should gate attachment the same way. See [docs/spec/links.md](docs/spec/links.md).

## Neovim

```lua
require("track").setup({
  -- vault_dir is required unless TRACK_VAULT is already set
  vault_dir = "/path/to/vault",
})
```

Commands:

```vim
:Track open [title]    " open or create a note by title (visual selection / args / prompt-with-cword); existing titles are reused
:Track template [name] " open or create a template for editing
:Track from_template [template] [title]
                       " create a note from a template; prompts when omitted
:Track follow          " follow the [[...]] link under the cursor (also mapped to <CR>)
:Track backlinks       " show notes that link to the current note in quickfix
:Track babel_exec      " run the source block under the cursor; result shows below it
:Track babel_restore   " restore stored babel results without running code
:Track babel_clear     " clear rendered babel results in the buffer
:Track today           " open today's journal note
:Track yesterday
:Track tomorrow
:Track journal [n]     " journal note at day offset n
:Track keywords        " list the link keyword dictionary
:Track dump            " diagnostic state dump
```

Markdown links whose target starts with `track://` are handled as track actions when followed with `:Track follow` / `<CR>`, for example `[本日のmtg](track://journal?template=meeting)`. Use Markdown's `<...>` destination form for values with spaces: `[今日の会議](<track://open?template=meeting&title={{date}} Project MTG>)`.

The optional [telescope.nvim](https://github.com/nvim-telescope/telescope.nvim) extension provides title and body search pickers:

```lua
require("telescope").load_extension("track")
require("telescope").extensions.track.search_title({ query = "Go" })
require("telescope").extensions.track.search_body(require("telescope.themes").get_dropdown({ query = "TODO" }))
```

The picker uses Telescope's prompt for live searching. `query` seeds the initial prompt text when supplied, and Telescope picker options, including themes, can be passed through the opts table.

In a vault buffer, resolved `[[...]]` links are underlined (`TrackLink` highlight group) and unresolved ones use `TrackLinkUnresolved`; press `<CR>` on a link to jump to its note. By default the `[[ ]]` brackets are concealed so links read as plain text (`[[Go|ゴー]]` shows `ゴー`); in normal mode the link under the cursor is shown raw (anti-conceal) while other links stay concealed, and while typing the whole cursor line is shown raw so the completion popup stays aligned. `conceal = false` keeps brackets visible. Raising conceallevel would otherwise let Neovim's treesitter markdown query hide code-fence delimiters, so track reveals them by default (toggle with `reveal_code_fences`). This is backed by `track-lsp`.

Completion of titles and aliases inside `[[` is served over LSP. The plugin merges [`cmp-nvim-lsp`](https://github.com/hrsh7th/cmp-nvim-lsp) capabilities when nvim-cmp is installed, so candidates surface through your existing nvim-cmp setup (add `{ name = "nvim_lsp" }` to its sources). The completion source is UI-independent, so other clients work too.

Babel fence info strings are completed over the same LSP source. On an opening fence such as ```` ```lua :results output ````, track completes configured Babel languages, supported header keys, and fixed values for headers such as `:results`, `:eval`, `:cache`, `:session`, `:exports`, `:noweb`, and `:tangle`. Header-key candidates insert one trailing space, so accepting `:eval` leaves the cursor at `:eval ` where value candidates such as `yes`, `no`, and `query` are available.

## Data safety

Note bodies are plain `.md` files, but their metadata (aliases, tags, created date, Babel results) lives in sidecar files under `.track/notes/`.
That directory is **authoritative** and cannot be rebuilt from the note bodies, so back it up and keep it in version control, just as you would `.git`.
The SQLite index is a disposable cache outside the vault and can be rebuilt at any time with `track reindex --full`.
See [docs/spec/storage.md](docs/spec/storage.md) for details.

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
