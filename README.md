# track

A journal + Zettelkasten note tool: a Go CLI engine with a SQLite index, plus a
thin Neovim frontend.

The Go CLI is the source of truth — it parses notes, maintains the index, and
resolves links. The Neovim plugin shells out to it. The engine lives in reusable
`internal/track/*` packages so a future LSP server can reuse it directly.

## Concepts

- **Notes** are markdown files named `{unix-timestamp}.md` in a vault directory.
- **Metadata**: note metadata is stored outside the markdown file, under
  `.track/notes/{id}.yaml`. The file is versioned so future metadata shape
  changes can be handled explicitly:

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

- **Auto-links** work like the old Hatena keyword auto-link: any registered note
  title or alias is automatically highlighted and followable wherever it appears
  in vault buffers — no special markup, and it works mid-word for Japanese
  (longest-match substring). Unregistered words do nothing.
- **Journal**: each day maps to a stable `yyyyMMdd` note, so opening "today" is
  idempotent. Journal notes are stored separately under
  `journal/` and named `yyyyMMdd.md`, so lexical file order follows day order.

## Layout

```
cmd/track/main.go        # thin CLI entry point
internal/cli/            # argument routing -> engine -> JSON
internal/track/          # engine (config, note metadata, store, index, match)
lua/track/               # Neovim frontend (config, client, autolink, follow, ...)
nix/apps/                # `nix run .#test-nvim` launcher
flake.nix                # Go CLI + Vim plugin packaging
```

## CLI

All commands except `version` print a single line of JSON; errors are
`{"error":...}` with exit code 1. The vault must be set explicitly with
`$TRACK_VAULT`; the index db defaults to `<vault>/.track/index.db`.

```sh
track new --title <t> [--id <unix>]   # create a note
track journal [--offset <n>]          # open/create a daily note (0=today)
track reindex [--full]                # rebuild the index
track keywords                        # dump the auto-link dictionary
track resolve --term <s>              # resolve a keyword to a note
track search --query <s> [--limit N]  # search notes
track backlinks (--id N | --path P)   # list backlinks
track dump                            # placeholder state
track version                         # print the version
```

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
:TrackFollow        " follow the auto-link under the cursor (also mapped to <CR>)
:TrackToday         " open today's journal note
:TrackYesterday
:TrackTomorrow
:TrackJournal [n]   " journal note at day offset n
:TrackKeywords      " list the auto-link dictionary
:TrackDump          " diagnostic state dump
```

In a vault buffer, registered keywords are underlined (`TrackLink` highlight
group); press `<CR>` on one to jump to its note.

## Development

```sh
nix develop              # Go on PATH
go test ./...            # run the engine + CLI tests
go build ./cmd/track     # build the CLI

nix build .#track-cli    # build just the CLI
nix build .#track        # build the Neovim plugin (references the CLI)
nix run .#test-nvim      # launch Neovim with a test vault under /tmp
```

The Nix-built Neovim plugin embeds the store path of the matching `track`
binary, so Nix users do not need to add `track` to `$PATH` manually.

## License

MIT
