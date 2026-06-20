<p align="center">
  <img src="docs/assets/logo.png" alt="track" width="320" />
</p>

> [!CAUTION]
>  Please note that this is currently in an experimental phase. Destructive changes may be apply.

A journal + Zettelkasten note tool: a Go CLI/LSP engine with a SQLite index, plus a thin Neovim frontend.

The Go engine is the source of truth — it parses notes, maintains the index, and resolves links.
The CLI exposes scriptable commands, and `track-lsp` exposes interactive editor navigation.
The engine lives in reusable `internal/track/*` packages so a future LSP server can reuse it directly.

## Concepts

- **Notes** are markdown files named `note/{id}.md` in a vault directory. Regular note ids are sortable time-derived ids: `Unix seconds * 1000 + same-second sequence`.
- **Metadata**: note metadata is stored outside the markdown file, under `.track/notes/{id}.yaml`. The sidecar `title` is the authoritative note title and link keyword; body H1 headings are ordinary Markdown content. The file is versioned so future metadata shape changes can be handled explicitly:

  ```markdown
  # リンク

  本文 ...
  ```

  ```yaml
  version: 1
  title: リンク
  tags: [zettel]
  created: 2026-05-24
  ```

- **Links** are explicit, written `[[title]]`, with optional Obsidian-style `[[target|display]]` aliases. A heading anchor jumps inside a note: `[[note#foo]]`, `[[note##bar]]`, … where the number of `#` is the Markdown heading level and the first matching heading wins. Resolved links are highlighted and followable; links to notes that don't exist yet are flagged with a warning diagnostic. Completion offers titles as you type inside `[[`, then headings once you type `#`. Exact-match resolution works for Japanese without word boundaries. See [docs/spec/links.md](docs/spec/links.md).
- **Journal**: each day maps to a stable `yyyyMMdd` note, so opening "today" is idempotent. Journal notes are stored as `journal/<yyyyMMdd>.md`. Creating a daily note also rolls it up into month (`journal/<yyyyMM>.md`) and year (`journal/<yyyy>.md`) summary notes, appending `- [[yyyyMMdd]]` to the month and `- [[yyyyMM]]` to the year. The appends are idempotent, so reopening a journal never duplicates the links.

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
The vault is read from the platform user config file (`config.yml` under the track config directory) and defaults to `$HOME/track` when unset (ADR 0015); precedence is `TRACK_VAULT` > config `vault_dir` > `$HOME/track`. Environment variables are intended for tests and one-off overrides.
The rebuildable index db defaults to the user cache directory under `track/`.
The Neovim frontend sets `TRACK_CACHE_DIR` to `vim.fn.stdpath("cache") .. "/track"`.

```yaml
vault_dir: ~/track
# Optional: pick a different template as the default for new notes/journals (created without
# --template or a body). Defaults to the shipped builtin "default" / "journal" templates.
# default_template: my-note
# journal_template: my-journal
```

Typical config locations are `~/.config/track/config.yml` on XDG-style systems and `~/Library/Application Support/track/config.yml` on macOS.

```sh
track new --title <t> [--id <id>] [--template <s>] [--parent-path <p>] [--body <s>] [--tag <s>]
                                      # create a note (fails if the title exists); body is saved verbatim
track open --title <t> [--template <s>] [--parent-path <p>] [--body <s>] [--tag <s>]
                                      # open the note with this title, creating it if absent
track append (--id N | --title S | --path P) [--body <s>] [--tag <s>]
                                      # append body text and/or merge tags
track rename (--id N | --title S | --path P) --to S
                                      # rename a sidecar title and rewrite backlinks
track journal [--offset <n>] [--template <s>] [--body <s>]
                                      # open/create a daily note (0=today)
track reindex [--full]                # rebuild the index
track doctor [--fix]                  # report vault/sidecar divergence; --fix restores by auto-numbering
track keywords                        # dump the link keyword dictionary
track resolve --term <s>              # resolve a keyword to a note
track search --query <s> [--scope all|title|body] [--limit N]
                                      # search notes; use #tag terms to filter by tags
track backlinks (--id N | --path P)   # list backlinks
track graph (--id N | --path P)       # local link graph around a note
track web [--addr 127.0.0.1:8765]     # local interactive web workspace
track template new --name <s> [--id N]
                                      # create a template under template/
track template open --name <s>        # open or create a template
track template list                   # list templates
track babel exec (--id N | --path P) [--name S|--ordinal N] [--yes]
                                      # run a fenced source block (see docs/spec/babel.md)
track babel restore (--id N | --path P)
                                      # list stored source block results
track export (--id N | --title S | --path P) [--out F] [--frontmatter]
                                      # render a note as Markdown
track dump                            # placeholder state
track version                         # print the version
```

