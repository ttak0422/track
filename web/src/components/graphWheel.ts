// isZoomWheel: a graph zooms only on an explicit gesture — ctrl+wheel (how every engine reports a
// trackpad pinch, and what a deliberate ctrl+scroll means) or shift+wheel — and the gesture is the
// same on every surface: the note aside, the floating panel, and /graph. A bare wheel is always
// left alone, so over a scrollable page it scrolls the page and never fights the graph. (Guessing
// the device from a single event — integer notches = mouse wheel = zoom — misread smooth-scrolling
// mice, whose fractional deltas made a wheel turn zoom and scroll at once.)
export interface WheelLike {
  ctrlKey: boolean;
  shiftKey: boolean;
  deltaX: number;
  deltaY: number;
}

export function isZoomWheel(e: WheelLike): boolean {
  return e.ctrlKey || e.shiftKey;
}

// Browsers turn shift+wheel into a horizontal delta, so the zoom step is whichever axis moved.
export function zoomDelta(e: WheelLike): number {
  return e.deltaY !== 0 ? e.deltaY : e.deltaX;
}
