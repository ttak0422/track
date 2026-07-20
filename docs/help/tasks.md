# Tasks

A task is a Markdown checkbox line whose box character carries a named state. The notation stays
plain GFM — `- [ ]` and `- [x]` mean what they always meant — and extra states, priorities, and dates
are ordinary inline text, so a note remains readable anywhere Markdown renders.

Back to [[track]]. The commands referenced here are part of the [[CLI]].

## Task states

The character inside the brackets names the state (the Obsidian-style custom-checkbox convention):

```md
- [ ] TODO — not started
- [/] DOING — in progress
- [?] WAITING — blocked on someone else
- [x] DONE — finished
- [-] CANCELLED — will not happen
```

DONE and CANCELLED are *done-family* states: moving a task into one stamps a completion date on the
line, and moving it back out removes the stamp again.

The state set is configurable per vault in `config.yml`; the set above is the default. Each state
needs a unique name and a unique single-character marker, and `done: true` marks it as
completion-family:

```yaml
task_states:
  - { name: BACKLOG, char: " " }
  - { name: ACTIVE, char: "/" }
  - { name: SHIPPED, char: "x", done: true }
```

Every transition is appended to the note's sidecar metadata (`task_log`), so the history of when a
task moved between states survives without timestamps piling up in the note body.

## Priorities and dates

Bracket tokens anywhere on the task line add metadata:

| Token | Meaning |
| --- | --- |
| `[#A]`, `[#B]`, `[#C]` | Priority, `A` highest. Any single letter works. |
| `[sched:2026-07-18]` | Scheduled — the day you plan to work on it. |
| `[due:2026-07-24]` | Deadline — the day it must be finished. |
| `[done:2026-07-09]` | Completion date, written automatically on entering a done-family state. |

Dates are always `YYYY-MM-DD`, independent of the vault's display date format, so task lines stay
portable between vaults.

```md
- [ ] Write the report [#A] [due:2026-07-24]
- [/] Draft the slides [sched:2026-07-18]
```

## Progress cookies

A `[n/m]` or `[p%]` cookie on a heading or on a parent list item tracks the tasks beneath it: a
heading counts every task until the next heading of the same or a shallower level, a list item
counts its deeper-indented children. The engine recomputes every cookie whenever a task changes
state, so the numbers never go stale.

```md
## Sprint 12 [1/3]

- [x] Ship the parser
- [ ] Refresh the screenshots
- [ ] Announce it [2/2]
  - [x] Draft the post
  - [x] Review the post
```

## CLI

| Command | Purpose |
| --- | --- |
| `track task set (--id N \| --title S \| --path P) --line N --state NAME` | Move the task on line `N` into a named state. |
| `track tasks [--state A,B] [--due YYYY-MM-DD] [--overdue] [--sort priority]` | List indexed tasks as JSON, across the vault or one note. |
| `track toggle (--id N \| --title S \| --path P) --line N` | Two-state shorthand: flip between the first open and first done state. |

`track task set` rewrites only the one line: it swaps the state marker, stamps or clears the
`[done:...]` token, recomputes parent progress cookies, logs the transition in the sidecar, and
reindexes the note. `--line` is the 1-based line number reported by `track search` and
`track export`.

`track tasks` filters compose: `--state TODO,DOING` keeps only those states, `--due 2026-07-24`
keeps open tasks due on or before that date, `--overdue` keeps open tasks whose deadline has
passed, and `--sort priority` orders open tasks first, `[#A]` before `[#B]` before unprioritized,
ties broken by deadline.

```sh
track task set --title "Release plan" --line 12 --state DOING
track tasks --overdue --sort priority
```

## Task board

A ` ```taskboard ` fence in a note renders the note's tasks as a kanban board — one column per
state, one card per task. In the live [[Web workspace]] a card drags between columns (or moves via
its state select), which runs the same engine write path as `track task set`. On a published static
site the board renders read-only.

## Example

A full task section as it is written in a note — the heading cookie counts the tasks below it, and
the `taskboard` fence renders them as a board:

````md
### Release checklist [2/5]

- [/] Write the announcement post [#A] [due:2026-07-24]
- [ ] Refresh the screenshots [#B] [sched:2026-07-18]
- [?] Wait for the mirror to sync
- [x] Tag the release candidate [done:2026-07-09]
- [-] Rewrite the changelog generator

```taskboard
```
````