User templates are stored under `template/`, are not indexed as notes, and currently support only safe built-in substitutions. track also ships builtin `default` and `journal` templates in the binary; creating a note or journal without `--template` or a body applies the default (a new note gets `# {{ title }}`). A same-named user template in `template/` overrides a builtin, `default_template`/`journal_template` in `config.yml` pick a different default, and an explicit `--template` or `--body` overrides per invocation. See [docs/spec/templates.md](docs/spec/templates.md). A ready-to-copy example covering every supported option lives at [examples/templates/example.template.md](examples/templates/example.template.md).

## Web

`track web` serves a local-only workspace for interactive exploration:

```sh
track web
```

Open `http://127.0.0.1:8765/` while the command is running. The first version includes note search, Markdown preview, backlinks, and a Canvas graph with a Local/Global toggle. Use `#tag` terms, such as `#graph #web`, to filter by sidecar tags. This is not the publish/export path; public output should use `track export` plus a static site generator.

A standalone Markdown image link on its own line (`![alt](url)`) renders as an embed: YouTube links (`youtu.be`, `youtube.com/watch`, `/shorts`, `/live`, `/embed`, honoring a `t=`/`start=` timestamp) become an inline player, Twitter/X status links (`twitter.com`/`x.com/<user>/status/<id>`) embed the actual post via Twitter's widget (falling back to a card if it cannot load), `.pdf` links an inline viewer with an "open" fallback, image URLs an image, and any other `http(s)` page an Open Graph card (title, description, and thumbnail, fetched by the local server with an SSRF guard; it falls back to a plain link when the page has no metadata or cannot be reached). A plain `[label](url)` stays a link — embedding is opt-in via `![…]()` so ordinary links are never turned into cards — and inline `![…](…)` mixed into a paragraph is left untouched.

The workspace theme can be set with `web.theme` (`system`/`light`/`dark`) in `config.yml`, and colors can be overridden by pointing `web.colors_path` at a palette file:

```yaml
web:
  theme: dark
  colors_path: ~/.config/track/colors.yml
```

The palette file has optional `light:` and `dark:` sections mapping a themeable variable (`accent`, `accent-strong`, `bg`, `panel`, `panel-soft`, `text`, `muted`, `line`, `generated`, `danger`) to a CSS color. Unknown keys are ignored and only safe color values are accepted.

## LSP

`track-lsp` is a Go Language Server Protocol frontend over the same engine packages.
It currently provides:

- `textDocument/documentLink`: returns ranges for resolved `[[...]]` links.
- `textDocument/definition`: jumps from the `[[...]]` under the cursor to the target note, or to the matching heading line for a `[[note##heading]]` anchor.
- `textDocument/hover`: previews the linked note under the cursor, including tags and the note's leading body.
- `textDocument/references`: lists backlinks to the current note or the link target under the cursor.
- `textDocument/completion`: offers titles inside an open `[[` — with each matching note's headings offered alongside it as full `note##heading` anchors — plus narrowed heading candidates after a `[[note#` anchor (more `#` selects a deeper heading level), Markdown action link candidates inside `[label](<...>)`, and Babel fence info-string candidates.
- `textDocument/codeAction`: creates a note from an unresolved `[[...]]` link, repairs a link to a renamed note, and offers "Rename note …" for the link target under the cursor (or the current note), which prompts for a new title and runs the rename below.
- `textDocument/rename`: renaming the `[[link]]` under the cursor (or the current note when not on a link) updates the target's sidecar title, records rename history, and returns backlink edits; the target body is not edited.
- `track/backlinks`: returns notes and link locations that reference the current note.
- `track/outgoingLinks`: returns resolved link locations inside the current note.

