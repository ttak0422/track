# track

Dummy scaffold for a Nix-built toolset: a Go CLI plus a Neovim plugin written
in [Teal](https://github.com/teal-language/tl).

The CLI is the source of truth; the Neovim plugin is a thin frontend that shells
out to it. Everything here is a placeholder implementation.

## Layout

```
cmd/track/main.go     # Go CLI (dummy subcommands)
teal/track/init.tl    # Neovim plugin source (Teal)
lua/                  # generated Lua (tl build output; git-ignored)
tlconfig.lua          # Teal compiler config (teal/ -> lua/)
nix/apps/             # `nix run .#test-nvim` launcher
flake.nix             # Go CLI + Teal->Lua + Vim plugin packaging
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
nix develop              # Go + tl on PATH
go build ./cmd/track     # build the CLI
tl build                 # compile teal/ -> lua/

nix build .#track-cli    # build just the CLI
nix build .#track        # build the Neovim plugin (references the CLI)
nix run .#test-nvim      # launch Neovim with the plugin loaded
```

The Nix-built Neovim plugin embeds the store path of the matching `track`
binary, so Nix users do not need to add `track` to `$PATH` manually.

## License

MIT
