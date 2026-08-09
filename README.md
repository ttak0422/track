<p align="center">
  <img src="docs/assets/logo.png" alt="track" width="320" />
</p>

> [!CAUTION]
>  Please note that this is currently in an experimental phase. Destructive changes may be apply.

A journal + Zettelkasten note tool: a Go CLI/LSP engine with a SQLite index, plus a thin Neovim frontend.

The Go engine is the source of truth — it parses notes, maintains the index, and resolves links.
The CLI exposes scriptable commands, `track-lsp` exposes interactive editor navigation, and `track web` serves a local workspace.

**Documentation: <https://ttak0422.github.io/track/>** — syntax, CLI reference, web workspace, tasks, queries, and visualization.

## Layout

```
cmd/track/main.go        # thin CLI entry point
cmd/track-lsp/           # LSP server entry point
internal/cli/            # argument routing -> engine -> JSON
internal/track/          # engine (config, note metadata, store, index, link, lsp)
lua/track/               # Neovim frontend (config, client, lsp, follow, ...)
web/                     # web workspace frontend
nix/apps/                # `nix run .#test-nvim` launcher
flake.nix                # Go CLI + Vim plugin packaging
```

## Agent skills

Skills that let a coding agent drive this CLI live in
[ttak0422/track-lab](https://github.com/ttak0422/track-lab) as the `note` plugin.

What stays here is the contract they build on: every command prints single-line JSON, errors
are `{"error":...}` with exit code 1, and the tool-neutral workflow reference is
[docs/spec/agent-workflows.md](docs/spec/agent-workflows.md).

## Data safety

Note bodies are plain `.md` files, but their metadata (title, tags, created date, Babel results) lives in sidecar files under `.track/notes/`.
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

make site                # build the static help site from docs/help
```

The Nix-built Neovim plugin embeds the store paths of the matching `track` and `track-lsp` binaries, so Nix users do not need to add them to `$PATH` manually.

Design decisions live in [docs/adr/](docs/adr/), specifications in [docs/spec/](docs/spec/).

## License

MIT
