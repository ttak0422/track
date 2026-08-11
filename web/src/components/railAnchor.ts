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
  // A screen with no cursor, or a window with no room for a lane, lays the dock along the foot
  // instead (the same query in styles.css — keep the two in step), and there is nothing beside a
  // button there but the next button. So the flyout rises from the whole dock: from the button's top
  // edge, and from the window's left margin rather than the button's own column, which for a button
  // near the right edge would put the panel off screen.
  if (window.matchMedia?.("(hover: none), (max-width: 540px)").matches) {
    return { left: 8, bottom: window.innerHeight - rect.top + 8 };
  }
  const left = rect.right + 12;
  return rect.top > window.innerHeight / 2
    ? { left, bottom: window.innerHeight - rect.bottom }
    : { left, top: rect.top };
}
