# Agent Guide

This repository is developed with multiple coding agents, including Codex and Claude.
Keep this file focused on durable collaboration rules and pointers to shared project knowledge.

## Project Shape

- The Go CLI is the source of truth for parsing notes, indexing, search, and link resolution.
- The Neovim Lua plugin is a thin frontend that shells out to the CLI.
- Reusable engine code lives under `internal/track/*` so future integrations can use it without depending on the CLI command layer.

## Shared Knowledge

- Put durable design decisions in `docs/adr/`.
- Put stable specifications and reference material in `docs/spec/`.
- Use `docs/spec/agent-workflows.md` for the stable CLI contract expected by agents and automation.
- Read `docs/spec/design.md` before adding or restyling web UI, and pick exactly one control variant from it rather than inventing a treatment.
- Do not put daily scratch notes, rough ideas, or private agent transcripts in `docs/`; they are not project assets.

## The help vault

`docs/help` is a track vault, not a directory of Markdown (ADR 0059) — it is what `make site`
publishes. Its pages are `docs/help/note/<id>.md`, with the title in `docs/help/.track/notes/<id>.yaml`
beside each one, so a page is found by title or id rather than by file name.

Address it without registering it: pass `--path docs/help/note/<id>.md`, which names the vault by
itself, or prefix anything else with `TRACK_VAULT=docs/help`. Without one of those a command uses the
default vault — a read comes back empty, and a **write lands in another vault silently**.

### Document user-visible changes

- Write help prose, headings, metadata, and example labels in English, matching the surrounding
  help. Keep non-English text only when it is needed to demonstrate language-specific behavior.
- New features and user-visible behavior changes must include corresponding `docs/help` updates in
  the same PR. Explain how to use the behavior, give runnable examples where appropriate, and cover
  important limits and errors. ADRs, technical specifications, and CLI reference text alone do not
  replace user-facing help.
- Update the affected `docs/help/note/<id>.md` pages and keep navigation, heading links, assets, and
  any changed `.track/notes/<id>.yaml` metadata consistent so users can find the guidance. Follow the
  vault-selection instructions above when using the CLI.
- Verify examples and relevant behavior tests, run `nix develop --command make site`, and inspect
  the affected rendered pages and links. The PR must identify the help pages updated and report
  verification results, including anything blocked or not run. Check CI for the final published head.
- Internal-only changes with no user-visible effect may omit help updates; explain that exception
  in the PR rather than silently skipping documentation.

## Pull requests

- Write PR titles and bodies in English. Use a concise conventional title, for example
  `docs: clarify diagram examples`, `feat: add architecture views`, or
  `fix: fit diagrams to the reading width`.
- Follow `.github/pull_request_template.md`: lead with the concrete change in Summary, identify
  user-facing help updates (or explain the internal-only exception), and report actual verification
  results. Include relevant limits, blocked or unrun checks, and CI links for the exact published
  head. Remove placeholder text and omit optional sections that add no useful information.
- Leave created PRs open for review. Do not merge them or enable auto-merge unless the user gives
  a separate instruction for that specific PR.

## Development

- This project is under active development: prioritize the best design over backward compatibility, and do not hesitate to make breaking changes when they lead to a better result.
- Prefer existing package boundaries and local helpers over introducing new abstractions.
- When the user asks for implementation work, commit completed changes automatically in coherent units unless the user says not to commit.

## Agent skills

### Issue tracker

Issues are tracked in this repo's GitHub Issues via the `gh` CLI; external PRs are not a triage surface. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles use their default label strings (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
