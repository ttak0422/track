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
- `{{ parent }}`: title of the note the creation was triggered from, supplied by `track new`/`track open --parent-path <path>` (track resolves the title from that note's metadata). It is empty when no parent path is given — for example, when following a `[label](<note?template=...>)` action link, the Neovim frontend passes the source note as the parent so the template can reference it.

No executable substitutions are implemented yet. Template syntax such as shell command execution is intentionally unsupported in the current implementation.

## Rendering Rules

The rendered body is saved as note content. It may contain any headings or no headings.

The target title is written to sidecar metadata, not inferred from the rendered body.

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

## Builtin Templates

track ships builtin templates in the binary (provided by the repository, not the vault):

- `default`: applied to new notes.
- `journal`: applied to new journals.

Builtins are resolved by name like any other template, but a user template of the same name in `template/` is resolved first, so creating a `default` (or `journal`) user template overrides the builtin. Builtins are never written into the vault; to customize one, create a same-named user template rather than editing a vault file. A builtin is also usable explicitly, e.g. `--template default`.

## Default Templates

When `track new`, `track open` (on create), or `track journal` (on create) is called with neither `--template` nor an inline body, track applies a default template, resolved in this order:

1. A configured default wins: `default_template` (notes) or `journal_template` (journals) in `config.yml`, overridable for one-off runs by `TRACK_DEFAULT_TEMPLATE` / `TRACK_JOURNAL_TEMPLATE`. The value is a template name or a vault path, same as `--template`.
2. Otherwise the template named `default` (notes) or `journal` (journals) is used — a user template of that name when present, otherwise the shipped builtin. Because the builtins always exist, a bare `track new` / `track journal` applies them by default (a new note gets `# {{ title }}`, a journal additionally gets the date).

The default is always overridable per invocation: an explicit `--template` selects a different template, and an explicit `--body` opts out of the default entirely. A configured default that names a missing template surfaces the usual "template not found" error. The default applies only to the explicit creation commands; the LSP "create note" code action on an unresolved `[[link]]` still writes an empty stub.

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
