# Literate dotfiles with Babel

Use one [Markdown note](dotfiles.md) to explain and share configuration, then extract a shell command
and Git configuration with the ordinary `track babel tangle` command. No Nix or dotfiles-specific
engine is required. Org Babel supplies the model; track currently authors these blocks in Markdown,
not `.org` files.

## Create an isolated vault and tangle

Run from the track repository root with Go installed. The example uses an explicit numeric note ID
(`100`) in a new temporary vault, so the commands never select a note in your usual vault.

```sh
demo_dir=$(mktemp -d)
go build -o "$demo_dir/track" ./cmd/track
export PATH="$demo_dir:$PATH"
export TRACK_VAULT="$demo_dir/vault"
export TRACK_CACHE_DIR="$demo_dir/cache"
export TRACK_CONFIG="$demo_dir/machine.yml"

track init
track new --title 'Literate dotfiles' --id 100 \
  < examples/literate-dotfiles/dotfiles.md

# Print paths, block counts, and byte counts; create no output files.
track babel tangle --id 100 --dry-run

# Generate the files; repeating this command replaces their contents.
track babel tangle --id 100

sh -n "$TRACK_VAULT/note/generated/bin/hello"
sh "$TRACK_VAULT/note/generated/bin/hello"
# Hello from literate dotfiles.
cat "$TRACK_VAULT/note/generated/.gitconfig"
```

Targets are relative to the note's directory, so `generated/` here means
`$TRACK_VAULT/note/generated/`. Tangle creates the parent directories inside the vault. A named fragment
expands through `:noweb tangle`, and the Git output explicitly includes its named fragment. The shebang is part
of the source body; the generated command is run with `sh` because it is not marked executable.
`:eval no` prevents Babel execution without preventing extraction. No files are copied into `$HOME`.

## Share the same note

For portable Markdown, export the original note. This removes Babel header arguments from fences;
the `:noweb tangle` reference remains source text because this is not the tangle phase.

```sh
track export --id 100 --out "$demo_dir/dotfiles.md"
```

For a browsable static site, install Node.js/npm and Python 3 as well, then run these commands from
the repository root in the same shell. The config below belongs only to the new demonstration vault.
`make site` installs the frontend dependencies, builds the static frontend and CLI, runs `export-site`,
and prerenders the pages. `SITE_OUT` is recreated by that target.

```sh
printf 'web:\n  home: "100"\n' > "$TRACK_VAULT/.track/config.yml"
make SITE_VAULT="$TRACK_VAULT" SITE_CACHE="$TRACK_CACHE_DIR" \
  SITE_OUT="$demo_dir/site" site
python3 -m http.server 8000 --directory "$demo_dir/site"
```

Open <http://localhost:8000/>. The generated site directory can also be uploaded to a static host.
`track export-site` needs the built static frontend (`--frontend`); exporting the data alone does not
produce a working viewer.

The web exporter currently preserves source blocks and their headers. It does not apply Babel
`:exports` filtering or include stored execution results. The website therefore shows the original
source, including noweb references, rather than the assembled files. Neither export command executes
code, and the example does not depend on result rendering.

Nix module composition, package realization, machine-specific selection, secret handling, and
installing files into a home directory need a separate design. This example exercises the generic
Babel extraction and publishing path without adding those policies to it.
