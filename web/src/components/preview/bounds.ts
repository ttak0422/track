// Geometry for the draggable/resizable wiki preview window. These helpers are pure (they read only the
// current viewport via window.innerWidth/innerHeight), so they can be unit-tested without rendering.

export const previewMargin = 12;
export const minPreviewWidth = 280;
export const minPreviewHeight = 180;
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

export type PreviewResizeCorner = "nw" | "ne" | "sw" | "se";

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
    280,
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
  return {
    width,
    height,
    left: clamp(bounds.left, previewMargin, Math.max(previewMargin, window.innerWidth - width - previewMargin)),
    top: clamp(bounds.top, previewMargin, Math.max(previewMargin, window.innerHeight - height - previewMargin)),
  };
}

export function resizePreviewBounds(
  corner: PreviewResizeCorner,
  start: PreviewBounds,
  dx: number,
  dy: number,
): PreviewBounds {
  let next = { ...start };
  if (corner.includes("e")) {
    next.width = start.width + dx;
  }
  if (corner.includes("s")) {
    next.height = start.height + dy;
  }
  if (corner.includes("w")) {
    next.left = start.left + dx;
    next.width = start.width - dx;
  }
  if (corner.includes("n")) {
    next.top = start.top + dy;
    next.height = start.height - dy;
  }
  if (next.width < minPreviewWidth && corner.includes("w")) {
    next.left = start.left + start.width - minPreviewWidth;
  }
  if (next.height < minPreviewHeight && corner.includes("n")) {
    next.top = start.top + start.height - minPreviewHeight;
  }
  return constrainPreviewBounds(next);
}
