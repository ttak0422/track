# ADR 0003: Use Implicit Longest-Match Auto-Links

## Status

Accepted

## Context

track is intended to support Japanese notes well.
Explicit markdown links are too noisy for every reference, and word-boundary matching does not work reliably for CJK text.

The desired behavior is close to Hatena keyword auto-linking: registered terms become followable wherever they appear.

## Decision

Use implicit auto-links derived from note titles and aliases.

Matching is pure substring matching with longest-match precedence and non-overlapping output.
Fenced code blocks are excluded.

The Go engine uses this rule to build the link graph.
The Lua frontend mirrors the same rule for visible-range highlighting.

## Consequences

Japanese text can link without special markup or spaces.

Users must manage titles and aliases carefully because any registered term can link wherever it appears.
Longest-match behavior reduces ambiguity when one keyword contains another.
