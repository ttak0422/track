# ADR 0005: Store Journals Under Date-Named Paths

Status: Superseded. Journal note ids remain `yyyyMMdd`, but current storage is flat (`<vault>/<yyyyMMdd>.md`) so paths can be derived from ids and do not need to be cached.

## Status

Accepted

## Context

Journal notes are navigated by day.
The previous implementation used midnight Unix timestamps as note ids and stored journals alongside ordinary notes.

That made journal files hard to distinguish from regular notes, and timestamp file names do not make adjacent day navigation obvious by inspection.

## Decision

Store journal notes under a dedicated `journal/` directory and name them with `yyyyMMdd`:

```text
<vault>/journal/20260530.md
```

The journal note id is the numeric `yyyyMMdd` value.
Metadata remains in the same sidecar namespace:

```text
<vault>/.track/notes/20260530.yaml
```

## Consequences

Journal files are visually separated from regular notes.

Lexical file order is chronological order, which makes adjacent-day navigation possible by scanning sorted journal file names.

The note id space now contains both Unix timestamp ids for regular notes and `yyyyMMdd` ids for journal notes.
Callers should use paths and tags when they need to distinguish journal notes from regular notes.
