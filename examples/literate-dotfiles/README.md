# Literate dotfiles with Babel

Use one [Markdown note](dotfiles.md) to explain and share configuration, then extract a shell command
and Git configuration with the ordinary `track babel tangle` command. No Nix or dotfiles-specific
engine is required. Org Babel supplies the model; track currently authors these blocks in Markdown,
not `.org` files.

## Extract files without a vault

Run from the track repository root with Go installed. File-based tangling needs no track
configuration, database, vault initialization, or `HOME`.

```sh
demo_dir=$(mktemp -d)
go build -o "$demo_dir/track" ./cmd/track
export PATH="$demo_dir:$PATH"

# Print output paths, winning/overridden source locations, block counts, and byte counts.
track babel tangle --file examples/literate-dotfiles/dotfiles.md \
  --out-dir "$demo_dir/output" --dry-run

# Generate the files; repeating this command replaces their contents.
track babel tangle --file examples/literate-dotfiles/dotfiles.md \
  --out-dir "$demo_dir/output"

sh -n "$demo_dir/output/generated/bin/hello"
sh "$demo_dir/output/generated/bin/hello"
# Hello from literate dotfiles.
cat "$demo_dir/output/generated/.gitconfig"
```

Targets are relative to `--out-dir`, so `generated/` here means `$demo_dir/output/generated/`.
Tangle creates missing parent directories. Without `--out-dir`, it creates a unique system temporary
directory and returns `output_dir` with `temporary: true`; a successful real run retains it for the
caller to consume and clean up. Failure and dry-run remove a temporary root. Use an explicit output
root when another tool, such as a Nix build, owns the directory layout.

For several inputs, repeat `--file` in one invocation so the complete plan is validated before
writing. A shared target across different files is an error regardless of input order. Within one
file, the last block targeting a path replaces the entire content. Repeated input aliases are
processed only once; noweb names stay local to each file.

A named fragment expands through `:noweb tangle`, and the Git output explicitly includes its named
fragment. The shebang is part of the source body; the generated command is run with `sh` because it
is not marked executable. `:eval no` prevents Babel execution without preventing extraction.
No files are copied into `$HOME`.

## Share the same note

Publishing uses the existing vault export workflow. Create a separate temporary vault with an
explicit note ID (`100`), so these commands never select a note in your usual vault:

```sh
export TRACK_VAULT="$demo_dir/vault"
export TRACK_CACHE_DIR="$demo_dir/cache"
export TRACK_CONFIG="$demo_dir/machine.yml"

track init
track new --title 'Literate dotfiles' --id 100 \
  < examples/literate-dotfiles/dotfiles.md

track export --id 100 --out "$demo_dir/dotfiles.md"
```

The portable Markdown export removes Babel header arguments from fences; the `:noweb tangle`
reference remains source text because this is not the tangle phase.

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