The server uses UTF-8 positions and reads the same config file as the CLI. `TRACK_VAULT` remains available as a test/one-off override.
It only acts on track notes: a request is served only for a supported note file (`.md`) inside the vault, excluding `.track/`. Markdown opened elsewhere gets no links, completion, or actions, even if the editor attaches the server to it. The Neovim layer also attaches `track-lsp` only to markdown under the vault; other editor integrations should gate attachment the same way. See [docs/spec/links.md](docs/spec/links.md).

## Neovim

```lua
require("track").setup({
  -- Optional: overrides config.yml for this Neovim setup.
  vault_dir = "/path/to/vault",
})
```

Commands:

```vim
:Track open [title]    " open or create a note by title (visual selection / args / empty prompt); existing titles are reused
:Track template [name] " open or create a template for editing
:Track from_template [template] [title]
                       " create a note from a template; prompts when omitted
:Track templates       " search templates with Telescope and open one for editing
:Track search_title [query]
                       " search note titles with Telescope
:Track search_body [query]
                       " search note bodies with Telescope
:Track follow          " follow the [[...]] link under the cursor (also mapped to <CR>)
:Track backlinks       " show notes that link to the current note (Telescope when available, else quickfix); listed by title, not the epoch filename
:Track links           " show links from the current note in quickfix
:Track graph           " show a local note graph around the current note
:Track web [addr]      " start the local web workspace and open it in a browser
:Track babel_exec      " run the source block under the cursor; result shows below it
:Track babel_restore   " restore stored babel results without running code
:Track babel_clear     " clear rendered babel results in the buffer
:Track today           " open today's journal note
:Track yesterday
:Track tomorrow
:Track journal [n]     " journal note at day offset n
:Track reindex         " delete and rebuild the SQLite index after confirmation
:Track keywords        " list the link keyword dictionary
:Track dump            " diagnostic state dump
```

Markdown links can trigger track actions when followed with `:Track follow` / `<CR>`, for example `[今日](<journal?offset=0>)` or `[今日の会議](<note?template=meeting&title={{date}} Project MTG>)`. In action links, `{{date}}` and `{{journal}}` both expand as `yyyyMMdd`. When a `note` action creates from a template, the source note (where the link was followed) is passed as the parent, so the template can reference it with `{{ parent }}`.

