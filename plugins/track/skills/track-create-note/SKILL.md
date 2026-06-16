---
name: track-create-note
description: Use the track CLI to create or open linked Markdown notes, journals, and template-backed notes in a track vault. Use when the user asks to take a note, create/open a note by title, create today's/yesterday's/tomorrow's journal, create a note from a template, or manage note templates before creating notes.
---

# Track Create Note

Use the `track` CLI as the source of truth for note creation, IDs, sidecar metadata, indexing, and link resolution. In the track source repo, `go run ./cmd/track` is an acceptable substitute for `track`.

## Preconditions

- Prefer the user's normal track config. `TRACK_VAULT` is for tests and one-off overrides.
- Commands print single-line JSON. Parse stdout as JSON and treat `{"error":...}` with exit code 1 as failure.
- Titles are link keywords. Use `[[Title]]` in bodies to link related notes.
- Body text may start with a Markdown heading; the note title is stored in sidecar metadata.

## Create or Open Notes

Create a new note and fail if the title already exists:

```sh
track new --title "Title" --body "Markdown body" --tag project
```

Create or open idempotently by title:

```sh
track open --title "Title" --body "Initial body used only when created"
```

Open or create journals:

```sh
track journal              # today
track journal --offset -1  # yesterday
track journal --offset 1   # tomorrow
```

Append to an existing note:

```sh
track append --title "Title" --body "Additional Markdown"
track append --id 123 --tag project
```

## Template-Backed Creation

Create or open templates before using them:

```sh
track template new --name meeting
track template open --name meeting
track template list
```

Template files live under `template/` and start with a directive:

```markdown
<!-- track-template
name: meeting
-->
# {{ title }}

date: {{ date }}
kind: {{ kind }}
id: {{ id }}
```

Supported substitutions are safe built-ins only: `{{ title }}`, `{{ id }}`, `{{ date }}`, and `{{ kind }}`. The directive is removed from generated notes.

Use a template when creating notes or journals:

```sh
track new --title "Project meeting" --template meeting
track open --title "Project meeting" --template meeting
track journal --offset 0 --template daily
```

`--body` and `--template` are mutually exclusive. `track open --template` and `track journal --template` use the template only when they create a new file; existing notes/journals are returned unchanged.

## Verify

After creation, confirm the note when needed:

```sh
track resolve --term "Title"
track export --title "Title"
```
