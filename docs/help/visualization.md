# Visualization

Visualization is how track turns note content into something you *see* — not just prose and links.
It splits into four families, each with its own page:

- [[Diagrams]] — Mermaid (flowcharts, sequence, class, state, Gantt, and every other type it knows),
  Graphviz DOT graphs, and D2 diagrams, written inline or kept as a `.mmd` / `.dot` / `.d2` attachment.
- [[Mindmaps]] — a note's heading tree, or an indented outline, drawn as a tree by a small built-in
  renderer.
- [[Charts]] — statistical charts (line, bar, scatter, heatmap, timeline, …) rendered by `track render`
  from a declarative **View Spec** over the **Canonical Data Model**. Embed one in a note as a
  `.viewspec.json` asset and it is rendered to a static SVG at build time.
- [[Embeds]] — rich media from a standalone `![alt](src)`: YouTube players, Twitter/X posts, PDF
  decks, image URLs, and Open Graph link cards.

All are driven by ordinary Markdown — a fenced block or an image link on its own line — so a note stays
plain text and portable, while the [[Web workspace]] and the static export ([[CLI]] `export-site`)
render the visuals identically.
