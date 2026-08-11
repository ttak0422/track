// Stacking helpers shared by WikiLink and the floating windows.
import { useSyncExternalStore } from "react";

// Keep the complete preview layer below search (190). The stack is renormalized after each change, so
// repeated opens cannot consume the layers reserved for search, confirmation, and notifications.
export const previewBaseZIndex = 100;
export const previewMaxZIndex = 189;
// Hover intent: only open a preview once the pointer rests on a link, so sweeping the cursor down a
// column of links does not flash a popup under every one it crosses.
export const previewOpenDelay = 260;

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

type PreviewEntry = { active: boolean; order: number };

const previewEntries = new Map<string, PreviewEntry>();
const previewListeners = new Set<() => void>();
let previewStackVersion = 0;
let previewID = 0;

function notifyPreviewStackChanged() {
  previewStackVersion += 1;
  for (const listener of previewListeners) listener();
}

function renormalizePreviewStack() {
  const active = [...previewEntries.values()].filter((entry) => entry.active);
  const firstRank = Math.max(0, active.length - (previewStackSize() - 1));
  active.forEach((entry, index) => {
    // The oldest overflow entries share the floor; every newer entry keeps a distinct rank, including
    // the frontmost one. This is a bounded overflow policy, not a counter clamp: raises still reorder
    // the active stack and can never cross the search layer.
    entry.order = index < firstRank ? 0 : index - firstRank + 1;
  });
}

function subscribePreviewStack(listener: () => void): () => void {
  previewListeners.add(listener);
  return () => previewListeners.delete(listener);
}

export function createPreviewID(): string {
  const id = `preview-${previewID++}`;
  previewEntries.set(id, { active: false, order: 0 });
  return id;
}

export function registerPreview(id: string) {
  previewEntries.set(id, { active: true, order: 0 });
  renormalizePreviewStack();
  notifyPreviewStackChanged();
}

export function activatePreview(id: string) {
  const entry = previewEntries.get(id);
  if (!entry) return;
  entry.active = true;
  bringPreviewToFront(id);
}

export function deactivatePreview(id: string) {
  const entry = previewEntries.get(id);
  if (!entry?.active) return;
  entry.active = false;
  renormalizePreviewStack();
  notifyPreviewStackChanged();
}

export function releasePreview(id: string) {
  if (!previewEntries.delete(id)) return;
  renormalizePreviewStack();
  notifyPreviewStackChanged();
}

export function bringPreviewToFront(id: string) {
  const entry = previewEntries.get(id);
  if (!entry?.active) return;
  // Map insertion order is the stack order. Reinsert the entry at the end, then compact all active
  // ranks so the finite preview band remains available after any number of interactions.
  previewEntries.delete(id);
  previewEntries.set(id, entry);
  renormalizePreviewStack();
  notifyPreviewStackChanged();
}

export function getPreviewStackOrder(id: string): number {
  return previewEntries.get(id)?.order ?? 0;
}

export function usePreviewStackOrder(id: string): number {
  return useSyncExternalStore(subscribePreviewStack, () => getPreviewStackOrder(id), () => 0);
}

export function usePreviewStackVersion(): number {
  return useSyncExternalStore(subscribePreviewStack, () => previewStackVersion, () => 0);
}

export function previewStackSize(): number {
  return previewMaxZIndex - previewBaseZIndex + 1;
}
