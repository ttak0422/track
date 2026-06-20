---
name: track-project-intake
description: Use when the user asks for project work (fix a bug, add a feature, a TODO) that should be recorded against the project before or instead of doing it immediately. Finds the track note for the current repository/project, creating it if missing, then records the request under its `## Bug` or `## TODO` checklist. Pairs with track-task-runner, which later implements those checklist items.
---

# Track Project Intake

Turn an incoming project work request into a tracked checklist item in the project's track note. This
is the **intake** side of the workflow: it captures and records work. `track-task-runner` is the
**execution** side that later picks items off the same checklists, implements them, and moves them to a
completion note.

Use the `track` CLI as the source of truth for finding, creating, and updating notes. In the track
source repo, `go run ./cmd/track` is an acceptable substitute for `track`.

## When to use

Trigger when the user gives a work request scoped to a project — "fix this bug", "add a feature",
"here's a TODO for X" — and the request should be logged against that project rather than (or before)
being acted on. Typical phrasings: "record this bug for <repo>", "note that we need to …", "add a TODO
to the <project> note".

Do not use this for pure note creation (`track-create-note`), read-only lookup (`track-search-notes`),
or for *implementing* already-recorded items (`track-task-runner`).

## Preconditions

- `track` CLI on `PATH`, resolving against the user's normal vault. `TRACK_VAULT` is only for tests and
  one-off overrides.
- Every command prints single-line JSON; parse stdout and treat `{"error":...}` with exit code 1 as
  failure.
- Titles are link keywords; write `[[Title]]` in bodies to link related notes.

## Workflow

### 1. Identify the project

Derive the project name from the working directory — usually the git repository name (the basename of
`git rev-parse --show-toplevel`, falling back to the current directory's basename). That name is the
note title to look for (e.g. repo `track` → note titled `track`). If the user names the note or project
explicitly, use that instead.

### 2. Find the project's note

```sh
track resolve --term "<project>"      # {"found":true,"note_id":N,"path":"…"} when it exists
```

If `resolve` reports `found:false`, widen with a title search before concluding it is missing:

```sh
track search --scope title --query "<project>"
```

### 3a. Note exists — review, then record

Read the note and inspect its existing checklists so you do not file a duplicate:

```sh
track export --id <note_id>
```

Check the `## TODO` and `## Bug` sections for an item that already covers the request. If one exists,
tell the user and stop (optionally clarify/augment the existing item rather than adding a new one).
Otherwise continue to step 4.

### 3b. Note missing — create it

Create the note idempotently, seeding the two checklist headings the runner expects:

```sh
printf '## TODO\n\n## Bug\n' | track open --title "<project>"
```

`open` is a no-op if the note already appeared between steps 2 and 3, so this is safe.

### 4. Clarify before recording

If the request is vague, **ask the user for the missing details before writing the item** — this is the
"指示を乞う" step. For a bug: reproduction steps, expected vs. actual behavior, affected area. For a
feature/TODO: the concrete outcome wanted and any constraints. Capture enough that `track-task-runner`
could later act on the item without re-interviewing the user.

### 5. Record the item under the right heading

Classify the request: a defect goes under `## Bug`, new work or an enhancement under `## TODO`. Write it
as a single unchecked checklist line, in the note's existing language:

```text
- [ ] <concise, actionable description of the request>
```

`track append` only adds to the end of a note, so it cannot place a line under a specific heading. To
file the item under `## Bug` or `## TODO`, **edit the note body directly** (path from step 2/3): insert
the `- [ ]` line beneath the target heading, after any existing items in that section. (This mirrors how
`track-task-runner` removes lines in place — track has no line-insert command.)

For a brand-new note whose section is still empty, the line is simply the first entry under the heading.

### 6. Reindex

Keep search, links, and backlinks consistent:

```sh
track reindex
```

## Verify

Confirm the item landed under the intended heading:

```sh
track export --id <note_id>     # the new - [ ] line appears under ## Bug or ## TODO
```

## Handoff

Once recorded, the item is ready for `track-task-runner` to implement and move to a dated completion
note. If the user wants the work done now rather than just logged, record it first (so nothing is lost),
then proceed — or hand off to `track-task-runner` to sweep the checklist.
