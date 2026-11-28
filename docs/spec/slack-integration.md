# Slack client for Neovim and Track integration

Status: Proposed. This document does not add a client, connect to a workspace,
or authorize the storage or publication of Slack data.

## Decision in brief

A Slack client and a Track integration are two separate features. Use a
standalone Neovim frontend with an optional Go Slack adapter. Track should be
able to capture a deliberately chosen Slack reference without depending on
Slack connectivity, knowing the user's Slack credentials, or acting as a chat
service. Keep Slack UI and synchronization out of Track's core and Komado in
the first release. The adapter can be a separately installable companion;
co-location in this repository, if desired, does not imply linking it into the
Track CLI or starting it by default.

## What Track already provides

- [ADR 0001](../adr/0001-go-cli-as-source-of-truth.md) assigns persistent
  behavior to Go and UI to Lua. The Neovim client in
  [lua/track/client.lua](../../lua/track/client.lua) already supplies the CLI
  boundary. Its ordinary helpers wait synchronously; any new Slack-facing
  Neovim action should use an asynchronous call with cancellation/stale-result
  handling rather than wait on a remote API from an editor mapping.
- [Capture](../adr/0039-capture-refile-archive.md) already writes selected text
  into a chosen note or heading and reindexes it. Verify the intended vault
  before invoking `track capture --target ...` with the selected text on stdin.
  Start with the existing command, not a parallel Slack-specific write path.
- [ADR 0082](../adr/0082-retire-built-in-agent-requests.md) removed built-in
  external request delivery; do not reintroduce an agent launcher, Slack bot
  relay, or message-to-task automation to implement capture.
- Timeline is an explicit record of a project's work, not a Slack activity
  mirror. If people later want to use a Slack link as evidence in a Timeline
  report, keep that an explicit authoring action rather than ingesting channel
  events into Timeline. See [ADR 0084](../adr/0084-explicit-agent-timeline-records.md).

## Product boundaries

| Layer | Owns | Does not own |
| --- | --- | --- |
| Neovim plugin | Conversation list, history, threads, draft editing, confirm/send UI, explicit Capture to Track | Slack OAuth secrets, protocol retries, background vault writes |
| Optional Slack Go adapter | Public Web API, OAuth, account/workspace identity, request limits, normalized message/attachment metadata, local transient state | Track storage, indexing, export, Timeline |
| Track | Local vault/target selection, capture, Markdown and notes | Slack credentials, login, notification transport, a private-message mirror |

Keep one adapter contract shared by the UI and terminal tests. A versioned
JSON interface over stdio is enough initially; a per-editor helper process
can keep in-memory state and serialize refreshes. No always-running service,
HTTP bridge, Web/GPUI feature, or shared database is needed for v1. The Lua
client displays messages and calls explicit adapter verbs; it does not parse
raw Slack responses. Treat Slack message text and rich blocks as untrusted data.
They never become executable Ex commands, modelines, or tool instructions.

## Public API and consent

