# track

Dummy scaffold for a Nix-built toolset: a Go CLI plus a Neovim plugin written
in Lua.

The CLI is the source of truth; the Neovim plugin is a thin frontend that shells
out to it. Everything here is a placeholder implementation.

## Layout

```
cmd/track/main.go     # Go CLI (dummy subcommands)
lua/track/init.lua    # Neovim plugin source (Lua)
nix/apps/             # `nix run .#test-nvim` launcher
flake.nix             # Go CLI + Vim plugin packaging
```

## CLI

```sh
track dump      # print the current state as JSON
track version   # print the version
```

## Neovim

```vim
:TrackDump      " open a diagnostic dump of track state in a scratch buffer
```

## Development

```sh
nix develop              # Go on PATH
go build ./cmd/track     # build the CLI

nix build .#track-cli    # build just the CLI
nix build .#track        # build the Neovim plugin (references the CLI)
nix run .#test-nvim      # launch Neovim with the plugin loaded
```

The Nix-built Neovim plugin embeds the store path of the matching `track`
binary, so Nix users do not need to add `track` to `$PATH` manually.

## License

MIT
