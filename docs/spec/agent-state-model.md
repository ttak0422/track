# Agent State Model Specification

This document specifies how track tracks agent executions over the vault: the durable state that
distinguishes *what was asked* from *who is doing it* and *which attempt*, and the invariants that
keep a retried or restarted execution from corrupting settled state.

It is a specification only. No engine code is specified or required by this document, and the mobile
and remote-operation surfaces are out of scope.

## Purpose

track's agent-facing contract ([agent-workflows.md](agent-workflows.md)) describes the CLI an agent
uses, but the vault holds no record of the *execution* itself. An agent that is killed, retried, or
run twice over the same note leaves no trace of which attempt produced which change. Generations
([ADR 0025](adr/0025-generation-snapshots-and-trash.md)) bound a run for review and undo, but they
say nothing about the run's shape: there is no durable notion of *a task*, *an attempt*, or *a
message between the coordinator and the worker*.

This document defines those notions as a Run/Task/Dispatch/Message state model for multi-agent
execution over a single vault. The mapping is deliberate and one-directional: the four model
entities land on track concepts, not the other way around.

## Source model

The four entities and their statuses (row types, the dispatch settlement path, the DAG
convergence check, and the mailbox delivery ack design):

- **Run** — the top-level objective that owns a set of tasks. It records the coordinator handle and
  progresses `idle → running → completed | failed`.
- **Task** — a unit of work with a spec, a status
  (`pending | ready | dispatched | completed | failed | blocked`), optional `deps` (a DAG edge set),
  and a result. Tasks form a DAG via `parent_id` and `deps`.
- **Dispatch** — one concrete attempt to execute a task. A task can have many dispatches (retries);
  each is a distinct row with its own status
  (`pending | dispatched | completed | failed | circuit_broken`), a failure count, and timestamps
  (`dispatched_at`, `completed_at`, `last_heartbeat_at`).
- **Message** — a mailbox message between two terminal handles, typed
  (`status | dispatch | worker_done | merge_ready | escalation | handoff | decision_gate | question | heartbeat`),
  with `from_handle`, `to_handle`, `priority`, `thread_id`, `read`, and a monotonic `sequence`.

The four invariants this document carries over are the **task+dispatch dual key**, **stale-completion
rejection**, **DAG convergence**, and the **mailbox delivery ack**.

## Mapping to track

| Model | track | Meaning |
| --- | --- | --- |
| Run | a run note (or a dedicated `.track/` record) | the objective that owns a set of dispatched work |
| Task | a note | a unit of work; the note id is the task id |
| Dispatch | an agent execution | one attempt by one agent to work on one note |
| Message | a mailbox entry | a coordinator/worker message addressed to a mailbox |

Two of the four already have a home in the vault; two do not. The rest of this section fixes each
entity's identity and home, then states the invariants.

### Task = note

A task is a note. The note id is the task id, so a task is addressable with every existing targeting
form (`--id`, `--title`, `--path`). A task's spec is the note's body or the instruction that produced
it; a task's result is the body change the dispatch made, which `gen status` already reports
file-level (added/changed/deleted paths) relative to the cursor generation.

Note-level execution status is distinct from line-level task state. The checkbox states
`TODO/DOING/WAITING/DONE/CANCELLED` ([ADR 0058](adr/0058-fixed-task-state-set.md)) describe lines
inside a note. An execution status describes the note as a unit of dispatched work and is not one of
the five checkbox states. The two must not be conflated: a note whose dispatched work is `completed`
can still contain `WAITING` or `TODO` lines.

### Dispatch = agent execution

A dispatch is one attempt by one agent to work on one note. It is the concept track currently lacks,
and the one this document is most about. Because a task can be retried, a note can have several
dispatches; each dispatch is a distinct record with its own identity and status. A dispatch needs a
stable id (not the note id) so that two attempts over the same note are distinguishable.

A dispatch records, at minimum: its id, the note id it targets, its status
(`pending | dispatched | completed | failed`), a failure count and last failure, and the timestamps
that mark when it was dispatched, when it completed, and when it last reported liveness (heartbeat).
The status set is the note-level analog of the model's dispatch status, reduced by dropping the
federation/remote states (`circuit_broken` is out of scope with mobile/remote).

### Message = mailbox

A message is a mailbox entry between a coordinator and a worker. Mailbox addressing follows this
form: a mailbox is the run's address, the dispatch's address, or a bare agent handle. In track terms
the handle is a marker the executing agent adopts for the session, and the mailbox record is the
durable counterpart of the CLI contract's one-shot reports (`worker_done`, `heartbeat`).

### Run

A run is the objective that owns the dispatched work. track has no run today. The natural home is
either a run note whose sidecar carries the run identity and coordinator handle, or a dedicated
`.track/` record keyed by run id. Whichever home is chosen, the run is what links the set of
dispatched notes together and holds the coordinator identity, so the implementation PR must pick one
and record it. This document fixes only the identity requirement: a run has an id, an objective, and
a status (`idle | running | completed | failed`).

## Invariants

### Dual key: task + dispatch

