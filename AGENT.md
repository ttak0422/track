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
- Do not put daily scratch notes, rough ideas, or private agent transcripts in `docs/`; they are not project assets.

## Development

- This project is under active development: prioritize the best design over backward compatibility, and do not hesitate to make breaking changes when they lead to a better result.
- Prefer existing package boundaries and local helpers over introducing new abstractions.
- When the user asks for implementation work, commit completed changes automatically in coherent units unless the user says not to commit.
