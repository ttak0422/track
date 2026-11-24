# Track Timeline in Codex

Use the [Track Timeline skill](https://github.com/ttak0422/track-lab/blob/7a010f333bd19ece6b9cd588fb71446f8f8eda49/plugins/task/skills/track-timeline/SKILL.md) when
you want Codex to track a piece of work. It records intentional updates through
the local Track CLI; Komado and Web read the same existing timeline project.

## Quick start

1. Make sure `track` is on the PATH used by Codex (`command -v track`).
   If needed, build it from this checkout with Go 1.26 or newer:
   `go build -o bin/track ./cmd/track`, then put that binary on PATH.
2. Install the [track-lab task plugin](https://github.com/ttak0422/track-lab/blob/7a010f333bd19ece6b9cd588fb71446f8f8eda49/plugins/task/README.md#install) to use
   the skill in Codex or Claude. See [track-lab#57](https://github.com/ttak0422/track-lab/pull/57)
   for the migration and availability. No AGENTS.md change or hook is needed.
3. Ask once in the session, for example: **`$track-timeline` Track the progress
   of this work in the existing `work` vault and `demo` project.** A clear
   natural-language request to track with Track works too if your client selects
   the skill. When the destination is ambiguous, Codex asks which existing vault
   or project to use. To start a new project, say so. The skill never creates a
   vault or changes its registration.
4. Work normally in that conversation. Codex attaches once and updates the same
   task IDs at meaningful milestones. It returns the project ID; use
   `:KomadoTimeline demo` in a Komado setup that views the same vault, or
   `:Track timeline demo` in Neovim. For an independent new conversation, ask
   to track once there as well. If the previous conversation's binding is
   missing, specify the desired continuation rather than guessing it.

No global environment variables, CLI pipes, model calls, or background timers
are required. A stop/exit does not mark tasks complete. Only deliberate task
text and requested reports are recorded; prompts, transcripts and tool output
are not imported. Explicit text is not automatically redacted. Skills rely on
client discovery, permissions and model behavior; they are not enforcement.

## CLI reference for the skill

These commands are usually run by the agent, not by the user. Use the known
registered vault for each call; for an unregistered existing vault replace
`track --vault "$VAULT"` with `TRACK_VAULT="$VAULT_PATH" track` and use an
absolute path. Do not treat a failed read as a missing project.

```sh
# The following variables are placeholders to bind in this shell only.
VAULT=work
PROJECT=demo
LABEL=work-session-unique-label

track vault list
track --vault "$VAULT" timeline-project list
# Only if the user wants a new project:
track --vault "$VAULT" timeline-session init --project "$PROJECT" --title='Demo project'
track --vault "$VAULT" timeline-session show --project "$PROJECT"
track --vault "$VAULT" timeline-session start --project "$PROJECT" --agent codex --session "$LABEL"
# Copy the returned JSON run value into RUN, then:
track --vault "$VAULT" timeline-session task --project "$PROJECT" --run "$RUN" \
  --task implement --title='Implement feature' --status active
track --vault "$VAULT" timeline-session task --project "$PROJECT" --run "$RUN" \
  --task implement --status done
```

Use a new label for an independent run. Keep the same binding, run and local task
IDs when resuming an existing run. Existing task fields are patched, not reset.
The underlying project-list command can open or refresh the normal Track index
cache, but it does not create or modify canonical timeline records. Use
`timeline-session show` to inspect a known project directly.

## Advanced automatic SessionStart

This optional route is unchanged for existing Claude/Codex installations. Use
it only if you want the agent to start tracking substantive work without a fresh
request in each session.

- Confirm the existing vault/project and put `track` on the client PATH. Merge
  the relevant [Codex hook](codex.hooks.json) or [Claude hook](claude.settings.json)
  into the intended client configuration, not over it. In Codex the trusted
  repository location is `.codex/hooks.json`; review it using `/hooks` on CLI
  where required. Register the hook in one layer only.
- If wanted, merge [the standing instructions](WORKSPACE-INSTRUCTIONS.md)
  into `AGENTS.md` (Codex) or `CLAUDE.md` (Claude). The hook already provides
  a self-contained policy; the project instructions reinforce it.
- Launch the client with `TRACK_VAULT` set to the intended vault, the existing
  project in `TRACK_TIMELINE_PROJECT`, and `TRACK_TIMELINE_ENABLED=1`.
  Prefer a workspace-local launch environment rather than global settings.

The hook attaches a run based on a hash of the actual session ID. Resume and
compact with the same ID reuse the attachment. It does not create a project, add
tasks, or mark work done. Without opt-in it is inert; set
`TRACK_TIMELINE_ENABLED=0` to disable automatic mode. Explicit skill requests
and CLI writes remain available. Missing or failed hook context skips automatic
tracking; it must never reuse a prior context or block unrelated work. The hook
ignores prompts and transcript paths, and invokes no extra model calls.

See the [Codex hook contract](https://learn.chatgpt.com/docs/hooks) and
[Claude hook contract](https://code.claude.com/docs/en/hooks). Track CLI and
hook fixtures do not establish live client skill discovery, Desktop environment
inheritance, or model compliance. For live acceptance use a disposable work
repository and vault: ask once to track a small verified task, check its project
and milestones in Komado, continue the same conversation, then stop tracking
and confirm no more records are written.
