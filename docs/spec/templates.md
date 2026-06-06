# Template Specification

This document describes the current template implementation.

## File Layout

Templates are markdown-like files under the vault `template/` directory:

```text
<vault>/template/<id>.template.md
```

The id is numeric. When the caller does not pass `--id`, track uses the same second-bucket allocator as regular notes: `Unix seconds * 1000 + same-second sequence`.

Templates are not indexed as notes. They do not appear in note search, keyword resolution, links, backlinks, or LSP note features.

## File Format

A template file starts with a `track-template` HTML comment directive. The rest of the file is the markdown body that will be rendered into a note or journal.

```markdown
<!-- track-template
name: daily
-->
# {{ title }}

date: {{ date }}
kind: {{ kind }}
id: {{ id }}
```

Current directive fields:

- `name`: required. This is the stable user-facing template name used by `--template` and `:Track from_template`.

The directive is removed from generated notes.

## Substitutions

Current substitutions are safe built-ins only:

- `{{ title }}`: target note title. For journals this is the journal name such as `20260606`.
- `{{ id }}`: target note id.
- `{{ date }}`: target date formatted with track's date format, currently `YYYY-MM-DD`.
- `{{ kind }}`: target kind, currently `note` or `journal`.

No executable substitutions are implemented yet. Template syntax such as shell command execution is intentionally unsupported in the current implementation.

## Rendering Rules

Rendering requires the rendered body to contain an H1 title.

For regular notes, the first rendered H1 must equal the requested note title. For journals, the first rendered H1 must equal the journal name. If this check fails, creation fails and no note body is written.

Generated notes get a trailing newline if the rendered template omitted one.

## CLI

Template management commands:

```sh
track template new --name <name> [--id <id>]
track template open --name <name>
track template list
```

`template new` creates a template and fails if another template with the same name already exists.

`template open` returns an existing template by name, or creates a new template with the default body when it is absent.

`template list` scans `template/` and returns parsed templates sorted by name. Each listed item includes `id`, `name`, `path`, and `content_hash`.

Template-backed creation commands:

```sh
track new --title <title> [--id <id>] --template <name-or-path>
track open --title <title> --template <name-or-path>
track journal [--offset <n>] --template <name-or-path>
```

`track open --template` uses the template only when it creates a new note. If the title already resolves to an existing note, the existing note is opened unchanged.

`track journal --template` uses the template only when the journal file does not exist yet. Existing journals are opened unchanged.

The template spec can be a template name or a path. Relative paths are resolved from the vault root.

## Neovim

The Neovim frontend exposes:

```vim
:Track template [name]
:Track from_template [template] [title]
:Track templates
```

`:Track template` opens or creates a template file for editing.

`:Track from_template` creates a regular note from a template. If either the template name or title is omitted, the command prompts through Neovim UI helpers.

`:Track templates` opens a Telescope picker over `track template list`; selecting a template opens its file for editing. The same picker is exported as `require("telescope").extensions.track.search_templates()`.

## Trust

There is no trust database or confirmation step in the current implementation because templates cannot execute commands yet.

Future executable substitutions should require validation and a trust step keyed by template content hash before rendering.