- **Deployment first:** start with a test workspace and an approved internal
  Slack app, one workspace at a time, posting as the consenting user. Do not
  use Slack Web internals, browser cookies, or bot impersonation. Decide any
  wider distribution model separately; Slack's
  [distribution and rate-limit guidance](https://docs.slack.dev/changelog/2025/05/29/rate-limit-changes-for-non-marketplace-apps/)
  distinguishes internal apps from commercially distributed non-Marketplace
  apps, for which history and replies can be limited to one request per minute
  and 15 items. Do not shard requests or credentials to work around that.
- **Login:** browser-based Slack OAuth with
  [S256 PKCE](https://docs.slack.dev/authentication/using-pkce/), `state` and a
  registered localhost callback. Bind only to loopback, time-bound the
  one-shot listener, validate the callback and exact redirect, and exchange
  the code without shipping a client secret. Desktop redirects cannot
  request bot scopes. Store tokens in the OS credential store keyed by app,
  workspace and account; never in the repository, vault, arguments or logs.
  Serialize token rotation and save new refresh credentials atomically; offer
  re-authentication on expiry/revocation and local sign-out/cleanup. Do not
  silently fall back to plaintext storage.
- **Read path:** use [users.conversations](https://docs.slack.dev/reference/methods/users.conversations/)
  for joined conversations, [conversations.history](https://docs.slack.dev/reference/methods/conversations.history/)
  for the selected conversation, and [conversations.replies](https://docs.slack.dev/reference/methods/conversations.replies/)
  for a selected thread. Request only the read/history scopes for enabled
  conversation types. Scope and membership failures are distinct from empty
  results. Resolve user display names lazily if enabled; retain ID fallback.
- **Write path:** add `chat:write` only when posting is enabled. Use
  [chat.postMessage](https://docs.slack.dev/reference/methods/chat.postMessage/)
  with an explicit workspace, destination and parent `thread_ts` for replies.
  Maintain an editable draft until confirmation and guard duplicate submissions.
  If the response is lost, show delivery as unknown and let the user verify in
  Slack rather than automatically retrying. These operations are never part of
  a read, a capture, or an automation.
- **Navigation performance:** fetch the selected conversation on demand; do
  not fan out across all channels. Support cursor paging, bound/consolidate
  concurrent reads, obey HTTP 429 and `Retry-After` per method/workspace/app,
  and discard late UI replies after switching. Keep cache memory-only first,
  keyed by workspace/account. Use opaque string timestamps; deduplicate by
  `(workspace, conversation, message_ts)` and use parent IDs for threads.
- **Deferred:** workspace-wide search needs its own permission and API
  decision; don't index Slack histories in Track FTS. Desktop PKCE login
  alone does not provide Socket Mode. Evaluate live updates separately:
  [Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode/)
  requires app-level credentials and currently cannot be listed in the public
  Marketplace. v1 uses on-demand refresh; no daemon, push notification, or
  full history synchronization. Files, edit/delete, and reactions are later
  work.

## Track-specific capture contract

1. Resolve the selected message permalink with the public
   [chat.getPermalink](https://docs.slack.dev/reference/methods/chat.getPermalink/)
   API. Present the target vault, note and optional heading before writing.
   A link remains accessible only to people with Slack access.
2. Save a permalink and user-authored annotation by default. An excerpt from
   the channel or DM is a separate, explicit choice. Do not silently copy
   other messages, files, member details, or AI summaries.
3. Route a confirmed capture through the existing CLI using stdin, with no
   credentials. Treat Slack excerpts as quoted text, not active Track markup
   or fenced code. Never default to a publishable vault/target for excerpts;
   if a target's publication status cannot be guaranteed, fail closed for
   excerpts and ask for a private destination. A deliberate local capture is
   a snapshot, not synchronization; nothing silently copies later Slack
   edits, and deleted source content is not automatically removed from a
   saved excerpt. Retention and removal policy must be decided before
   supporting broader copies.
4. A permalink-only workflow needs no new Track engine code. Add a focused
   query/validation primitive only if capture cannot reliably identify a
   destination or if reviewed provenance/deduplication needs structured
   metadata. Do not add Slack columns to the core schema speculatively.

## Delivery order and acceptance gates

1. **Read-only first:** approved test app with PKCE login and credential
   storage; Go adapter and opt-in Neovim views. Demonstrate a public channel,
   a private channel, a DM, a group DM, and thread paging in a test workspace.
   Exercise rendering of Japanese, code blocks, multiline text, and oversized
   messages. Mock revoked tokens, missing scopes, 429, dropped connections,
   and quick workspace/view changes. Log no secrets or message bodies.
2. **Confirmed send:** prove the displayed account, channel/DM, and parent
   thread match what Slack receives. Test cancel, late acknowledgments,
   duplicate keypresses, and ambiguous network failures.
3. **Capture to Track:** demonstrate link-only capture to a disposable vault
   with no Slack account configured in Track. Verify cancel and pre-write failures leave the vault unchanged. If capture
   writes a note but indexing fails, surface the partial result and reconcile
   before retrying. Verify that private excerpt capture cannot leak through
   static export, synchronized vaults or published indexes. Add an excerpt
   action only after this privacy gate. Keep the client fully usable without
   Track installed.

Do not treat a mock or the initial prototype as production verification.
Start implementation only after agreeing on the app/credential model and the
repo/package boundary; test a real authorized workspace before enabling any
integration by default.
