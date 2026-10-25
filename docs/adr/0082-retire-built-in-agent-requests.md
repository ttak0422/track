# ADR 0082: Retire built-in agent requests

## Status

Accepted (2026-10-25). Codex/Claude follow-ups below are proposals, not implemented capabilities.

## Decision

Retire the built-in **エージェントに依頼** panel (explain, research, update). Remove its navigation,
note-menu and voice-selection actions, frontend polling/client types, machine recipient projection,
agmsg delivery worker, authenticated claim/result/fail bridge, and request mutation/apply engine.
Remove `track agent` and its obsolete usage directions. Track does not start or message an external
agent through this feature anymore.

Preserve `.track/requests/*.json` in place, including attempts, follow-up context, answers,
saved-note links, before/after content and interrupted states. Keep the version 1 schema and only
read-only `GET /api/requests` and `GET /api/requests/<id>` on a loopback workspace, with their existing
vault selector and pagination. No read performs recovery or writes a note. Creation returns 405;
retired recipient/action endpoints return 410 so stale clients receive an explicit response.
Machine `agents:` configuration remains accepted but inactive, with no credential changes or
migration. The synced vault config still rejects that machine-owned key. Archived records remain
outside static publication.

## Shared dependencies and preserved paths

- The bridge's default workspace address also serves `track markdown`; keep it with that command.
- Loopback checks also protect Markdown handoff and the separate static-app origin; keep them.
- Keep the JSON CLI, note-level execution log and dispatch model, generation snapshots, LSP,
  Neovim integration, portable Markdown copying/export, and local Markdown viewer/handoff.
- Voice transcription, selection search, creation, journal saving, and native speech work are
  independent. Only voice-selection request buttons and their styling are removed here.
- No npm or Go dependency was exclusive to this feature; manifests and locks stay unchanged.

## Focused Codex/Claude direction

The existing common boundary is the CLI (`docs/spec/agent-workflows.md`), rather than a
vendor-specific session API. `track notes`, `search`, `resolve`, `export`, `append`, and generation
snapshots already support an external agent workflow. Copy MD in the note menu and `track markdown`
provide the manual exchange and local reading paths. There is no current Codex/Claude account or
session bridge in main. Repo-local `AGENTS.md` and `.agents/skills` are absent; `AGENT.md` is the
repository guide.

Proposed next steps, each independently reviewable:

1. Verify one small CLI workflow in each client: select a vault explicitly, search/read a note,
   export portable context, propose a change, and review through existing generations/diffs.
   Record only reusable instructions and command contracts; do not duplicate notes or transcripts.
2. If manual handoff is still awkward, add one explicit **Copy context** action containing the
   vault label, note ID/title, selected text, and current content hash. Reuse portable Markdown;
   do not send, start a session, install anything, or persist another content store.
3. If tool discovery is the remaining obstacle, prototype a local stdio MCP adapter with only
   `search`, `read_note`, `resolve`, and `export`, backed by existing engine/CLI contracts and one
   explicitly selected vault. Test it in Codex and Claude Code before adding write tools.
   This is a proposed shared adapter, not a claim that `track mcp` exists today.

Both [Codex MCP](https://developers.openai.com/codex/mcp) and
[Claude Code MCP](https://code.claude.com/docs/en/mcp#option-3-add-a-local-stdio-server) document local
stdio servers. This makes a shared read-only adapter a plausible next experiment; it does not
justify building remote hosting, account linking or persistent credentials. Deep embedded Codex
session control uses the [Codex app server](https://developers.openai.com/codex/app-server) and
would require a separate product decision. The former
[`codex mcp-server`](https://developers.openai.com/codex/mcp-server) entry point has been removed;
do not base a new integration on historical examples of that command. Sources checked 2026-10-25.

## Verification boundary

Regression coverage checks archived v1 fields, read-only pagination, invalid paths/schemas,
missing archive storage, inactive legacy settings, request mutation rejection, interrupted updates
remaining unapplied, static-export exclusion, and removed UI controls alongside ordinary voice,
note-menu and Markdown behavior. No destructive migration, credential revocation, agent-session
activation, publication, or deployment is part of this decision.
