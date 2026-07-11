# Block links

Sometimes the thing worth linking to is not a whole note or a heading section but a single
paragraph or list item. Mark the block with an id, and it becomes a link target — the same idea as
Obsidian's block references.

up:: [[Linking notes]]

## Marking a block

End a paragraph or list item with `^id` — a caret, then letters, digits, `-` or `_`:

```markdown
The paragraph you want to point at. ^my-block

- a list item worth citing ^li-1
```

Ids are **manual only**: track never generates one, matching its explicit-link philosophy — an
anchor exists only where you wrote one. The marker is data, not prose: the rendered note hides it.
Inside one note, the first occurrence of an id wins, so keep ids unique per note.

## Linking to a block

`[[Title#^id]]` resolves to the marked block. In the [[Web workspace]] (and on this published
site), following the link opens the note, scrolls to the block, and highlights it; the static
export gives the block a real anchor, so a deep link into a page lands on it too. In the editor,
go-to-definition on the link jumps to the marker's line.

Try it live — this link targets the marked intro paragraph of [[Linking notes]]:

[[Linking notes#^explicit-links|The "links are explicit" paragraph]]

## Transcluding a block

A block reference also works with transclusion: `![[Title#^id]]` on its own line embeds just that
block, through the same machinery that embeds a whole note (`![[Title]]`) or a heading section
(`![[Title#Heading]]`). The block arrives with its marker stripped.

The embed below is written as `![[Linking notes#^explicit-links]]`:

![[Linking notes#^explicit-links]]

## This page marks its own blocks

This very paragraph carries the id `^self-demo`, so `[[Block links#^self-demo]]` navigates here —
click [[Block links#^self-demo|this self-link]] and watch the highlight. ^self-demo
