import type { PointerEvent } from "react";

// The rail's flyouts open on hover and toggle on click. A tap is not a hover, but it fires
// pointerenter on the way down and pointerleave on the way up all the same — so on a touch screen
// the tap opened the panel, the button's own click found it already open and closed it again, and
// the panel could not be opened at all. The close was scheduled behind the same tap besides.
//
// Every rail flyout takes its hover handlers from here: a touch pointer is ignored and the click is
// left to do the whole job, while a mouse or a pen behaves exactly as it did. The event says which
// kind of pointer it came from, so a laptop with a touchscreen gets both idioms rather than one.
export function hoverOpen(enter: () => void, leave: () => void) {
  return {
    onPointerEnter: (event: PointerEvent) => {
      if (event.pointerType !== "touch") enter();
    },
    onPointerLeave: (event: PointerEvent) => {
      if (event.pointerType !== "touch") leave();
    },
  };
}
