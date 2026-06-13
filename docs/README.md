# Documentation

This directory contains durable project knowledge that should be committed and shared across agents and contributors.

## Specifications

- `roadmap.md`: candidate feature TODOs and discussion state.
- `spec/architecture.md`: implementation architecture and package boundaries.
- `spec/babel.md`: Markdown-first Org Babel compatibility and support matrix.
- `spec/storage.md`: vault layout, sidecar metadata, and SQLite cache shape.
- `spec/templates.md`: template file format, substitutions, and creation flows.
- `spec/links.md`: `[[...]]` link syntax, keyword resolution, and link graph behavior.
- `spec/export.md`: single-note Markdown export rendering and options.
- `spec/web.md`: local web workspace HTTP API, save conflict detection, graph scopes, and theme/palette config.
- `spec/agent-workflows.md`: CLI workflow contract for agents and automation.

## ADRs

- `adr/0001-go-cli-as-source-of-truth.md`
- `adr/0002-versioned-sidecar-metadata.md`
- `adr/0003-implicit-longest-match-autolinks.md`
- `adr/0004-explicit-vault-configuration.md`
- `adr/0005-journal-date-paths.md`
- `adr/0006-body-title-is-authoritative.md`
- `adr/0007-go-lsp-for-editor-navigation.md`
- `adr/0008-explicit-wiki-links.md`
- `adr/0009-heading-anchor-links.md`
- `adr/0010-unique-titles-open-command.md`
- `adr/0011-markdown-export-pipeline.md`
- `adr/0012-drop-alias-keywords.md`
- `adr/0013-metadata-title-is-authoritative.md`
- `adr/0014-vault-health-checks-and-repair.md`

## Not Here

Daily scratch notes, rough ideas, and private agent transcripts should stay in ignored local paths such as `.local/` or `devlog/`.
