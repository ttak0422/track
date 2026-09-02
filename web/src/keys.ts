import type { KeyboardEvent as ReactKeyboardEvent } from "react";

// Bindings live here as predicates rather than scattered `event.key === ...` checks: every surface
// agrees on what "next" means, and making them configurable later is a matter of feeding this table
// from settings instead of hunting the comparisons down. Vim's keys, with the arrows alongside for
// anyone who does not think in Vim.
type KeyLike = Pick<KeyboardEvent | ReactKeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey">;

export const keys = {
  // Bare "/" — a modified slash belongs to the browser (⌘/ and the like).
  openSearch: (event: KeyLike) => event.key === "/" && !event.ctrlKey && !event.metaKey && !event.altKey,
  // ⌘P opens it too: a Vim-keys browser extension takes "/" for itself before the page ever sees it,
  // and Quick Open is the chord an editor user already reaches for. Ctrl+P is deliberately not a
  // second spelling — it is "previous" in the result list right below.
  openSearchChord: (event: KeyLike) =>
    event.metaKey && !event.ctrlKey && !event.altKey && (event.key === "p" || event.key === "P"),
  next: (event: KeyLike) => event.key === "ArrowDown" || (event.ctrlKey && event.key === "n"),
  prev: (event: KeyLike) => event.key === "ArrowUp" || (event.ctrlKey && event.key === "p"),
  accept: (event: KeyLike) => event.key === "Enter" || (event.ctrlKey && event.key === "y"),
  close: (event: KeyLike) => event.key === "Escape",
};

// isTypingTarget answers "would this keystroke have been text?" — a global single-key shortcut must
// not fire while someone is writing a note or filling a field.
export function isTypingTarget(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null;
  if (!element || typeof element.tagName !== "string") return false;
  return (
    element.tagName === "INPUT" ||
    element.tagName === "TEXTAREA" ||
    element.tagName === "SELECT" ||
    element.isContentEditable === true
  );
}

// step moves a selection by one, wrapping at both ends the way a completion list does. -1 means
// nothing is selected yet, which happens whenever the result list changes under the cursor.
export function step(index: number, delta: number, count: number): number {
  if (count === 0) return -1;
  if (index < 0) return delta > 0 ? 0 : count - 1;
  return (index + delta + count) % count;
}
