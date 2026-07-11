# 0035. Task management on checkbox lines: named states, inline tokens, sidecar log

Status: Accepted

## Context

The engine could only toggle `- [ ]`/`- [x]` checkboxes (`track toggle`). Real task work needs more:
in-progress and blocked states, priorities, scheduled/deadline dates, completion timestamps, and a
board view — without turning notes into a database. Notes must stay plain Markdown that renders
acceptably in any viewer, and every surface (CLI, web workspace, static export, indexer) must share
one parser and one write path.

Prior art: Org mode keywords (`TODO`/`DONE` words before the headline, `SCHEDULED:`/`DEADLINE:`
planning lines, `[#A]` priorities, `[2/5]` statistics cookies, LOGBOOK drawers) and Obsidian custom
checkboxes (`- [/]`, `- [-]` — the box character carries the state) plus the Tasks plugin's inline
emoji tokens.

## Decision

- **The box character is the state.** A task is a GFM checkbox item whose bracket character names a
  state: default set `TODO [ ]`, `DOING [/]`, `WAITING [?]`, `DONE [x]`, `CANCELLED [-]`. This is
  the Obsidian custom-checkbox convention, chosen over Org-style keyword words because it degrades
  gracefully: `- [ ]`/`- [x]` remain standard GFM, other states still render as list items, and no
  extra token competes with the task text. States are configurable per vault (`task_states`:
  unique name, unique single-character marker, done-family flag); done-family membership — not a
  hardcoded state name — drives completion behavior, so custom sets get stamps for free.
- **Metadata is inline bracket tokens.** `[#A]` priority (Org's syntax verbatim), `[sched:YYYY-MM-DD]`
  and `[due:YYYY-MM-DD]` dates, `[done:YYYY-MM-DD]` completion stamp. One shared token shape
  (`[key:value]`-ish, short, ASCII) rather than Org planning lines (a second line per task does not
  fit list items) or Tasks-plugin emoji (grep-hostile, input-method-hostile). Dates are fixed
  `YYYY-MM-DD`, deliberately independent of the vault's display `date_format`, so task lines are
  portable and lexically comparable in SQL.
- **Completion stamps are dates in the body; the full history lives in the sidecar.** Entering a
  done-family state writes `[done:date]` on the line (visible where the reader is); leaving removes
  it. Every transition appends `{at, line, from, to, text}` to the note's sidecar metadata
  (`task_log`, metadata v5) — the existing per-note sidecar already carries babel results and page
  metadata, so history survives without an Org-style LOGBOOK drawer polluting the body.
- **Progress cookies recompute on engine writes only.** `[n/m]`/`[p%]` on a heading counts tasks to
  the next same-or-shallower heading; on a list item, its deeper-indented children. Recompute
  happens on every engine task mutation, not on a file watcher — hand edits are reconciled the next
  time the engine touches the note.
- **One write path.** `task.SetState` (pure string manipulation) under `note.ApplyTaskState` (file +
  sidecar) serves `track task set`, `track toggle` (now the two-state shorthand: first open ↔ first
  done-family state), and the web board's `POST /api/task`. Tasks index into a `tasks` table
  (schema v3) feeding `track tasks` filters (`--state`, `--due`, `--overdue`, `--sort priority`).
- **The board is a fence.** A ` ```taskboard ` fence renders the note's tasks as a kanban board,
  like `mermaid`/`viewspec` fences render diagrams and charts: the note itself opts into the view,
  no per-note config. Live workspace boards drag cards through the state-set API; the static export
  bakes the parsed tasks into the note JSON and renders read-only.
- **Fenced code is invisible to the task parser.** Task notation quoted inside a code fence never
  parses as a task, counts toward a cookie, or accepts a state change — same masking rule the static
  export applies to asset references.

## Consequences

- Notes remain valid GFM; only `[/]`, `[?]`, `[-]` boxes render as plain list items in foreign
  viewers, which is the accepted cost of keeping the state inside the box.
- Line numbers are the task identity (`--line`, the board's drag payload). They are stable only
  against the file on disk, so the web board renders from saved note state, and concurrent editors
  can race a stale line number — acceptable for a single-user vault.
- The sidecar log grows without bound; if it ever matters, pruning can piggyback on the existing
  metadata versioning.
- A configured state set is per vault, not per note; the published static site always shows the
  state set the site was built with.
