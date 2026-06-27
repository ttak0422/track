# ADR 0018: Persistent Floating Windows

## Status

Accepted

## Context

The web reader shows a note in a floating, draggable/resizable preview window
when you hover a `[[wiki link]]`. The window lives inside the `WikiLink`
component, which is rendered deep inside the routed note view. Two desired
behaviors do not fit that placement:

- A pinned preview should **survive navigation**: pin a referenced note, click
  through to another note, and keep reading the pinned one. Today the window
  unmounts the moment the route's note view re-renders.
- The same floating window should host **media** (an image or PDF embed), opened
  from a pin affordance on the embed, not just note bodies.

Anything owned by the note view cannot outlive it, and duplicating the
drag/resize/collapse window for media would fork the logic.

## Decision

Introduce one app-level **floating window layer** that both wiki previews and
media embeds share.

- A `FloatingProvider` (context + state) and a `FloatingLayer` render component
  live in `Shell`, which is the router's root component and stays mounted across
  child-route navigation. Windows promoted into this layer therefore persist
  until explicitly closed.
- A generic `FloatingWindow` owns the chrome and interaction — drag, four-corner
  resize, the collapse toggle, and the pin/close buttons — using the pure
  geometry in `preview/bounds.ts`. Content is passed as children, so the same
  window frames a note body or a media embed.
- Hover previews stay **transient and inline** in `WikiLink`: hovering opens an
  unpinned `FloatingWindow` with the hover-intent delay, and it auto-closes on
  mouse-out. Pinning **promotes** the window — its current bounds and collapsed
  state are handed to the provider, the inline copy closes, and the layer renders
  the persistent one. This keeps the cheap, ephemeral hover path out of global
  state while only persisting what the user explicitly pinned.
- Media embeds show a pin affordance on hover; pinning opens a media
  `FloatingWindow` directly in the layer.
- The provider de-duplicates by content key (`note:<id>` / `media:<src>`):
  pinning something already open just raises it. Stacking order is shared with
  the inline previews so the most recently touched window is frontmost.

## Consequences

- Pinned windows are independent of the view that spawned them; the layer
  re-fetches a note by id, so a pinned note window renders correctly even after
  its originating link is gone.
- `WikiPreview` is generalized into `FloatingWindow` plus a note-body content
  component; the media embed gains a hover pin control.
- Unpinned hover previews are deliberately not persisted, so they still close on
  navigation; only pinned windows live in the layer.
- The layer is a fixed overlay above the workspace; its windows manage their own
  z-order via the existing preview stacking counter.
