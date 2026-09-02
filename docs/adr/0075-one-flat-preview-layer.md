# ADR 0075: One flat preview layer

## Status

Accepted. Supersedes ADR 0018's inline hover preview (the floating layer itself stands).

## Context

ADR 0018 kept hover previews "transient and inline in `WikiLink`", promoting a window into the
app-level floating layer only when the reader pinned it. The cheap path stayed out of global state,
which was the point — but it also put the window inside the DOM of the thing that opened it, and two
behaviours follow from that placement rather than from any decision:

- **Order is inherited, not chosen.** A window rendered inside a link is inside that link's stacking
  context. Hovering a link *inside* a preview opens a window that cannot come in front of a window
  opened earlier from the page, however recently it was touched. The same applies to a graph node's
  preview, which sits under `.graph-full` (`z-index: 90`) and so can never rise above the layer at
  all. The stack in `preview/stack.ts` was already flat; the DOM was not, and the DOM wins.
- **Closing a window closes what it opened.** A preview opened from inside another preview is that
  preview's React child, so closing the outer one unmounts the inner one — even when the reader had
  dragged the inner window aside to keep it.

Neither was intended: both are what the placement forces. Three components had also grown their own
copy of the hover-intent machine (`WikiLink`, `GraphFullView`, `MediaFrame`), each with its own
timers, sticky flag and hand-off-to-the-layer path, differing in the details.

## Decision

Every preview window is opened in the floating layer. There is no inline window and no parent-child
relationship between windows.

- `FloatingProvider` gains a **transient** window: one the pointer opened and the pointer can take
  away. It closes once the pointer has left both its opener and the window itself; dragging,
  resizing, or pinning settles it. `pinned` keeps its old meaning — surviving navigation.
- The hover lifecycle (`hold`, `scheduleClose`, `settle`) lives in the provider, because both the
  opener and the window drive it, and because a window has to outlive an opener that is unmounted
  while the pointer rests on it.
- The openers keep only their intent timer and the id of the window they last opened. A window
  belongs to the layer from the moment it opens.
- Stacking order is the flat one it always was, now unobstructed: the window activated last is in
  front, whoever opened it. `PreviewDepthContext` and `FloatingWindow`'s `depth` prop are deleted —
  `depth` was already read by nothing.

## Consequences

- A preview opened from inside a preview is a sibling: it can be raised over the one it came from,
  and it stays when that one is closed.
- The layer de-duplicates by content key, so one note is one window wherever it was opened from.
  Hovering the same link again raises and re-anchors it; a window the reader has dragged keeps its
  place.
- Transient windows are dropped on navigation with the other unpinned ones, so the hover path still
  leaves nothing behind.
- One hover machine instead of three. `MediaFrame`'s click-opened preview is simply a window that
  opens settled.
