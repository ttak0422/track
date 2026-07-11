# Visualization

Visualization is how track turns note content into something you *see* — not just prose and links.
It splits into three families, each with its own page:

- [[Diagrams]] — full Mermaid support: flowcharts, sequence, class, state, Gantt, and every other
  diagram type, written inline or kept as a `.mmd` attachment.
- [[Charts]] — statistical charts (line, bar, scatter, heatmap, timeline, …) rendered by `track render`
  from a declarative **View Spec** over the **Canonical Data Model**. Embed one in a note as a
  `.viewspec.json` asset and it is rendered to a static SVG at build time.
- [[Embeds]] — rich media from a standalone `![alt](src)`: YouTube players, Twitter/X posts, PDF
  decks, image URLs, and Open Graph link cards.

All are driven by ordinary Markdown — a fenced block or an image link on its own line — so a note stays
plain text and portable, while the [[Web workspace]] and the static export ([[CLI]] `export-site`)
render the visuals identically.

Back to [[track]].

tags:: help/visualization
