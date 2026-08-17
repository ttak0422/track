# 0072. Agent status reads Claude Code's session records

Status: Accepted

## Context

`track agent ls` and `track agent log` answer "which Claude Code sessions are running right now,
and what is the latest one saying". The source of that state is `~/.claude`: Claude Code writes a
record per session to `~/.claude/sessions/<pid>.json` and an append-only transcript to
`~/.claude/projects/<cwd-slug>/<sessionId>.jsonl`, for its own resume and telemetry. Hooking or
scraping those would duplicate machinery Claude Code already runs, so the records themselves are
the natural source.

Three design questions fall out of that choice. First, the session records also survive dead
sessions and reused pids, and the official CLI's own process inventory mixes in pool spares that
serve no session — so reading the files is not enough, and track must decide for itself which
records describe a live session. Second, agent state is not a note: it does not belong in the
vault index, the sidecars, or the note graph, and `track agent` must work on a machine that has no
vault at all. Third, a session list is only the read half of the feature — sending input to a
session would require a PTY, which track does not own.

## Decision

`track agent` reads Claude Code's session records and adds its own three-stage liveness check:

- `kill(pid, 0)` proves the pid exists (`ESRCH` is dead, `EPERM` passes).
- One `ps -o command=,lstart=` call supplies the process command line and start time. The command
  line must not be the daemon — the only Claude Code process that can never run a session. A
  `claude bg-spare` process that a session record names is the background session itself
  (`kind: "bg"`), so it stays; `bg-pty-host` and warm spares are not filtered either, because they
  never get a session record and a pid reused by them is caught by the start-time check. Only the
  first program token and first argument are trusted, since macOS truncates the command column to
  16 characters when ps combines it with another column. The start time must equal the record's
  `procStart`, which guards against pid reuse; `ps` runs under `TZ=UTC` because Claude Code writes
  `procStart` in UTC while `ps` defaults to local time. Records that fail any check are dropped
  silently, never an error.

The engine takes the home directory as an argument and never opens the vault: agent state is not a
vault asset, so it is excluded from the index and the notes, and `track agent` works with no vault
configured. The transcript is read backward in chunks (never whole) until the requested number of
messages is collected, picking up the newest `ai-title` and `pr-link` lines along the way.

Sending input to a session is out of scope: track has no PTY and does not build one. The PTY for a
session belongs to its process tree (interactive) or to the daemon's PTY host (background), and any
future input feature delegates to the PTY owner rather than re-implementing terminal handling.

## Consequences

- The session list reflects only processes track can positively identify as live sessions; dead
  records and reused pids do not surface, and `kind: "bg"` sessions stay visible because the record
  names the bg-spare process that runs them.
- The `ps` probe is a function field on a small struct, so the whole liveness decision tree is
  unit-testable without spawning processes.
- Forward compatibility is cheap: session records are decoded with plain `json.Unmarshal`, so new
  fields Claude Code adds between releases are ignored instead of dropping sessions.
- A transcript's `aiTitle`/`pr` may be empty for a short `--tail`: the backward scan stops at the
  requested message count and does not chase the file head for a title.
- `track agent` never reads or writes the vault, so it is exempt from vault selection entirely and
  needs no `TRACK_VAULT` setup.