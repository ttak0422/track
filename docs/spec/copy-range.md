# Copy Range source positions

Copy Range in the live Markdown reader copies a plain-text file reference for a person or agent
that can open the original note body. It is not a Track wiki link, URL, shell command, pinned citation,
or a new resolver input. Repository searches found no parser consuming the existing output; the
only producer is the web selection popover. Existing wiki links, CLI ranges, LSP positions, and the
inclusive whole-line source used by Copy markdown / Copy for Confluence retain their own contracts.

## Format

- Verified text endpoints: `notes/example.md:12:4-14:9`.
- Fallback: existing `notes/example.md:12-14`, or `notes/example.md:12` for a single line.
- Precise line and column numbers are **1-based**. Columns count **UTF-16 code units**, including
  tabs as one unit; they are neither UTF-8 bytes, code points, nor display cells. `日😀本` occupies
  four units. Selecting just the emoji on line 1 produces `:1:2-1:4`.
- The precise start is inclusive and end is **exclusive**. `:1:3-2:1` ends before the first character
  of line 2 and includes the preceding newline. Both endpoints are always written, even on one line.
  Fallback line numbers remain inclusive at both ends.
- The path is preserved verbatim, including spaces, colons, Unicode, `#`, and `%`. No Markdown
  escaping or URL percent encoding is applied: the clipboard flavor is plain text. Consumers must
  not treat the reference as an encoded URL or execute it as a shell command.

## Prior art and choice

There is no single universal range-reference standard. The
[VS Code CLI](https://code.visualstudio.com/docs/configure/command-line#_core-cli-options) accepts
`file:line:character` for a position; its URL scheme also supports line/column positions. Extending
Track's existing colon-delimited file reference with two explicit positions keeps it readable.
This does not claim VS Code's `--goto` accepts Track's full two-endpoint range.

[GitHub snippet links](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/creating-a-permanent-link-to-a-code-snippet)
identify lines using `#L…`; that is useful for whole-line web navigation, but does not specify the
partial-line selection this action needs. Track is copying a local file reference, not a GitHub URL.

LSP 3.17 uses zero-based positions with negotiated encodings and UTF-16 as the mandatory default;
its [position definition](https://raw.githubusercontent.com/microsoft/language-server-protocol/gh-pages/_specifications/lsp/3.17/types/position.md)
and [range definition](https://raw.githubusercontent.com/microsoft/language-server-protocol/gh-pages/_specifications/lsp/3.17/types/range.md)
make the exclusive-end convention explicit. Track adopts UTF-16 and an exclusive end for reliable
DOM selection mapping, with 1-based numbers for a human-readable file reference. This is not an LSP
wire object or a claim that every editor uses UTF-16 columns.

## Rendered Markdown mapping and limits

The old DOM only carried inclusive source spans on top-level blocks and list items. A partial
paragraph therefore copied its whole block. The new mapping retains those spans and adds text
wrappers only when the rendered text value exactly equals its original Markdown source slice.
Positions are derived from parser line/column coordinates against the original body, rather than
parsed absolute offsets: transclusion splicing preserves lines but changes earlier character counts.

The live reader and floating window supply the adopted file body separately from the server's
rendered Markdown. Columns are enabled only when those bodies match, allowing trailing-newline
normalization. A server-expanded query/dashboard or flattened action link disables columns for the
whole note. Unverified, stale, or unsaved previews also retain line-only behavior. No columns are
inferred from sanitized/generated source, even when a coincidental substring would match.

At selection time, both endpoint wrappers must still match the source. DOM Range normalizes reverse
selections. Counting wrapper text up to each boundary survives BudouX word-break nodes and nested
inline formatting without counting those presentation elements as characters. Surrogate-interior
boundaries are refused. Multiline selections may span Markdown syntax between their endpoints;
columns refer to the source, not the displayed layout.

Decoded entities, backslash escapes, inline/fenced code, rewritten wiki labels, math, embeds and
other generated content are not guaranteed exact mappings. If either endpoint cannot be verified,
Copy Range keeps the existing whole-block/list-item line reference. Title chrome, diagram SVG,
controls, endpoints outside the note body, collapsed selections and multiple DOM ranges offer no
source-copy action. Unsupported content is never assigned guessed columns. These are body source
positions, not metadata-sidecar positions, screen columns, or offsets into an embedded note.

Copy markdown and Copy for Confluence continue copying whole selected blocks/items. There is no
change to their source slicing, portable Markdown conversion, or clipboard formats. Published pages
and the external Markdown viewer still omit source-copy actions.
