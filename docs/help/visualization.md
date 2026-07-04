# Visualization

Visualization is how track turns note content into something you *see* — not just prose and links.
It splits into two families, each with its own page:

- [[Charts]] — statistical charts (line, bar, scatter, heatmap, timeline, …) rendered by `track render`
  from a declarative **View Spec** over the **Canonical Data Model**. Embed one in a note as a
  `.viewspec.json` asset and it is rendered to a static SVG at build time.
- [[Embeds]] — rich media from a standalone `![alt](src)`: YouTube players, Twitter/X posts, PDF
  decks, image URLs, Open Graph link cards, and text-file attachments (including Mermaid diagrams).

Both are driven by ordinary Markdown — an image link on its own line — so a note stays plain text and
portable, while the [[Web workspace]] and the static export ([[CLI]] `export-site`) render the visuals
identically.

Back to [[track]].
