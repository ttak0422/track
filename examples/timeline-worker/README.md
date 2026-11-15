# Explicit local timeline worker

A Python-standard-library reference adapter for Track's `timeline-project` CLI. It
advances a finite number of sleep-only demo steps, records effort and an authored
report, and handles cooperative pause/resume requests between steps. It does not
launch agents, run supplied commands, access a model, contact a service, or collect
files or conversations. Run it explicitly; Track never starts it automatically.

## Try it in a disposable vault

Build Track from this checkout, then use an isolated machine configuration/cache:

```sh
go build -o /tmp/track-timeline-demo ./cmd/track
export TRACK_CONFIG="$(mktemp -d)/config.yml"
export TRACK_CACHE_DIR="$(mktemp -d)"
export TRACK_VAULT="$(mktemp -d)"
/tmp/track-timeline-demo init
/tmp/track-timeline-demo timeline-project apply --id demo <<'JSON'
{"source":"human","event_id":"create-demo","expected_revision":0,"kind":"project.create","payload":{"title":"Local worker demo","mode":"serial"}}
JSON
python3 examples/timeline-worker/worker.py --track /tmp/track-timeline-demo \
  --vault "$TRACK_VAULT" --project demo --task first-run --owner demo-worker \
  --steps 30 --interval 1 --pause-timeout 60
```

The project must already exist. Use a **new task ID** for each invocation and a
distinct owner label for each worker. Reusing an existing task is rejected rather
than assuming ownership or inventing restart recovery. `--vault` selects an explicit
local vault; existing Track machine configuration remains in effect otherwise.

## Pause and resume

While the worker runs, use the Web or Neovim agent timeline view for the same vault
and project to append `control.request` records. Each request needs a unique `id`,
`task_id: "first-run"`, `owner: "demo-worker"`, and `action: "pause"` or `"resume"`.
Use the snapshot's current revision as `expected_revision`. Refresh if a concurrent
step makes that revision stale. A pause keeps polling until a resume request or the
configured pause timeout. On timeout the process exits with an error and leaves
the task paused; it does not invent a completion or successful report.

Requests are acknowledged first, then the demo loop enters its local safe-point
state, updates task status and records completion. Requests routed to another task
or owner are ignored. A pre-existing acknowledged but incomplete request stops the
demo for manual reconciliation. Owner/source labels are routing information, not
credentials. Completion receipts describe this demo loop only, not other processes.
Control arrival during a step is handled at the next safe point; after the final
step the worker exits and does not service further requests.

## Failure boundaries

The worker compares its complete last accepted task with every refreshed snapshot,
including after a step or while paused. Same-owner external edits stop the worker
without overwriting status or effort. An identical retry returning a newer snapshot
does not adopt another writer’s task; manual reconciliation is required.

- The only subprocess is the explicitly selected Track executable, with fixed
  argument arrays and JSON stdin, never shell evaluation.
- Timeout retries use the same serialized request and event identity. Two timeouts
  produce an uncertain-outcome error; inspect recorded events before restarting.
- A definitive revision-conflict rejection permits at most three refresh/retry
  attempts with new event IDs. Task/control records and relevant control requests
  must remain identical; report references must also still match. Unrelated task
  writes can therefore proceed concurrently, while same-task edits, ownership
  changes and control changes require manual reconciliation. Unsupported operations
  fail without rebasing. Timeouts never trigger revision refresh or a new event ID.
  Keep one writer per task; this is optimistic reconciliation, not a worker lease.
- A crash between acknowledgement, task status and completion can leave an incomplete
  control receipt. These operations are separate ledger events, not a transaction.
- The report references this invocation's actual events, including progress and
  receipts. Its prose is fixed demo text, not generated or captured conversation.

## Tests

```sh
go build -o /tmp/track-timeline-demo ./cmd/track
TRACK_TEST_BIN=/tmp/track-timeline-demo python3 -B -m unittest discover \
  -s examples/timeline-worker -v
```

Integration tests use temporary vaults and exercise real CLI writes, progress/report
references, safe-point controls and owner filtering. Tests also cover unrelated-task conflict
reconciliation, rejection of same-task/control changes, bounded retries and identical
request retries after timeouts. Without `TRACK_TEST_BIN`, only the
unit tests run; integration tests are explicitly skipped.
