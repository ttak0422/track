# Syntax

A note is plain **Markdown** — CommonMark plus [GitHub-Flavored Markdown](https://github.github.com/gfm/)
(tables, task lists, strikethrough). track is **strongly influenced by [Obsidian](https://obsidian.md/)**
and borrows its conventions where they fit — `[[wiki links]]`, standalone-image embeds, and `$…$` / `$$…$$`
math — so a note written for Obsidian mostly reads the same here.

up:: [[track]]

## Text formatting

Wrap text in `**` for **bold**, a single `*` or `_` for *italic*, `~~` for ~~strikethrough~~, and
backticks for `inline code`:

```markdown
**bold**, *italic* (or _italic_), ~~strikethrough~~, `inline code`
```

## Structure

Standard Markdown blocks work as expected:

```markdown
# Heading 1
## Heading 2

- a bullet list
  - nested item
1. an ordered list

- [ ] an open task
- [x] a done task

> a blockquote

---
```

Task lists (`- [ ]` / `- [x]`) render as real checkboxes.

## Alerts

A blockquote whose first line is a `[!TYPE]` marker becomes a colored callout, matching GitHub's alert
syntax. Five types are recognized — `NOTE`, `TIP`, `IMPORTANT`, `WARNING`, `CAUTION`:

```markdown
> [!NOTE]
> Useful context the reader should notice.

> [!WARNING]
> Something that needs care.
```

They render as:

> [!NOTE]
> Useful context the reader should notice.

> [!TIP]
> A helpful suggestion.

> [!IMPORTANT]
> Key information the reader must not miss.

> [!WARNING]
> Something that needs care.

> [!CAUTION]
> A risky action to think twice about.

A blockquote without a `[!TYPE]` marker stays an ordinary quote.

## Code blocks

A fenced block is syntax-highlighted when you name the language after the opening fence, and the web
reader adds a copy button:

````markdown
```go
func Title(text string) string {
    return strings.TrimSpace(text)
}
```
````

It renders as:

```go
func Title(text string) string {
    return strings.TrimSpace(text)
}
```

A ` ```mermaid ` fence renders a diagram instead — see [[Diagrams]]. Fenced blocks in a real note can
also be *run* as code; see [[Babel]].

## Tables

Pipe tables from GitHub-Flavored Markdown, with `:` in the divider row to set column alignment:

```markdown
| Feature | Supported |
| --- | :---: |
| Tables | yes |
| Math | yes |
```

| Feature | Supported |
| --- | :---: |
| Tables | yes |
| Math | yes |

## Math

Math is written in LaTeX, the same as in Obsidian: single dollars for inline, double dollars for a
centered block. It is rendered by [KaTeX](https://katex.org/):

```markdown
Euler's identity is $e^{i\pi} + 1 = 0$.

$$\int_0^1 x^2 \,dx = \tfrac{1}{3}$$
```

Euler's identity is $e^{i\pi} + 1 = 0$.

$$\int_0^1 x^2 \,dx = \tfrac{1}{3}$$

## Links, embeds, and visuals

These are the track-specific constructs, each with its own page:

- **`[[Title]]`** — a wiki link to another note. See [[Linking notes]].
- **`![alt](src)`** on its own line — a rich embed (image, YouTube, PDF, tweet, Open Graph card, or a
  text/Mermaid attachment). See [[Embeds]].
- **`.viewspec.json`** embeds — declarative charts. See [[Charts]].

Everything renders the same in the [[Web workspace]] and in the published static export.
