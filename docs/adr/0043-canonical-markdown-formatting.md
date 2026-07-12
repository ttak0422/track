# 0043. Canonical Markdown formatting (`track fmt`)

Status: Accepted

## Context

Notes are hand-written Markdown, so their whitespace and list style drift: trailing spaces, stray
runs of blank lines, headings that hug their neighbours, a mix of `*`, `+`, and `-` bullets. None of
this breaks the vault — `track doctor` already covers the breakage axis (missing sidecars, orphan
files, duplicate titles) — but it makes diffs noisy and notes inconsistent.

Prior art: `gofmt`, Prettier, and org-mode's built-in indentation all establish that a *canonical*
formatter earns its keep by being small, opinionated, and idempotent, so it can run unattended and in
CI without a human weighing each change. Notes carry executable fenced code blocks (see
`docs/adr/0018`-era babel work), which a formatter must never disturb.

## Decision

Add `track fmt` as the style counterpart to `track doctor`. The engine logic lives in
`internal/track/mdfmt` (`Format(string) string`, a pure function); the CLI walks files.

**The rule set, exhaustively:**

1. Strip trailing whitespace (spaces, tabs, carriage returns) from every line.
2. Collapse runs of blank lines to a single blank line, except for the two blank lines immediately
   before a heading, and drop blank lines at the start and end of the document.
3. Ensure exactly two blank lines before and one blank line after each ATX heading (`#`…`######`).
4. Normalize unordered-list bullets to `-` (from `*` or `+`), preserving indentation and the text.
5. End the document with exactly one newline.

**What is never touched:** fenced code blocks (` ``` ` or `~~~`) pass through verbatim — content,
blank lines, and any heading- or list-like lines inside them. The rules are line-oriented and only
ever change a line's end (rule 1) or its leading bullet character (rule 4), so inline code-span
content, which lives in a line's interior, is never rewritten. Thematic breaks (`***`, `---`, `* * *`)
are recognized and left alone so rule 4 does not turn them into list items.

**Idempotence is a property, not an aspiration:** `Format(Format(x)) == Format(x)` is asserted by a
test over every golden input and output.

**Interface:** `track fmt [--check] (<path>… | --all)`. It rewrites in place by default; `--all`
covers the vault's note and journal directories; explicit paths may be files or directories (walked
for `.md`/`.markdown`); `--check` writes nothing, lists the files that would change, and exits
non-zero for CI. Output is the usual single-line JSON.

## Consequences

- Formatting is intentionally shallow: indented (4-space) code blocks are not specially protected, and
  the space *inside* a heading or after a bullet is left as written. Fenced blocks are the protected
  construct; notes should fence their code. Deepening to full block parsing is a later step, only if
  real notes get mangled.
- `fmt` does not reindex. It changes only whitespace and bullet characters, which do not affect titles
  or `[[links]]`; the next create/append/reindex picks up any body change.
