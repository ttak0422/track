# Linking notes

Links in track are explicit: you write `[[Title]]` in a note body to point at another note. There is
no implicit auto-linking of bare words — a link exists only where you wrote one.

icon:: 🔗

Back to [[track]].

## Link syntax

| Form | Meaning |
| --- | --- |
| `[[Title]]` | Link to the note whose title is `Title`. |
| `[[Title\|display]]` | Same target, shown as `display`. |
| `[[Title#heading]]` | Link to a heading within the target note. |

A title is the resolution key, and titles come from each note's metadata — editing a body heading does
not rename a note. Use `track rename` to change a title and rewrite the backlinks that point to it.

## Backlinks and the graph

Because links are explicit and indexed, track can answer the reverse question: which notes point here?
That is what `track backlinks` and the [[Web workspace]] backlinks panel show. `track graph` returns
the local neighbourhood of a note as nodes and edges.

## In the static export

When you publish with `track export-site`, a `[[...]]` link becomes a real anchor **only when its
target is part of the published set**. Links to notes outside the selection are flattened to plain
text, so the exported site never has dangling links. See [[CLI]] for the export commands.
