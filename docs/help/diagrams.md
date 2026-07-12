# Diagrams

track renders diagrams as a first-class part of [[Visualization]] — not only statistical [[Charts]].
Two engines are built in, each triggered by its fence language:

- **[Mermaid](https://mermaid.js.org/)** (` ```mermaid `) — **every diagram type Mermaid supports
  works**, because track hands the block straight to the Mermaid library: flowcharts, sequence, class,
  state, entity-relationship, Gantt, pie, user-journey, gitgraph, and the rest.
- **[Graphviz](https://graphviz.org/)** (` ```dot `) — the DOT language with Graphviz's own layout
  engine, for arbitrary graphs where you say *what connects to what* and let the layout be computed.

Part of [[Visualization]] (see also [[Charts]], [[Mindmaps]], and [[Embeds]]). Back to [[track]].

## Writing a diagram

Fence a block with `mermaid` and write Mermaid syntax. It renders inline; if the syntax is wrong, the
original source is shown instead of a broken image, so a typo never hides your text.

````markdown
```mermaid
flowchart TD
  idea([New idea or source]) --> capture["Capture a note"]
  capture --> link["Link related notes with wiki links"]
  link --> explore{"Explore the local graph"}
  explore -->|gap found| capture
  explore -->|ready| write["Synthesize a write-up"]
  write --> embed["Embed charts, media, and diagrams"]
  embed --> publish["Publish with track export-site"]
  publish --> review([Review and revisit])
  review --> idea
```
````

It renders as:

```mermaid
flowchart TD
  idea([New idea or source]) --> capture["Capture a note"]
  capture --> link["Link related notes with wiki links"]
  link --> explore{"Explore the local graph"}
  explore -->|gap found| capture
  explore -->|ready| write["Synthesize a write-up"]
  write --> embed["Embed charts, media, and diagrams"]
  embed --> publish["Publish with track export-site"]
  publish --> review([Review and revisit])
  review --> idea
```

## Graphviz

Fence a block with `dot` (or `graphviz`) and write plain [DOT](https://graphviz.org/doc/info/lang.html).
Graphviz runs compiled to WebAssembly, right in the browser — nothing to install — and a syntax error
falls back to the message plus your source, the same as Mermaid:

````markdown
```dot
digraph publish_workflow {
  rankdir=TB;
  node [shape=box, style=rounded];
  capture [label="Capture a note"];
  clarify [label="Clarify the idea"];
  link [label="Link related notes"];
  draft [label="Draft a write-up"];
  review [label="Review and revise"];
  publish [label="Publish"];
  archive [label="Archive source material"];

  capture -> clarify -> link -> draft -> review -> publish;
  review -> draft [label="revise"];
  publish -> archive;
}
```
````

It renders as:

```dot
digraph publish_workflow {
  rankdir=TB;
  node [shape=box, style=rounded];
  capture [label="Capture a note"];
  clarify [label="Clarify the idea"];
  link [label="Link related notes"];
  draft [label="Draft a write-up"];
  review [label="Review and revise"];
  publish [label="Publish"];
  archive [label="Archive source material"];

  capture -> clarify -> link -> draft -> review -> publish;
  review -> draft [label="revise"];
  publish -> archive;
}
```

The graph background defaults to transparent so it sits on the page in both themes; set `bgcolor`
yourself to override it.

## Diagrams as attachments

Prefer to keep a diagram in its own file? A `.mmd` / `.mermaid` attachment renders with the Mermaid
engine and a `.dot` / `.gv` attachment with Graphviz — see [[Embeds]] for the standalone
`![](assets/diagram.mmd)` syntax. A diagram kept as a separate file looks identical to one written
inline.

## Viewing

In the [[Web workspace]] a rendered diagram is interactive: drag to pan, the wheel or the +/- buttons to
zoom toward the cursor, and ↺ to reset. A large diagram opens fitted to a readable size, so you can take
it in at a glance and explore the detail without scrolling the page. The published static export
([[CLI]] `export-site`) renders the same diagrams with the same engine.

tags:: help/visualization/diagrams
