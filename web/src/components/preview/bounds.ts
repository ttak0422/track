// Geometry for the draggable/resizable wiki preview window. These helpers are pure (they read only the
// current viewport via window.innerWidth/innerHeight), so they can be unit-tested without rendering.

export const previewMargin = 12;
export const minPreviewWidth = 280;
export const minPreviewHeight = 180;
// A preview's opening height: at least this (the previous fixed size, kept as the floor) and otherwise
// a fraction of the viewport height, so a taller screen opens a taller, more readable window. Width is
// unchanged. The height is still capped by the room below the placement point.
export const defaultPreviewHeight = 280;
export const previewHeightRatio = 0.7;
// Keep at least a short grab/readable strip visible when a preview is dragged mostly off-screen.
export const minPreviewVisible = 32;
// Gap between a link and a preview placed beside it.
export const previewSideGap = 12;

export interface PreviewBounds {
  left: number;
  top: number;
  width: number;
  height: number;
}

// The link's viewport rect, so the preview can be placed beside it (keeping the link column visible)
// rather than directly below it.
export interface PreviewAnchor {
  linkLeft: number;
  linkRight: number;
  linkTop: number;
  linkBottom: number;
}

// The anchor a control hands the window it opens: its own rect. An element that is not laid out
// (jsdom, or one unmounted between the click and the read) anchors at the viewport origin, which
// initialPreviewBounds then places like any other cramped corner.
export function elementAnchor(el: Element | null | undefined): PreviewAnchor {
  const rect = el?.getBoundingClientRect();
  return rect
    ? { linkLeft: rect.left, linkRight: rect.right, linkTop: rect.top, linkBottom: rect.bottom }
    : { linkLeft: 0, linkRight: 0, linkTop: 0, linkBottom: 0 };
}

// How far an explicitly floated window may land from where its anchor alone would put it. Windows
// opened from a column of buttons measure near-identical anchors, so without a scatter every one of
// them lands on the same pixel and a stack of them reads as a single window.
export const previewScatter = 56;

// scatterPreviewBounds nudges a placement by up to previewScatter in each direction. The randomness
// is a parameter so the placement stays testable; the viewport still has the last word, since a
// nudged window is constrained back inside it.
export function scatterPreviewBounds(
  bounds: PreviewBounds,
  random: () => number = Math.random,
): PreviewBounds {
  return constrainPreviewBounds({
    ...bounds,
    left: bounds.left + (random() * 2 - 1) * previewScatter,
    top: bounds.top + (random() * 2 - 1) * previewScatter,
  });
}

// A resize target on the window: one of the four corners or four edges. An edge drag moves only the
// width or the height, while a corner drag moves both dimensions.
export type PreviewResizeHandle = "nw" | "ne" | "sw" | "se" | "w" | "e" | "n" | "s";

export function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

export function initialPreviewBounds(anchor: PreviewAnchor): PreviewBounds {
  const width = clamp(
    Math.min(window.innerWidth * 0.5, 640),
    minPreviewWidth,
    Math.max(minPreviewWidth, window.innerWidth - previewMargin * 2),
  );
  // Prefer placing the preview beside the link (right, then left) so a column of links below the
  // hovered one stays visible. Fall back to just below the link only when neither side has room.
  const roomRight = window.innerWidth - previewMargin - (anchor.linkRight + previewSideGap);
  const roomLeft = anchor.linkLeft - previewSideGap - previewMargin;
  let left: number;
  let top: number;
  if (roomRight >= width) {
    left = anchor.linkRight + previewSideGap;
    top = anchor.linkTop;
  } else if (roomLeft >= width) {
    left = anchor.linkLeft - previewSideGap - width;
    top = anchor.linkTop;
  } else {
    left = anchor.linkLeft;
    top = anchor.linkBottom + 8;
  }
  const height = clamp(
    Math.max(defaultPreviewHeight, window.innerHeight * previewHeightRatio),
    minPreviewHeight,
    Math.max(minPreviewHeight, window.innerHeight - top - previewMargin),
  );
  return constrainPreviewBounds({ left, top, width, height });
}

export function constrainPreviewBounds(bounds: PreviewBounds): PreviewBounds {
  const width = clamp(bounds.width, minPreviewWidth, Math.max(minPreviewWidth, window.innerWidth - previewMargin * 2));
  const height = clamp(
    bounds.height,
    minPreviewHeight,
    Math.max(minPreviewHeight, window.innerHeight - previewMargin * 2),
  );
  const horizontalMin = previewMargin + minPreviewVisible - width;
  const horizontalMax = window.innerWidth - previewMargin - minPreviewVisible;
  // Allow a little upward overflow, but keep enough chrome visible that the window remains draggable.
  const verticalMin = previewMargin - minPreviewVisible;
  const verticalMax = window.innerHeight - previewMargin - minPreviewVisible;
  return {
    width,
    height,
    left: clamp(bounds.left, horizontalMin, Math.max(horizontalMin, horizontalMax)),
    top: clamp(bounds.top, verticalMin, Math.max(verticalMin, verticalMax)),
  };
}

export function resizePreviewBounds(
  handle: PreviewResizeHandle,
  start: PreviewBounds,
  dx: number,
  dy: number,
): PreviewBounds {
  let next = { ...start };
  if (handle.includes("e")) {
    next.width = start.width + dx;
  }
  if (handle.includes("s")) {
    next.height = start.height + dy;
  }
  if (handle.includes("w")) {
    next.left = start.left + dx;
    next.width = start.width - dx;
  }
  if (handle.includes("n")) {
    next.top = start.top + dy;
    next.height = start.height - dy;
  }
  if (next.width < minPreviewWidth && handle.includes("w")) {
    next.left = start.left + start.width - minPreviewWidth;
  }
  if (next.height < minPreviewHeight && handle.includes("n")) {
    next.top = start.top + start.height - minPreviewHeight;
  }
  return constrainPreviewBounds(next);
}
