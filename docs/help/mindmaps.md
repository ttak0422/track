# Mindmaps

A `mindmap` fence draws a left-to-right tree of rounded nodes — a quick way to *see* the structure of
an idea, or of the note itself. The tree is laid out by a small built-in SVG renderer, so mindmaps need
no external engine, follow the light/dark theme, and are already drawn in the published static export
before any script runs.

icon:: 🧠

Part of [[Visualization]] (see also [[Diagrams]] and [[Charts]]). Back to [[track]].

## A note's own structure

Leave the fence **empty** and it maps the surrounding note's heading tree — `#` at the root, `##`
branches, `###` leaves. This block is empty in the source of this page, so what you see is this page's
actual outline:

```mindmap
```

Drop one at the top of a long note and it doubles as a visual table of contents that can never go
stale.

## Writing a Markdown mindmap

A non-empty fence can use Markdown headings for the hierarchy and list items for leaves. Links stay
interactive: ordinary Markdown links open their URL, and `[[wiki links]]` resolve to track notes:

````markdown
```mindmap
# Vault

## Notes
- [[Syntax]]
- [[Diagrams]]

## Resources
- [Graphviz](https://graphviz.org/)
```
````

It renders as:

```mindmap
# Vault

## Notes
- [[Syntax]]
- [[Diagrams]]

## Resources
- [Graphviz](https://graphviz.org/)
```

## When to use which

- **Mindmap** — hierarchy and links expressed with ordinary Markdown headings and lists.
- **[[Diagrams]]** (Mermaid, Graphviz) — anything with cross-links, cycles, or labeled edges.
- **[[Charts]]** — numbers.
