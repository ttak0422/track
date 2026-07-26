# 0058. Fix the task state set

Status: Accepted

## Context

ADR 0035 made the task state set configurable per vault (`task_states`: a name, a
single-character marker, and a done-family flag per state). Nothing about the
state machine needs the set to be data — done-family membership drives the
completion stamp, so a custom set worked without special-casing — and that is
why it was cheap to offer.

What it cost is visible everywhere the set is *read*.

**The marker is one character.** With five states the default set already spends
` `, `/`, `?`, `x`, `-`. A vault that re-spelled them gives a pasted line a
different meaning than the vault it came from: `- [?]` is WAITING here and
REJECTED there. Cross-vault references (ADR 0053) and `track mv` move text
between vaults, so this is not hypothetical.

**Every renderer needs the set, so it has to be shipped to every renderer.** The
Go engine parses with it; the web workspace needs it to draw a checkbox at all,
so `/api/tasks` carried a `states` array on every response and the bundle baked
one into every note's JSON; the frontend still kept a hardcoded copy for the
cases with no note context (previews, include embeds), and read
`tasks.states.length > 0 ? tasks.states : defaultTaskStates` at three call sites;
the Neovim plugin has its own `task_chars`/`task_done_chars`/`task_glyphs`, which
the docs told the user to "align with the vault's `task_states` when customized".

So the set was already fixed in practice — a user who changed it got a vault the
plugin decorated wrongly until they hand-synced two more lists. The
configurability was real only in the engine, and the wire field made the other
surfaces *look* like they were being told the truth.

## Decision

The task state set is fixed for every vault: `TODO [ ]`, `DOING [/]`,
`WAITING [?]`, `DONE [x]`, `CANCELLED [-]`.

- `task_states` is removed from the config, along with `Config.TaskStates`,
  `task.StatesOrDefault`, and `task.ValidateStates`.
- The set lives in one place per language: `task.States()` in Go and
  `web/src/taskStates.ts` in the frontend. Neither is served to the other.
- `task.Set` (the `/api/tasks` payload and the static bundle's note JSON) drops
  its `states` field and carries only `items`.
- Every engine function that took `states []State` — `Parse`, `At`, `SetState`,
  `StateNamed`, `NewSet`, `FirstStates` — takes it no longer. `FirstStates` also
  drops its error return: the set is known to hold a done and a not-done state.

## Consequences

- A task line means the same thing in every vault, which is the property that
  matters once notes move between them.
- Three copies of the set remain — Go, TypeScript, Lua — but they are now
  constants that must match rather than a config value plus two approximations of
  it. `TestStateSet` holds the invariants `ValidateStates` used to check (unique
  names, unique single-character markers) against the one real set.
- A vault that had customized `task_states` fails to load until the key is
  removed, and its notes' markers read as whatever the fixed set says they are.
  Task lines whose marker is not in the set parse as plain list items, which is
  the existing behaviour for an unknown marker, not a new failure mode.
- Adding a state is now a code change in three places. That is the intended
  price: a sixth state has to be drawn by every surface anyway, so it was never
  really a config change.
