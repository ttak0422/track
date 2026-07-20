# Babel

Babel runs the fenced code blocks in a note and keeps their results, so a note can be *executed*, not
just read — track's take on Org Babel, in plain Markdown. Results are stored in the note's sidecar
metadata, never written back into the Markdown body, so the source stays exactly as you typed it.

up:: [[CLI]]

## A runnable block

Any fenced code block whose info string starts with a language is a Babel block. Options follow the
language as Org-style `:key value` header arguments, keeping the fence a valid Markdown code block:

````markdown
```lua :name hello :results output
print("hi from a note")
```
````

## Running it

Execute a block with the CLI, choosing one by `:name`, position, or cursor line:

```sh
track babel exec --id <note-id> --name hello
track babel exec --path note.md --ordinal 0      # 0-based block index
track babel exec --path note.md --line 12        # block containing this 0-based line
```

Execution is opt-in and never happens on its own. A block is subject to its `:eval` policy:

- `:eval yes` (default) — runs when you invoke it.
- `:eval query` — refuses unless you pass `--yes`.
- `:eval no` — never runs.

`--timeout` caps each run (30s by default), and `:dir` runs the block relative to another directory
(constrained to the vault).

## Results

`:results` decides what is captured and kept, as a set of tokens (default `output replace`):

- `output` — captured stdout/stderr and exit status.
- `verbatim` — the raw value, stored as text.
- `replace` — keep only the latest result; `silent` runs without storing; `none` / `discard` throw
  the result away.

Stored results live in the sidecar keyed by the block. List what a note has kept with:

```sh
track babel restore --id <note-id>
```

In the [[Web workspace]] and the static export ([[CLI]] `export-site`), a block's stored result is
rendered next to its source (controlled by `:exports code|results|both|none`). In Neovim the result
shows as virtual lines just below the block, leaving the buffer text untouched.

## Composing blocks: noweb references

A block can pull in other named blocks with Org's noweb syntax: `<<name>>` is replaced by the body of
the block named `name`, recursively, before the code runs. Expansion is opt-in per block through
`:noweb`:

- `:noweb no` (default) — `<<...>>` is left as literal text.
- `:noweb yes` — expand before executing *and* before tangling.
- `:noweb eval` / `:noweb tangle` — expand only in that phase.

````markdown
```sh :name greeting
echo track
```

```sh :name main :noweb yes
<<greeting>>
echo compiling
```
````

Running `main` executes both lines. A reference standing alone on a line keeps its indentation (every
expanded line is re-indented to match), an unknown name is an error, and a reference cycle is refused
with the chain spelled out, e.g. `noweb cycle: a -> b -> a`.

## Calling blocks with variables

`track babel run` calls a named block with parameters:

```sh
track babel run --id <note-id> --name shout --var excitement=5
```

A block can also declare its own inputs with `:var` header arguments. A value that names another
block in the note feeds in that block's *stored result* — run the dependency once with `exec`, and
its output becomes the variable:

````markdown
```sh :name shout :var name=greeting :var excitement=3
printf '%s%s\n' "$name" "$(printf '!%.0s' $(seq "$excitement"))"
```
````

With `greeting`'s stored result being `track`, calling `shout` prints (real output):

```json
{"id":"shout","status":"success","stdout":"track!!!\n","exit_code":0,"stored":true}
```

Variables reach the block as **environment variables**, the one mechanism every configured language
shares. That sets the limits:

- Values are always strings; numbers arrive as their decimal text (`--var n=42` gives `$n` = `42`).
- Keys must be valid environment names (`[A-Za-z_][A-Za-z0-9_]*`).
- A `:var` value in the fence info string cannot contain spaces (the info string splits on
  whitespace); pass spacey values from the CLI, where `--var msg="hello world"` works.
- `--var` overrides a `:var` of the same name; a dependency with no stored result is an error telling
  you which block to `exec` first. Dependencies are never executed automatically.

`track babel run` and `track babel exec` are the same command under two names, so `:var` and `--var`
work with either.

## Tangling source to files

Literate-programming extraction, as in Org: a block carrying `:tangle <path>` is written out to that
file. Several blocks naming the same target concatenate in note order, separated by a blank line, and
`:noweb yes` / `:noweb tangle` blocks are expanded first:

````markdown
```sh :name prelude :tangle scripts/build.sh
set -eu
```

```sh :name compile :tangle scripts/build.sh :noweb tangle
<<greeting>>
echo compiling
```
````

```sh
track babel tangle --id <note-id> --dry-run   # print the plan, write nothing
track babel tangle --id <note-id>             # write the files
```

The dry-run plan for the note above (real output):

```json
{"dry_run":true,"targets":[{"blocks":2,"bytes":35,"path":".../note/scripts/build.sh"}]}
```

and the written `scripts/build.sh`:

```sh
set -eu

echo track
echo compiling
```

Targets resolve relative to the note's directory, missing parent directories are created, and any
path that lands outside the vault is refused — a note can never tangle over files beyond the vault it
lives in. `:tangle yes` (Org's derived file naming) is not supported; name the file explicitly.

## Languages

`lua` and `viml` ship as ready-to-run sample executors. Add any other language by pointing an
environment variable at its interpreter — `TRACK_BABEL_<LANG>` — for example:

```sh
export TRACK_BABEL_PYTHON="python3"
export TRACK_BABEL_SH="bash"
```

A block whose language has no executor configured is left unrun with a clear error, so nothing executes
by surprise.
