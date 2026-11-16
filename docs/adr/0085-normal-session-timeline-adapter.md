# ADR 0085: Normal-session Timeline adapter

Status: Proposed

## Context

ADR 0084 keeps explicit agent records in the Track engine. Its `codex exec --json`
example is useful for metadata ingestion but is not a suitable everyday bridge
for an interactive Claude or Codex session. DeepSeek Harness Timeline and
skanehira/claude-timeline integrate tools inside their own session runtime; their
native UI/module APIs are not a portable interface across both clients.

## Decision

Add `timeline-session` as a thin CLI over the existing journal, plus a shared
opt-in skill and optional official command SessionStart hook examples. Do not
wrap either CLI, install settings, read transcripts, introduce an agent launcher,
start an MCP service, or schedule model calls.

Require explicit vault/project binding. Derive a run from a domain-separated
agent/session hash, keep it stable over resume/compact, and scope task IDs to the
run. Record attachment once with a deterministic event identity. New session IDs
create new runs. Hooks fail open and remain inert without explicit opt-in.
They supply identity/context only, never infer task completion.

Task flags patch existing records and preserve omitted values. Exact resulting
state is a no-op. Revision conflicts require caller reconciliation, except the
immutable attachment can safely retry. Optional authored reports have explicit
retry IDs and event provenance. Existing engine validation, locking, limits,
permissions and renderer remain the source of truth.

## Consequences

Normal agent sessions can update the same project shown by Komado without a
stdout pipe. The skill is a behavioral instruction, not guaranteed telemetry.
Cross-session task handoff remains an explicit low-level reconciliation, not an
automatic merge of similarly named tasks. Live client/version compatibility must
be tested separately from CLI and hook-contract fixtures. No native plugin UI,
automatic summaries, or extra model budget is implied.

Only deliberately submitted text and opaque run IDs persist. Hashing session IDs
is not a guarantee of anonymity; explicit summaries are not automatically
redacted. The adapter must not use incoming hook cwd/transcript fields to select
or discover resources.

See [integration and official references](../../examples/session-timeline/README.md).