Every completion and heartbeat report is keyed by **both** the task id and the dispatch id, never by
task id alone. The task id alone names *what*; the pair names *which attempt*. A retried note has
multiple dispatch records, so a report that names only the note cannot say which attempt it belongs
to. A late completion from a failed first attempt, or a straggler heartbeat, must not be able to
complete or refresh the second attempt.

In track terms the task id is the note id and the dispatch id is the execution record id. A report is
settled only when both resolve and the dispatch id names the dispatch that is currently active for
that note.

### Stale-completion rejection

Settlement rejects, rather than silently applies, a report that does not name the live attempt. The
rejection reasons carried over from the source model are:

- `unknown_task` — no note with that id.
- `unknown_dispatch` — no execution record with that id.
- `task_dispatch_mismatch` — the execution record belongs to a different note.
- `inactive_dispatch` — the note or the dispatch is already settled, or another active dispatch still
  owns the note.
- `stale_dispatch` — the dispatch is not the current dispatch for the note.

The settlement is guarded by a compare-and-swap style write: the status transition succeeds only
where the stored status is still the pre-transition status. A dispatch whose status changed while a
report was in flight is rejected, not overwritten. This is the line-level `--expect` assertion of
[agent-workflows.md](agent-workflows.md#target-selection) lifted to execution records.

### DAG convergence

A run over a set of notes converges when every note has reached a terminal status
(`completed | failed`). Three outcomes are distinguished: the set is `empty` (no notes), `all-done`
(every note terminal), or `active`. A run is **stuck** when no note is `active`
(`pending | ready | dispatched`) yet some are `blocked`: the blocked notes wait on dependencies that
can never be satisfied. The stuck case must be reported, not silently idled.

Note-level dependencies map onto the existing link graph and the `up::` hierarchy relation
(`track nav`): a note is `blocked` when its dependency is not yet terminal. The convergence check is
a read over the note set, so it belongs with the engine, not the CLI.

### Mailbox delivery ack

A message that reaches a worker is delivered under an ack, not assumed delivered. The carried-over
states are `outstanding → acknowledged → fenced`. The ack is durable (recorded, not in-process) and
scoped to a **consumer generation**, so a worker that restarts cannot receive a replay of messages it
already acknowledged, and a mailbox never has more than one outstanding delivery. The `fenced` state
is the terminal barrier that keeps a late message from an old generation out of the new one.

This is the same shape as the existing durability rule for note metadata: the authoritative record is
durable and versioned, the in-process view is disposable. A mailbox entry, like a sidecar, must
survive a restart and must not be reconstructed from a transcript.

## Relationship to existing assets

### Generations (`gen`)

Generations are a review and undo boundary, not an execution record. They are a git-release model: a
generation is an immutable save point and the working vault is a disposable tree. They answer "what
did this run change, and can I take it back" via `gen increment`/`gen undo`/`gen status`.

The state model is orthogonal. A dispatch lifecycle has many steps that do not cut a generation, and
a generation does not record which dispatch produced the change. The two compose: a dispatch's
approval boundary can be expressed by bracketing it with `gen increment`, and `gen status`'s
added/changed/deleted set is the file-level evidence a completion report should not have to
self-report. But the execution record — which attempt, when, with what status — is what `gen` lacks
and this document adds. `gen` remains the undo boundary; the state model adds the *who* and *which
attempt*.

### Sidecar `task_log`

The sidecar `task_log` (metadata version 7, see `internal/track/note` and the `task.LogEntry`
shape) records line-level state transitions as `{at, line, from, to, text}`. It is the existing
proof that track already stores *transitions*, not just *states*.

A dispatch settlement is the same pattern at a different granularity. Where `task_log` records a
checkbox line moving `TODO → DOING`, the execution log records a dispatch moving
`dispatched → completed`, keyed by note id **and** dispatch id. The two logs coexist: `task_log`
stays line-scoped inside one note, the execution log is note-and-attempt-scoped. Both are append-only
transitions stamped with a time, and both are authoritative sidecar data, not rebuildable from the
body.

### Sidecar metadata and durability

The execution record follows the storage rule already in force ([storage.md](storage.md)): the
authoritative per-note state lives under `.track/notes/<id>.yaml`, the SQLite index is a rebuildable
cache, and neither may be reconstructed from the markdown body. An execution record written only to
the index, or derived from a transcript, would vanish on reindex or restart — exactly what the
durability section of `storage.md` forbids for titles and tags. Wherever the execution record and the
mailbox live, they are authoritative files under the vault, backed up like note bodies and sidecars,
never the cache.

The run record is the one piece whose home is not fixed here: a run spans many notes, so a
per-note sidecar cannot hold it. It is either the sidecar of a designated run note or a dedicated
`.track/` record, decided in the implementation PR, and it is authoritative data in either case.

## Non-goals

- **Engine implementation.** This document fixes a model and its invariants, not Go code, schema, or
  CLI verbs. Implementation is a separate change.
- **Mobile and remote operation.** The federation and remote-dispatch states (`circuit_broken`, the
  remote attachment and relay tables, remote wire compatibility) are out of scope. This model assumes
  a single local vault and a single machine.
- **Scheduling or autonomy.** Whether a coordinator is a human or an agent, and when it dispatches, is
  not specified here; only the state the execution leaves behind is.
