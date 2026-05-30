# Agent Guide

This repository is developed with multiple coding agents, including Codex and
Claude. Keep this file focused on durable collaboration rules and pointers to
shared project knowledge.

## Project Shape

- The Go CLI is the source of truth for parsing notes, indexing, search, and
  link resolution.
- The Neovim Lua plugin is a thin frontend that shells out to the CLI.
- Reusable engine code lives under `internal/track/*` so future integrations can
  use it without depending on the CLI command layer.

## Shared Knowledge

- Put durable design decisions in `docs/adr/`.
- Put stable specifications and reference material in `docs/spec/`.
- Do not put daily scratch notes, rough ideas, or private agent transcripts in
  `docs/`; they are not project assets.

## Local Notes

- Use `.local/` or `devlog/` for rough notes and ideas that should stay out of
  commits.
- Treat local notes as personal working memory, not as source material for
  implementation unless the user explicitly asks for them to be used.

## Development

- Prefer existing package boundaries and local helpers over introducing new
  abstractions.
- Keep metadata separate from markdown note bodies. Per-note metadata belongs
  under `.track/notes/` and must include a schema `version`.
- Run `go test ./...` after Go changes.
- When the user asks for implementation work, commit completed changes
  automatically in coherent units unless the user says not to commit.
