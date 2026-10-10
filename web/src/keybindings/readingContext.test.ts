import { describe, expect, it } from "vitest";
import { hasReadingOverlay, isReadingTextTarget } from "./readingContext";

function fixture(html: string) {
  const reader = document.createElement("section");
  reader.tabIndex = 0;
  reader.innerHTML = `<div class="note-preview">${html}</div>`;
  document.body.append(reader);
  return reader;
}

describe("reading focus context", () => {
  it("accepts prose, but not the aside or another reader", () => {
    const reader = fixture("<p><span>Prose</span></p>");
    expect(isReadingTextTarget(reader.querySelector("span"), reader)).toBe(true);
    expect(isReadingTextTarget(reader, reader)).toBe(true);
    const aside = document.createElement("aside");
    reader.append(aside);
    expect(isReadingTextTarget(aside, reader)).toBe(false);
    expect(isReadingTextTarget(document.body, reader)).toBe(false);
    reader.remove();
  });
  it.each(["<button><span>x</span></button>", '<a href="#"><span>x</span></a>',
    '<div contenteditable="true"><span>x</span></div>', '<div contenteditable="plaintext-only"><span>x</span></div>',
    '<div tabindex="0"><span>x</span></div>', '<div role="slider"><span>x</span></div>',
    "<input>", "<textarea></textarea>", "<select></select>", "<summary>x</summary>", "<iframe></iframe>", "<canvas></canvas>"])("preserves interactive subtree %s", (html) => {
    const reader = fixture(html);
    const element = reader.querySelector("span") ?? reader.querySelector(".note-preview")!.firstElementChild;
    expect(isReadingTextTarget(element, reader)).toBe(false);
    reader.remove();
  });
  it.each(['role="dialog"', 'role="alertdialog"', 'aria-modal="true"', 'role="menu"', 'class="menu-panel"', 'class="rail-menu-panel"', 'class="selection-copy"'])("blocks an open overlay %s", (attribute) => {
    const element = document.createElement("div");
    element.innerHTML = `<div ${attribute}></div>`;
    document.body.append(element);
    expect(hasReadingOverlay(document)).toBe(true);
    element.remove();
    expect(hasReadingOverlay(document)).toBe(false);
  });
});
