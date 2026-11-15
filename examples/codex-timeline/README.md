# Codex lifecycle importer

An opt-in Python-standard-library adapter for an explicitly piped `codex exec --json`
stream. It writes lifecycle metadata into an existing Track timeline project through
the CLI. It does not start Codex, call a model, discover sessions, read conversation
files, or control an agent. There is no interactive terminal UI.

## Run

Build Track and create a timeline project first. Select the intended vault explicitly.
Use one distinct run ID for each Codex invocation and a dedicated owner label. This
example assumes that project `demo` already exists in `/absolute/path/to/vault`:

```sh
go build -o /tmp/track-timeline ./cmd/track
# Bash or zsh: preserve a failure on either side of the pipeline.
set -o pipefail
codex exec --json --sandbox read-only 'Review the public API without changing files' |
  python3 examples/codex-timeline/adapter.py \
    --track /tmp/track-timeline --vault /absolute/path/to/vault \
    --project demo --owner codex-import --run-id review-001
```

The operator starts Codex separately and chooses its sandbox/authentication. Normal
Codex execution can contact its configured service; the importer itself only runs
the local Track CLI. Do not add `tee` or redirect the stream into a transcript merely
for this integration. Codex's own session persistence and stderr are outside this
adapter's control.

## Recorded data

- A generic execution task and one child per observed turn, identified by a hash of
  the explicit run ID and a turn ordinal. IDs are scoped to that invocation stream,
  not durable Codex turn identities across resumed invocations.
- Turn lifecycle states: active, completed, or blocked after observed failure or
  incomplete EOF. Completed means the turn ended successfully, not that all requested
  development work or every plan item was completed.
- Counts of completed command/file-change/MCP item events (deduplicated by item ID
  within a turn), separated by their fixed
  type/status labels, and the latest plan's completed/total count.

No thread IDs, item IDs, plan text, prompts, agent messages, reasoning, commands,
file paths, tool names, arguments, output, error text, token usage, or raw events are
persisted. Each terminal turn also gets a fixed metadata-only report citing its actual ledger events
and their timestamps. These periods are the Track ingestion/observation window, not
original Codex execution times: this JSONL schema carries no source timestamps.
No model-generated summary is collected.

No source text is printed in import errors. Unknown event/item types are
ignored. Recognized malformed envelopes stop the importer. Generic task titles and
fixed metadata are deliberate: this is not an automatically generated narrative report.

There are no inferred per-step identities, priorities, weights, estimates, or effort
measurements. Task weights are one and effort stays zero. Plans may reorder or replace
steps and provide no stable step IDs, so their counts belong in detail rather than
fabricated child tasks. The importer never acknowledges pause/resume: observing stdin
cannot pause or resume the upstream Codex process.

## Replay and failure

Replay only the **same invocation stream**, from its beginning, using the same run ID,
project, and owner. Event identities derive from run ID, input line position, and
operation. Identical projected actions are deduplicated by Track; altered projected
content under an existing identity is rejected. Excluded private fields are neither
hashed nor persisted and therefore are not part of replay comparison.

An uncertain write is retried once with identical JSON. It never receives a new event
ID. A definitive revision conflict permits at most three bounded-backoff retries after
checking that the affected task, parent, controls and report provenance are unchanged.
Only the expected revision changes; the logical action and event ID remain identical.
Same-target changes stop rather than silently rebase. New writes refuse to overwrite
an existing target whose latest task update came from outside this run. Do not reuse
run IDs for another invocation, concatenated streams, or a resumed session. This is
not a worker lease; keep one importer per run. A truncated prefix cannot append new
actions ahead of later records already imported under that run.

A clean EOF after an active turn records blocked, not done. Malformed input, resource
limits, external termination, or Track errors may leave the last active state intact;
that is not proof that Codex is still running. No partial input is rolled back. An
importer failure may close the pipe and affect its upstream producer: inspect the
pipeline exit status and Track records before retrying. Do not restart Codex merely
to retry a failed import, since that would create different work.

Limits: 1 MiB per JSONL line, 16 MiB per stream, 10,000 lines, and 1,000 steps in a plan.
Track's journal/event limits apply independently. No journal or transcript rotation
is performed. Keep real work streams within these bounds.

## Protocol compatibility

The [official non-interactive documentation](https://learn.chatgpt.com/docs/non-interactive-mode)
describes the JSONL lifecycle and item events. This adapter's fixtures use the official
[`ThreadEvent` schema](https://github.com/openai/codex/blob/b016cd18472588606c04af565528c4cd6746a41f/sdk/typescript/src/events.ts)
and [`TodoListItem` / typed item schemas](https://github.com/openai/codex/blob/b016cd18472588606c04af565528c4cd6746a41f/sdk/typescript/src/items.ts)
at revision `b016cd18472588606c04af565528c4cd6746a41f` (2026-11-15).
A plan item has a list-level ID and `items` with `text` and `completed`; turn lifecycle
events have no turn ID. The
[Rust event processor](https://github.com/openai/codex/blob/main/codex-rs/exec/src/event_processor_with_jsonl_output.rs)
maps plan states to completion booleans and closes the todo list when the turn ends.

This is a narrow schema compatibility target, not a promise of compatibility with all
Codex versions. Additive unknown events are ignored; changed recognized schemas fail
closed. The installed CLI reported `0.167.0-alpha.4`, but no live model run was performed.
Tests use synthetic schema-shaped fixtures, never private session recordings.

## Tests

```sh
go build -o /tmp/track-timeline ./cmd/track
TRACK_TEST_BIN=/tmp/track-timeline python3 -B -m unittest discover \
  -s examples/codex-timeline -v
```

Integration tests create disposable vaults and exercise real CLI persistence, exact
replay, conflicting reuse, external-edit protection, concurrent independent imports,
metadata-only report provenance, repeated item completion, multiple turns, incomplete/failing
streams and exclusion of sensitive sentinel strings. Without `TRACK_TEST_BIN`, real
CLI tests are explicitly skipped.
