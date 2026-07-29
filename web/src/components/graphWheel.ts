// isZoomWheel decides whether a wheel event should zoom the graph or be left to scroll the page.
//
// Two explicit signals always zoom: ctrl+wheel (every engine reports a trackpad pinch that way, and
// a deliberate ctrl+scroll means the same thing) and shift+wheel. When the graph sits inside a
// scrollable page (needsModifier — the note aside), those are the only zoom gestures: a plain wheel
// always scrolls, so zooming never fights the page.
//
// On the dedicated surfaces (floating panel, /graph) a bare mouse wheel should still zoom, so
// without needsModifier we fall back to telling devices apart:
//   - Physical mouse wheel → zoom  (line/page deltaMode, or big quantized integer vertical steps)
//   - Trackpad 2-finger scroll → page scroll  (small/fractional pixel deltas, often with deltaX)
//
// ponytail: single-event device heuristic — a mouse wheel delivers large, integer, purely-vertical
// steps while a trackpad delivers small or fractional deltas (frequently with a horizontal component).
// A fast trackpad flick can occasionally look like a mouse notch; upgrade to tracking event cadence if
// that misfire ever matters in practice.
export interface WheelLike {
  ctrlKey: boolean;
  shiftKey: boolean;
  deltaMode: number;
  deltaX: number;
  deltaY: number;
}

export function isZoomWheel(e: WheelLike, needsModifier = false): boolean {
  if (e.ctrlKey || e.shiftKey) return true; // pinch-zoom, or an explicit modifier
  if (needsModifier) return false;
  if (e.deltaMode !== 0) return true; // line/page deltas only come from a mouse wheel (e.g. Firefox)
  return e.deltaX === 0 && Number.isInteger(e.deltaY) && Math.abs(e.deltaY) >= 50;
}

// Browsers turn shift+wheel into a horizontal delta, so the zoom step is whichever axis moved.
export function zoomDelta(e: WheelLike): number {
  return e.deltaY !== 0 ? e.deltaY : e.deltaX;
}
