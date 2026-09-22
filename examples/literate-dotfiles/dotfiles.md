# Literate dotfiles

This note is both a readable explanation of a tiny configuration and the source of its files.
It uses track's Markdown fences with Org Babel-style headers; it is not an Org-format document.
Tangling writes into `generated/` beside the note, leaving the machine's installed configuration alone.

## A shell command

Keep a reusable greeting in a named block. It is only a fragment, so it has no output file.

```sh :name greeting :tangle no :eval no
printf '%s\n' 'Hello from literate dotfiles.'
```

The command starts with a literal shebang and expands the greeting when tangled.
Run the generated file with `sh`; tangling does not make it executable.

```sh :name hello :tangle generated/bin/hello :noweb tangle :eval no
#!/bin/sh
set -eu

<<greeting>>
```

## Git preferences

New repositories use `main` as their initial branch.

```gitconfig :name git-init :tangle no :eval no
[init]
    defaultBranch = main
```

Git uses `vi` when it needs an editor. Compose the initial branch preference explicitly through
noweb. If several blocks target the same file, the last block replaces its entire content.

```gitconfig :name git-editor :tangle generated/.gitconfig :noweb tangle :eval no
<<git-init>>

[core]
    editor = vi
```

All blocks disable Babel execution with `:eval no`; tangling only extracts their text.
Publishing this note shares the explanation and source, including the unexpanded `<<greeting>>`
reference. It neither executes the blocks nor installs the generated files.
