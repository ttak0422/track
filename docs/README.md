# Documentation

This directory contains durable project knowledge that should be committed and shared across agents and contributors.

## Specifications

- `spec/architecture.md`: implementation architecture and package boundaries.
- `spec/storage.md`: vault layout, sidecar metadata, and SQLite index shape.
- `spec/links.md`: `[[...]]` link syntax, keyword resolution, and link graph behavior.

## ADRs

- `adr/0001-go-cli-as-source-of-truth.md`
- `adr/0002-versioned-sidecar-metadata.md`
- `adr/0003-implicit-longest-match-autolinks.md`
- `adr/0004-explicit-vault-configuration.md`
- `adr/0005-journal-date-paths.md`
- `adr/0006-body-title-is-authoritative.md`
- `adr/0007-go-lsp-for-editor-navigation.md`
- `adr/0008-explicit-wiki-links.md`

## Not Here

Daily scratch notes, rough ideas, and private agent transcripts should stay in ignored local paths such as `.local/` or `devlog/`.
