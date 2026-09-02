// Stacking for the floating preview layer: one flat order, no parents. A window opened from inside
// another is its sibling here, and the only thing that decides what is in front is what was
// activated last.
import { useSyncExternalStore } from "react";

// Keep the complete preview layer below search (190). The stack is renormalized after each change, so
// repeated opens cannot consume the layers reserved for search, confirmation, and notifications.
export const previewBaseZIndex = 100;
export const previewMaxZIndex = 189;
// Hover intent: only open a preview once the pointer rests on a link, so sweeping the cursor down a
// column of links does not flash a popup under every one it crosses.
export const previewOpenDelay = 260;
// Grace period before a transient window closes, so the pointer can travel from the link to the
// window it opened without the window vanishing on the way.
export const previewCloseDelay = 220;

// A preview needs a cursor, and a touch screen has none. There is no resting on a link there — the
// tap that would open the preview is the tap that follows the link, and a tapped link is focused
// besides, which is the other way in — and what opens is a window to be dragged, resized, and
// dismissed by pointing somewhere else. The published site is the mobile surface, so on a pointer
// that cannot hover no preview opens at all and the link simply navigates. Read at the moment of
// opening rather than at render, so a page rendered on the server takes no position on the pointer
// that will read it.
export function pointerCanHover(): boolean {
  return window.matchMedia?.("(hover: none)").matches !== true;
}

// Map insertion order is the stack order, oldest first; the value is the rank the band was
// renormalized to.
const previewRanks = new Map<string, number>();
const previewListeners = new Set<() => void>();
let previewStackVersion = 0;

function notifyPreviewStackChanged() {
  previewStackVersion += 1;
  for (const listener of previewListeners) listener();
}

function renormalizePreviewStack() {
  const ids = [...previewRanks.keys()];
  const firstRank = Math.max(0, ids.length - (previewStackSize() - 1));
  ids.forEach((id, index) => {
    // The oldest overflow entries share the floor; every newer entry keeps a distinct rank, including
    // the frontmost one. This is a bounded overflow policy, not a counter clamp: raises still reorder
    // the stack and can never cross the search layer.
    previewRanks.set(id, index < firstRank ? 0 : index - firstRank + 1);
  });
}

function subscribePreviewStack(listener: () => void): () => void {
  previewListeners.add(listener);
  return () => previewListeners.delete(listener);
}

// A new window enters at the front: opening one is activating it.
export function registerPreview(id: string) {
  previewRanks.delete(id);
  previewRanks.set(id, 0);
  renormalizePreviewStack();
  notifyPreviewStackChanged();
}

export function releasePreview(id: string) {
  if (!previewRanks.delete(id)) return;
  renormalizePreviewStack();
  notifyPreviewStackChanged();
}

export function bringPreviewToFront(id: string) {
  const rank = previewRanks.get(id);
  if (rank === undefined) return;
  // Reinsert at the end (the front), then compact every rank so the finite preview band remains
  // available after any number of interactions.
  previewRanks.delete(id);
  previewRanks.set(id, rank);
  renormalizePreviewStack();
  notifyPreviewStackChanged();
}

export function getPreviewStackOrder(id: string): number {
  return previewRanks.get(id) ?? 0;
}

export function usePreviewStackVersion(): number {
  return useSyncExternalStore(subscribePreviewStack, () => previewStackVersion, () => 0);
}

export function previewStackSize(): number {
  return previewMaxZIndex - previewBaseZIndex + 1;
}
