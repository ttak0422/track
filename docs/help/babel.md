# Babel

Babel runs the fenced code blocks in a note and keeps their results, so a note can be *executed*, not
just read — track's take on Org Babel, in plain Markdown. Results are stored in the note's sidecar
metadata, never written back into the Markdown body, so the source stays exactly as you typed it.

Back to [[track]] (Babel is a [[CLI]] command).

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

## Languages

`lua` and `viml` ship as ready-to-run sample executors. Add any other language by pointing an
environment variable at its interpreter — `TRACK_BABEL_<LANG>` — for example:

```sh
export TRACK_BABEL_PYTHON="python3"
export TRACK_BABEL_SH="bash"
```

A block whose language has no executor configured is left unrun with a clear error, so nothing executes
by surprise.

tags:: help/authoring