The optional [telescope.nvim](https://github.com/nvim-telescope/telescope.nvim) extension provides title and body search pickers:

```lua
require("telescope").load_extension("track")
require("telescope").extensions.track.search_title({ query = "Go" })
require("telescope").extensions.track.search_body(require("telescope.themes").get_dropdown({ query = "TODO" }))
require("telescope").extensions.track.search_templates()
```

The same pickers are available as `:Track search_title [query]` and `:Track search_body [query]`, which is useful when a plugin manager lazy-loads track on the `:Track` command before Telescope extensions are registered.

The picker uses Telescope's prompt for live searching. `query` seeds the initial prompt text when supplied, and Telescope picker options, including themes, can be passed through the opts table. When the search turns up nothing (or you want a fresh note by that name), press `<CR>` to create and open a note titled with the current prompt text; with results highlighted, `<CR>` opens the selected note as usual.

In a vault buffer, resolved `[[...]]` links are underlined (`TrackLink` highlight group); unresolved ones are flagged by an `unresolved-link` warning diagnostic. Press `<CR>` on a link to jump to its note. By default the `[[ ]]` brackets are concealed so links read as plain text (`[[Go|ゴー]]` shows `ゴー`); in normal mode the link under the cursor is shown raw (anti-conceal) while other links stay concealed, and while typing the whole cursor line is shown raw so the completion popup stays aligned. `conceal = false` keeps brackets visible. Raising conceallevel would otherwise let Neovim's treesitter markdown query hide code-fence delimiters, so track reveals them by default (toggle with `reveal_code_fences`). This is backed by `track-lsp`.

Press `K` on a resolved link to show the linked note preview in Neovim's hover window.

Use `:checkhealth track` to verify the resolved CLI/LSP binaries, vault/cache configuration, and current-buffer LSP attachment.

Completion of titles inside `[[` is served over LSP. The plugin merges [`cmp-nvim-lsp`](https://github.com/hrsh7th/cmp-nvim-lsp) capabilities when nvim-cmp is installed, so candidates surface through your existing nvim-cmp setup (add `{ name = "nvim_lsp" }` to its sources). The completion source is UI-independent, so other clients work too.

Babel fence info strings are completed over the same LSP source. On an opening fence such as ```` ```lua :results output ````, track completes configured Babel languages, supported header keys, and fixed values for headers such as `:results`, `:eval`, `:cache`, `:session`, `:exports`, `:noweb`, `:tangle`, and `:visible-lines`. Header-key candidates insert one trailing space, so accepting `:eval` leaves the cursor at `:eval ` where value candidates such as `yes`, `no`, and `query` are available. `:visible-lines 4-5,8` is an editor-only display hint that hides source block body lines outside the listed 1-based ranges without changing execution.

## Claude Code plugin

This repository doubles as a [Claude Code](https://docs.claude.com/en/docs/claude-code) plugin marketplace, so coding agents can drive the CLI through bundled skills. The skills carry the JSON output contract and focused command workflows:

- `track-create-note`: create/open notes and journals, append to notes, and create notes from templates.
- `track-search-notes`: search, resolve, export/read notes, backlinks, and graph inspection.
- `track`: maintenance workflows such as rename/backlink repair, doctor, and reindex.

```text
# add this repo as a marketplace, then install the track plugin
/plugin marketplace add ttak0422/track
/plugin install track@track
```

The marketplace manifest is [`.claude-plugin/marketplace.json`](.claude-plugin/marketplace.json) and the plugin lives at [`plugins/track`](plugins/track) (manifest `plugins/track/.claude-plugin/plugin.json`, skills under `plugins/track/skills/`). After installing, configure `vault_dir` in `config.yml` so the agent's commands resolve against your vault. The tool-neutral contract the skills point to is [docs/spec/agent-workflows.md](docs/spec/agent-workflows.md).

## Codex skill

The bundled skills are also installable by Codex through this repository's marketplace. The marketplace manifest is [`.agents/plugins/marketplace.json`](.agents/plugins/marketplace.json), and the Codex plugin manifest is [`plugins/track/.codex-plugin/plugin.json`](plugins/track/.codex-plugin/plugin.json). It points at the same `plugins/track/skills/` directory.

From a local checkout:

```sh
codex plugin marketplace add .
codex plugin add track@track
```

From GitHub:

```sh
codex plugin marketplace add ttak0422/track
codex plugin add track@track
```

Restart Codex or start a new thread after installing. Configure `vault_dir` in `config.yml` so Codex-run `track` commands resolve against your vault.

## Data safety

Note bodies are plain `.md` files, but their metadata (title, tags, created date, Babel results) lives in sidecar files under `.track/notes/`. Manual title rename history lives in `.track/renames.yaml` for unresolved-link repair suggestions.
The `.track/` directory is **authoritative** and cannot be fully rebuilt from the note bodies, so back it up and keep it in version control, just as you would `.git`.
The SQLite index is a disposable cache outside the vault. `track reindex --full` deletes the cache database and rebuilds it from note files and sidecar metadata.
See [docs/spec/storage.md](docs/spec/storage.md) for details.

## Development

```sh
nix develop              # Go on PATH
go test ./...            # run the engine + CLI tests
go build ./cmd/track ./cmd/track-lsp  # build the Go binaries

nix build .#track-cli    # build the Go CLI and LSP binaries
nix build .#track        # build the Neovim plugin (references the CLI)
nix run .#test-nvim      # launch Neovim; the vault defaults to $HOME/track (TRACK_VAULT/config.yml override)

TRACK_VAULT="$(mktemp -d)" TRACK_CACHE_DIR="$(mktemp -d)" \
  nix run .#test-nvim -- --headless '+luafile scripts/e2e/nvim_action_links.lua'
                         # run the Neovim action-link E2E used by CI with temporary overrides
```

The Nix-built Neovim plugin embeds the store paths of the matching `track` and `track-lsp` binaries, so Nix users do not need to add them to `$PATH` manually.

## License

MIT
