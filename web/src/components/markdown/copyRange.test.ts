import { afterEach, describe, expect, it } from "vitest";
import { copyLineRangeMarkdown, copyLineRangeText, resolveCopyLineRange } from "./copyRange";

afterEach(() => document.body.replaceChildren());

function fixture(markdown = "a😀b") {
  const scope = document.createElement("div");
  const block = document.createElement("p");
  block.dataset.copyLineStart = "1";
  block.dataset.copyLineEnd = "1";
  const mapped = document.createElement("span");
  mapped.dataset.copySourceStart = "0";
  mapped.dataset.copySourceEnd = String(markdown.length);
  mapped.textContent = markdown;
  block.append(mapped);
  scope.append(block);
  document.body.append(scope);
  return { scope, block, mapped, text: mapped.firstChild! };
}

describe("copy range", () => {
  it("keeps existing inclusive line references and whole-line Markdown copying", () => {
    expect(copyLineRangeText("a.md", { start: 2, end: 2 })).toBe("a.md:2");
    const range = { start: 1, end: 2, source: { start: { line: 1, column: 2 }, end: { line: 2, column: 1 } } };
    expect(copyLineRangeText("a.md", range)).toBe("a.md:1:2-2:1");
    expect(copyLineRangeMarkdown("first\nsecond\nthird", range)).toBe("first\nsecond");
  });

  it("resolves normalized element boundaries without relying on selection direction", () => {
    const { scope, block, mapped } = fixture();
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(block, 1, mapped, 0);
    const range = resolveCopyLineRange(scope, selection, "a😀b")!;
    expect(copyLineRangeText("a.md", range)).toBe("a.md:1:1-1:5");
  });

  it("falls back when a boundary splits a surrogate pair or mapped text has changed", () => {
    const { scope, text } = fixture();
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(text, 2, text, 3);
    expect(resolveCopyLineRange(scope, selection, "a😀b")).toEqual({ start: 1, end: 1 });
    selection.setBaseAndExtent(text, 1, text, 3);
    expect(copyLineRangeText("a.md", resolveCopyLineRange(scope, selection, "a😀b")!)).toBe("a.md:1:2-1:4");
    expect(resolveCopyLineRange(scope, selection, "xxxx")).toEqual({ start: 1, end: 1 });
  });

  it("rejects controls, selections outside this body, collapsed and multiple ranges", () => {
    const { scope, block, text } = fixture();
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(text, 0, text, 0);
    expect(resolveCopyLineRange(scope, selection, "a😀b")).toBeNull();
    const other = fixture("outside");
    selection.setBaseAndExtent(text, 0, other.text, 1);
    expect(resolveCopyLineRange(scope, selection, "a😀b")).toBeNull();
    const button = document.createElement("button");
    button.textContent = "Copy";
    block.append(button);
    selection.setBaseAndExtent(button.firstChild!, 0, button.firstChild!, 4);
    expect(resolveCopyLineRange(scope, selection, "a😀b")).toBeNull();
    expect(resolveCopyLineRange(scope, { isCollapsed: false, rangeCount: 2 } as Selection, "a😀b")).toBeNull();
    expect(resolveCopyLineRange(scope, null, "a😀b")).toBeNull();
  });
});
