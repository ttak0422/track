# ADR 0002: Store Note Metadata in Versioned Sidecar Files

## Status

Accepted

## Context

The initial metadata design stored YAML at the end of markdown notes inside an
HTML comment. That kept note and metadata together, but it also made markdown
files noisier and exposed implementation metadata in the user's writing surface.

The metadata shape is expected to change. Future migrations need an explicit
schema version.

## Decision

Store per-note metadata separately from markdown note bodies.

For note:

```text
<vault>/<id>.md
```

the metadata file is:

```text
<vault>/.track/notes/<id>.yaml
```

New metadata writes include:

```yaml
version: 1
```

The parser rejects unsupported metadata versions. Legacy trailing footmatter can
be read only as a compatibility fallback when no sidecar metadata exists.

## Consequences

Markdown notes stay clean and focused on user-authored content.

The `.track` directory becomes the home for track-owned data, similar to the way
Git keeps repository metadata in `.git`.

Metadata can evolve by adding new versions and explicit migrations. The tradeoff
is that moving or copying a note outside the vault no longer carries metadata
unless the corresponding sidecar file is moved too.
