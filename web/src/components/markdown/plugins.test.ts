import { describe, expect, it } from "vitest";
import { spliceIncludeTokens, splitWikiTarget } from "./plugins";

describe("splitWikiTarget", () => {
  it("passes a plain key through", () => {
    expect(splitWikiTarget("Note")).toEqual({ key: "Note", blockID: "", headingID: "" });
  });

  it("splits a block anchor off the key", () => {
    expect(splitWikiTarget("Note#^intro-1")).toEqual({ key: "Note", blockID: "intro-1", headingID: "" });
  });

  it("resolves a heading anchor by its key, carrying the heading's id", () => {
    expect(splitWikiTarget("Note#Heading")).toEqual({ key: "Note", blockID: "", headingID: "heading" });
    // Extra "#"s are the heading level, which the id ignores — a link names a heading by text.
    expect(splitWikiTarget("Note##Deeper")).toEqual({ key: "Note", blockID: "", headingID: "deeper" });
  });

  it("keeps a trailing # in the key when nothing follows it", () => {
    expect(splitWikiTarget("C#")).toEqual({ key: "C#", blockID: "", headingID: "" });
  });

  it("treats an invalid block id as a heading anchor", () => {
    expect(splitWikiTarget("Note#^not an id")).toEqual({ key: "Note", blockID: "", headingID: "not-an-id" });
  });
});

describe("spliceIncludeTokens", () => {
  // The rendered body has to stay line-aligned with the note file: a task row resolves to the
  // engine-parsed task by line number, so a shift here writes a state change to the wrong task.
  function lineOf(markdown: string, needle: string): number {
    return markdown.split("\n").findIndex((line) => line.includes(needle)) + 1;
  }

  it("replaces the directive with a single line, keeping the lines below in place", () => {
    const body = "# Title\n![[Other]]\n\n- [/] a task [#A]";
    const spliced = spliceIncludeTokens(body, [1]);
    expect(lineOf(spliced, "a task")).toBe(lineOf(body, "a task"));
    expect(spliced.split("\n")).toHaveLength(body.split("\n").length);
  });

  it("stays 1:1 with several includes, and mid-paragraph", () => {
    const body = "before\n![[One]]\nafter\n![[Two]]\n- [ ] tail";
    const spliced = spliceIncludeTokens(body, [1, 3]);
    expect(lineOf(spliced, "tail")).toBe(lineOf(body, "tail"));
  });

  it("leaves a line that is no longer a directive alone", () => {
    const body = "# Title\nplain text";
    expect(spliceIncludeTokens(body, [1])).toBe(body);
  });
});
