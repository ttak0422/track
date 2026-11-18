# Normal-session Timeline integration

This is an opt-in shared skill plus Track CLI, not a wrapper around `claude` or
`codex exec`. The agent submits deliberate task updates through its normal shell
tool. Nothing installs itself, reads session files, streams transcripts or calls
a model. Komado/Web read the same existing Track timeline project.

## One-time setup (review before applying)

Build Track from this branch (`go build -o bin/track ./cmd/track`, Go 1.26+).
Put that binary on PATH. Choose an existing vault and a stable project explicitly:

```sh
export TRACK_VAULT=/absolute/path/to/vault
export TRACK_TIMELINE_PROJECT=demo
track timeline-session init --title 'Demo project'
```

`init` creates only a timeline project inside an existing vault. It does not
create/register a vault. It refuses an existing project with a different title.
Keep this binding in the environment used to launch the intended session. Do not
set it globally for unrelated repositories. The hook never trusts its incoming
`cwd` to choose a destination. Existing `--vault NAME` also selects a vault.

Copy `track-timeline/SKILL.md` into the intended repository's
`.claude/skills/track-timeline/SKILL.md` for Claude, or
`.agents/skills/track-timeline/SKILL.md` for Codex. Review the skill, then merge `WORKSPACE-INSTRUCTIONS.md` into the project
`CLAUDE.md` (Claude) and/or `AGENTS.md` (Codex), preserving existing instructions.
The standing policy selects substantive work automatically after opt-in; **do not
request Timeline or invoke a skill in every conversation**. Instruction loading
and execution remain subject to the client's trust and permission settings.

### Lifecycle context for automatic use

Review and **merge**, rather than overwrite, `claude.settings.json` into the
intended Claude settings, or `codex.hooks.json` into the intended Codex hooks
configuration. Review/approve hooks in the client's own UI where required.
Then opt in in the launching environment:

```sh
export TRACK_TIMELINE_ENABLED=1
claude  # or: codex
```

For automatic normal-session use, install the reviewed hook once. The command
SessionStart hook passes the real session ID to Track; Track hashes
agent + session ID into an opaque run ID and returns it as additional context.
It also supplies a compact, self-contained automatic-work policy, including CLI
examples, so activation does not rely solely on skill discovery. Persistent
workspace instructions reinforce the same opt-in gate. Neither mechanism changes
permissions or guarantees model compliance.
Startup/resume/compact with the same ID reuse a single attachment event. A new
ID (including a fork/clear that changes ID) gets a new run. The hook does not
create tasks or mark any done. No Stop/SessionEnd hook is installed.

Without `TRACK_TIMELINE_ENABLED=1`, the hook exits silently without reading stdin
or opening the store. Set it to `0` or remove the reviewed hook to disable it.
This disables lifecycle integration, **not explicit CLI writes** or the skill;
the persistent policy and skill must skip automatic writes when it is disabled.
Direct user-requested CLI writes remain explicit actions, not automatic tracking.

Malformed/oversized input, an unavailable vault/project, or a store failure
produces a generic stderr diagnostic, a static context invalidating inherited
Timeline run IDs, and exit 0. This prevents a failed startup from silently reusing
a forked conversation's previous run. No hook input is echoed. If the client never
executes the hook at all, that invalidation cannot be delivered; client hook
execution remains a required live acceptance check. A
missing Track binary is a client-level hook error: review PATH or remove the
hook. No background jobs, MCP server, or separate model API calls are involved.
Normal-session context and tool calls can increase ordinary token usage.
Without a successful hook context, automatic tracking skips rather than asking
for a new label every session. The following **manual diagnostic fallback** is
available, but is not the ordinary-session workflow:

```sh
track timeline-session start --agent codex --session my-stable-session-label
# Copy the returned run to subsequent commands. Keep it for resume/compact.
track timeline-session task --run codex-REPLACE_WITH_RETURNED_HASH --task implement --title 'Implement feature' --status active
```

Use a fresh label for independent/forked sessions. This manual fallback cannot
identify the real session for you. Never guess by reading private transcript
files. Claude users select `--agent claude` instead.

## Contracts and limits

- `show` reads the bound project. `task` patches only supplied fields, using
  run-scoped local IDs. Parent/child IDs stay within that run. Identical task
  snapshots are no-ops; a revision conflict requires reread/reconciliation.
- A new session starts a separate task namespace, not automatic handoff of old
  tasks. Review an existing project before continuing; use the low-level API for
  explicit cross-run reconciliation. Tasks reassigned to another owner are not
  overwritten by this CLI.
- `report --event ID --summary TEXT [--uncertainty TEXT]` is optional authored
  text with run-event provenance. Identical retries are idempotent, conflicting
  reuse is rejected. It does not independently validate claims or schedule AI.
- Session IDs are hashed, not anonymized guarantees. Only an opaque ID and fixed
  attachment text are persisted automatically. Transcript path, prompt, tool
  data, source paths and unrelated hook fields are ignored. Explicit text is
  **not** automatically redacted; never submit secrets or sensitive user data.
- Every task mutation is a journal entry. Batch meaningful milestones rather
  than tool-by-tool writes. Existing engine limits (10,000 events / 8 MiB per
  project) still apply. Simultaneous edits can conflict safely.
- Skills are instructions, not a guarantee of model compliance. This is not a
  native Claude/Codex UI extension and cannot guarantee automatic task updates.
  Session hooks supply identity, not semantic task progress.

## Compatibility evidence (2026-11-16)

The build environment has `codex-cli 0.167.0-alpha.4`; Claude is not installed.
An isolated CODEX_HOME reports `Not logged in`; no account credentials were copied
or sign-in attempted. Hook fixtures and CLI behavior are tested without a live
model session. No-invocation model behavior is therefore **not yet live verified**. Neither
interactive-client integration nor a minimum supported client release has been
live-verified; check your installed client's `/hooks` and skill support first.
Do not assume old Codex `notify` has the same contract as SessionStart hooks.

Reviewed official contracts:
- [Claude hooks](https://code.claude.com/docs/en/hooks): command SessionStart
  input and additionalContext, including resume/compact.
- [Codex hooks](https://learn.chatgpt.com/docs/hooks): command SessionStart,
  session_id, startup/resume/clear/compact/fork, hooks.json and trust review.

Product references (not imported dependencies):
- [DeepSeek Harness Timeline](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/experimental/timeline/README.md)
- [claude-timeline](https://github.com/skanehira/claude-timeline)

Both integrate inside their own session runtime. Track instead keeps storage
independent and offers a portable skill/CLI. Their native UI APIs and automatic
model-report schedules are not reproduced here.

Persistent instruction references: [Claude project instructions](https://code.claude.com/docs/en/memory), [Codex AGENTS.md](https://learn.chatgpt.com/docs/agent-configuration/agents-md). These are context instructions, not enforcement or permission grants.

## Live acceptance check (still required)

In a disposable repository and vault, complete the one-time setup above, start
an ordinary interactive client, and ask only: `Add a small sum function and test
it.` Do not mention Timeline or invoke its skill. Confirm that the project gains
a sanitized task plan and verified progress updates. Resume and compact the same
session and confirm the same run and task IDs are reused. Start a new session and
confirm separation. Then disable TRACK_TIMELINE_ENABLED before launching again:
ordinary work must proceed with no automatic Timeline records or repeated setup
questions. Repeat with missing binding/Track and denied shell permissions.

Run this with both supported clients using an authorized test account. CLI tests
assert the delivered policy and journal behavior, not that a model obeys it.
Do not reuse real user tasks or copy account credentials merely to run this check.
