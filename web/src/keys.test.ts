import { describe, expect, it } from "vitest";
import { isTypingTarget, keys, step } from "./keys";

function press(key: string, modifiers: { ctrlKey?: boolean; metaKey?: boolean; altKey?: boolean } = {}) {
  return { key, ctrlKey: false, metaKey: false, altKey: false, ...modifiers };
}

describe("keys", () => {
  it("opens search on a bare slash only", () => {
    expect(keys.openSearch(press("/"))).toBe(true);
    expect(keys.openSearch(press("/", { metaKey: true }))).toBe(false);
    expect(keys.openSearch(press("?"))).toBe(false);
  });

  it("also opens search on the Quick Open chord, but never on Ctrl+P", () => {
    expect(keys.openSearchChord(press("p", { metaKey: true }))).toBe(true);
    expect(keys.openSearchChord(press("P", { metaKey: true }))).toBe(true);
    expect(keys.openSearchChord(press("p"))).toBe(false);
    // Ctrl+P is "previous" in the result list; it must not double as "open".
    expect(keys.openSearchChord(press("p", { ctrlKey: true }))).toBe(false);
    expect(keys.prev(press("p", { ctrlKey: true }))).toBe(true);
  });

  it("takes both the Vim keys and the arrows", () => {
    expect(keys.next(press("n", { ctrlKey: true }))).toBe(true);
    expect(keys.next(press("ArrowDown"))).toBe(true);
    expect(keys.prev(press("p", { ctrlKey: true }))).toBe(true);
    expect(keys.prev(press("ArrowUp"))).toBe(true);
    expect(keys.accept(press("y", { ctrlKey: true }))).toBe(true);
    expect(keys.accept(press("Enter"))).toBe(true);
  });

  it("does not confuse an unmodified letter for a binding", () => {
    expect(keys.next(press("n"))).toBe(false);
    expect(keys.prev(press("p"))).toBe(false);
    expect(keys.accept(press("y"))).toBe(false);
  });
});

describe("isTypingTarget", () => {
  it("recognises the places a keystroke would have been text", () => {
    expect(isTypingTarget(document.createElement("input"))).toBe(true);
    expect(isTypingTarget(document.createElement("textarea"))).toBe(true);
    expect(isTypingTarget(document.createElement("div"))).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });
});

describe("step", () => {
  it("wraps at both ends", () => {
    expect(step(0, 1, 3)).toBe(1);
    expect(step(2, 1, 3)).toBe(0);
    expect(step(0, -1, 3)).toBe(2);
  });

  it("enters the list from whichever end the key points at", () => {
    expect(step(-1, 1, 3)).toBe(0);
    expect(step(-1, -1, 3)).toBe(2);
  });

  it("selects nothing when there is nothing", () => {
    expect(step(0, 1, 0)).toBe(-1);
  });
});
