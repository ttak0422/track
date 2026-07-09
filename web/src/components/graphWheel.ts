// isZoomWheel decides whether a wheel event should zoom the graph or be left to scroll the page.
//
// Three sources produce `wheel` events and we want different behaviour for each:
//   - Trackpad pinch      → zoom  (every engine reports it as ctrl+wheel)
//   - Physical mouse wheel → zoom  (line/page deltaMode, or big quantized integer vertical steps)
//   - Trackpad 2-finger scroll → page scroll  (small/fractional pixel deltas, often with deltaX)
//
// ponytail: single-event device heuristic — a mouse wheel delivers large, integer, purely-vertical
// steps while a trackpad delivers small or fractional deltas (frequently with a horizontal component).
// A fast trackpad flick can occasionally look like a mouse notch; upgrade to tracking event cadence if
// that misfire ever matters in practice.
export interface WheelLike {
  ctrlKey: boolean;
  deltaMode: number;
  deltaX: number;
  deltaY: number;
}

export function isZoomWheel(e: WheelLike): boolean {
  if (e.ctrlKey) return true; // pinch-zoom
  if (e.deltaMode !== 0) return true; // line/page deltas only come from a mouse wheel (e.g. Firefox)
  return e.deltaX === 0 && Number.isInteger(e.deltaY) && Math.abs(e.deltaY) >= 50;
}
