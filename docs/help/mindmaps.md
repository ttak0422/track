# Mindmaps

A `mindmap` fence draws a left-to-right tree of rounded nodes — a quick way to *see* the structure of
an idea, or of the note itself. The tree is laid out by a small built-in SVG renderer, so mindmaps need
no external engine, follow the light/dark theme, and are already drawn in the published static export
before any script runs.

Part of [[Visualization]] (see also [[Diagrams]] and [[Charts]]). Back to [[track]].

## A note's own structure

Leave the fence **empty** and it maps the surrounding note's heading tree — `#` at the root, `##`
branches, `###` leaves. This block is empty in the source of this page, so what you see is this page's
actual outline:

```mindmap
```

Drop one at the top of a long note and it doubles as a visual table of contents that can never go
stale.

## Writing an outline

A non-empty fence is an indented outline: one node per line, two more spaces (or a deeper bullet) per
level. Leading `-`/`*` bullets are optional and stripped:

````markdown
```mindmap
Vault
  Notes
    Atomic ideas
    Literature notes
  Journals
  Assets
    Images
    Diagrams
```
````

It renders as:

```mindmap
Vault
  Notes
    Atomic ideas
    Literature notes
  Journals
  Assets
    Images
    Diagrams
```

If several lines share the top level, they hang off an implicit unlabeled hub node.

## When to use which

- **Mindmap** — hierarchy only, zero syntax beyond indentation; the fastest way to sketch a breakdown.
- **[[Diagrams]]** (Mermaid, Graphviz) — anything with cross-links, cycles, or labeled edges.
- **[[Charts]]** — numbers.
