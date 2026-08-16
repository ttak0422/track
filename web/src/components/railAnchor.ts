import type { CSSProperties } from "react";

// Where a rail flyout is drawn. The rail clips its overflow and owns a stacking context below floating
// previews, so every flyout is positioned fixed from its trigger's own rect rather than laid out under
// it — and beside the button that summoned it, never over the rail.
//
// A flyout hangs from its button's top edge. A button in the lower half of the viewport (Settings is
// pinned to the foot of the dock, and the note group sits just above it) would send the panel off the
// bottom, so those rise from the button's foot instead. Half the viewport is the dividing line rather
// than the panel's own height: one rule, and no measuring pass after the panel mounts.
export function railAnchor(trigger: HTMLElement | null | undefined): CSSProperties | undefined {
  const rect = trigger?.getBoundingClientRect();
  if (!rect) return undefined;
  // A screen with no cursor, or a window with no room for a lane, has no rail to open beside (the
  // same query in styles.css — keep the two in step): the trigger is the floating mark, which the
  // reader drags wherever they like. A panel placed from that rect opened somewhere new every time,
  // so there it is not a flyout at all — it takes the window, the way the full-page graph takes the
  // reader, and opens in the same place whatever corner the mark is resting in. A list too short to
  // fill it simply leaves the rest empty; where the panel is matters more than how much of it is
  // used. The mark itself floats above it (z-index 100 over the panel's 95) and closes it.
  if (window.matchMedia?.("(hover: none), (max-width: 540px)").matches) {
    return { inset: 8 };
  }
  const left = rect.right + 12;
  return rect.top > window.innerHeight / 2
    ? { left, bottom: window.innerHeight - rect.bottom }
    : { left, top: rect.top };
}
