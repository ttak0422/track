# Documentation

This directory contains durable project knowledge that should be committed and shared across agents and contributors.

The user-facing documentation is not here: it is the track vault under `help/`, published to
<https://ttak0422.github.io/track/> (ADR 0059).

## Specifications

- `roadmap.md`: candidate feature TODOs and discussion state.
- `spec/architecture.md`: implementation architecture and package boundaries.
- `spec/agent-workflows.md`: CLI workflow contract for agents and automation.
- `spec/storage.md`: vault layout, sidecar metadata, and SQLite cache shape.
- `spec/links.md`: `[[...]]` link syntax, keyword resolution, and link graph behavior.
- `spec/templates.md`: template file format, substitutions, and creation flows.
- `spec/babel.md`: Markdown-first Org Babel compatibility and support matrix.
- `spec/export.md`: single-note Markdown export rendering and options.
- `spec/web.md`: local web workspace HTTP API, save conflict detection, graph scopes, and theme/palette config.
- `spec/design.md`: the reference for styling web UI; pick a control variant from here rather than inventing one.
- `spec/visualization.md`: Canonical Data Model, View Spec, and chart rendering.
- `spec/fetch.md`: the contract `track-fetch-*` tools target to bring outside data into the Canonical Data Model.

## Agent guides

- `agents/domain.md`: domain docs layout.
- `agents/issue-tracker.md`: GitHub Issues as the triage surface.
- `agents/triage-labels.md`: the canonical triage labels.

## ADRs

`adr/` holds the numbered architecture decision records, newest last. The filenames state the
decision, so browse the directory rather than a list here — a hand-maintained index goes stale
the moment an ADR lands.

## Not Here

Daily scratch notes, rough ideas, and private agent transcripts should stay in ignored local paths such as `.local/` or `devlog/`.
