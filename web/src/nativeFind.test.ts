// @ts-expect-error node builtin — tests run in Node
import { readFileSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const source = readFileSync("../desktop/Sources/TrackWebApp/PageFindScript.swift", "utf8").split('#"""')[1].split('"""#')[0];
interface FindState { supported: boolean; count: number; index: number; text: string; id: string; }
interface PageFind { update(query: string, delta?: number, restart?: boolean): FindState; clear(): FindState; snapshot(): FindState; }
const page = () => (globalThis as unknown as { __trackPageFind: PageFind }).__trackPageFind;
let highlights: Map<string, Set<Range>>;
const rect = { x: 20, y: 80, top: 80, bottom: 100, left: 20, right: 120, width: 100, height: 20, toJSON() {} };
beforeEach(() => {
  document.body.innerHTML = '<nav>Alpha chrome</nav><section class="reader"><p id="first">Alpha <em>al</em>pha ALPHA.</p><p id="second">alpha beta</p><p hidden>alpha</p><textarea>alpha draft</textarea><div contenteditable="true">alpha editor</div><aside class="note-aside">alpha graph</aside></section>';
  highlights = new Map();
  vi.stubGlobal("Highlight", class extends Set<Range> { priority = 0; });
  vi.stubGlobal("CSS", { highlights });
  vi.stubGlobal("matchMedia", () => ({ matches: false }));
  Object.defineProperty(Range.prototype, "getClientRects", { configurable: true, value: vi.fn(function (this: Range) {
    return (this.startContainer.parentElement?.closest('details:not([open])') ? [] : [rect]) as unknown as DOMRectList;
  }) });
  Object.defineProperty(Range.prototype, "getBoundingClientRect", { configurable: true, value: vi.fn(() => rect) });
  vi.spyOn(window, "scrollBy").mockImplementation(() => {});
  window.eval(source);
});
afterEach(() => {
  page()?.clear();
  delete (globalThis as unknown as { __trackPageFind?: PageFind }).__trackPageFind;
  document.getElementById("track-native-find-style")?.remove();
  window.history.replaceState({}, "", "/");
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});

describe("isolated native page Find", () => {
  it("highlights every rendered literal match across inline nodes with a distinct current range", () => {
    expect(page().update("alpha")).toMatchObject({ supported: true, count: 4, index: 0, text: "Alpha", id: "first" });
    expect([...highlights.get("track-find-all")!].map(range => range.toString())).toEqual(["Alpha", "alpha", "ALPHA", "alpha"]);
    expect([...highlights.get("track-find-current")!].map(range => range.toString())).toEqual(["Alpha"]);
    expect(document.querySelectorAll("mark")).toHaveLength(0);
  });
  it("navigates and wraps the current match independently of ordinary selection", () => {
    const selection = window.getSelection()!;
    const original = document.createRange(); original.selectNodeContents(document.querySelector("em")!);
    selection.removeAllRanges(); selection.addRange(original);
    const before = selection.toString();
    page().update("alpha");
    expect(page().update("alpha", -1)).toMatchObject({ count: 4, index: 3, id: "second" });
    expect(page().update("alpha", 1)).toMatchObject({ index: 0 });
    expect(page().update("alpha", 1)).toMatchObject({ index: 1 });
    expect(selection.toString()).toBe(before);
    page().clear();
    expect(selection.toString()).toBe(before);
    expect(highlights.size).toBe(0);
    expect(page().update("alpha", 1)).toMatchObject({ index: 0 }); // Cmd+G with the panel closed
    page().clear();
    expect(page().update("alpha", -1)).toMatchObject({ index: 3 });
  });
  it("handles repeated text and line breaks without matching across separate paragraphs", () => {
    document.querySelector(".reader")!.innerHTML = '<p>alpha<br><strong style="display:inline-block">beta</strong> alpha\n beta</p><p>alpha</p><p>beta</p>';
    expect(page().update("ALPHA beta")).toMatchObject({ count: 2 });
    expect([...highlights.get("track-find-all")!].map(range => range.toString())).toEqual(["alphabeta", "alpha\n beta"]);
  });
  it("treats regex and HTML syntax as text and clears stale matches on a missing or empty query", () => {
    const query = '<img src=x onerror="alert(1)"> [a.*]';
    document.querySelector(".reader")!.textContent = query;
    expect(page().update(query)).toMatchObject({ count: 1, text: query });
    expect(document.querySelector("img")).toBeNull();
    expect(page().update("[a.*]")).toMatchObject({ count: 1, text: "[a.*]" });
    expect(page().update("missing")).toMatchObject({ count: 0, index: -1 });
    expect(highlights.get("track-find-current")!.size).toBe(0);
    expect(page().update("")).toMatchObject({ count: 0 });
    expect(highlights.size).toBe(0);
  });
  it("refreshes ranges after rendered content and route changes without retaining stale results", async () => {
    page().update("alpha"); page().update("alpha", 1);
    document.querySelector(".reader")!.innerHTML = '<p id="changed">alpha</p>';
    await Promise.resolve();
    expect(page().update("alpha")).toMatchObject({ count: 1, index: 0, id: "changed" });
    window.history.pushState({}, "", "/notes/next");
    document.querySelector(".reader")!.innerHTML = '<p>nothing</p>';
    await Promise.resolve();
    expect(page().update("alpha")).toMatchObject({ count: 0, index: -1 });
  });
  it("excludes non-rendered details and CSS-hidden text while retaining offscreen matches", () => {
    document.querySelector(".reader")!.innerHTML = '<p style="display:none">alpha</p><p style="visibility:hidden">alpha</p><details><summary>closed</summary><p>alpha</p></details><p style="margin-top:2000px">alpha</p>';
    expect(page().update("alpha")).toMatchObject({ count: 1 });
  });
  it("keeps the existing WebKit single-match fallback available when CSS Highlights is absent", () => {
    delete (globalThis as unknown as { __trackPageFind?: PageFind }).__trackPageFind;
    vi.stubGlobal("Highlight", undefined);
    window.eval(source);
    expect(page().update("alpha")).toMatchObject({ supported: false, count: 0 });
    expect(highlights.size).toBe(0);
  });
});
