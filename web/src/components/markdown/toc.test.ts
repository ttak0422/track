import { describe, expect, it } from "vitest";
import { headingElementID, scanHeadings, tocEntries } from "./toc";

describe("scanHeadings", () => {
  it("reads ATX headings and skips fenced code", () => {
    const body = "# One\n\n```\n## Not a heading\n```\n\n## Two\n### Three";
    expect(scanHeadings(body).map((h) => `${h.level}:${h.text}`)).toEqual([
      "1:One",
      "2:Two",
      "3:Three",
    ]);
  });

  it("closes a fence only with one at least as long", () => {
    const body = "````\n```\n## Still fenced\n````\n\n# Out";
    expect(scanHeadings(body).map((h) => h.text)).toEqual(["Out"]);
  });
});

describe("tocEntries", () => {
  it("strips link and emphasis syntax from the label", () => {
    const entries = tocEntries("# See [[Other Note|the note]]\n## A *bold* `code` word");
    expect(entries.map((e) => e.text)).toEqual(["See the note", "A bold code word"]);
  });

  it("keeps Japanese headings addressable", () => {
    // A slug rule that dropped non-ASCII would collapse these to one empty id.
    const entries = tocEntries("# 設計\n# 実装");
    expect(entries.map((e) => e.id)).toEqual(["設計", "実装"]);
  });

  it("numbers repeated headings in document order", () => {
    const entries = tocEntries("# Notes\n## Notes\n## Notes");
    expect(entries.map((e) => e.id)).toEqual(["notes", "notes-2", "notes-3"]);
  });

  it("gives a heading with no sluggable characters a fallback id", () => {
    expect(tocEntries("# ???").map((e) => e.id)).toEqual(["section"]);
  });
});

describe("headingElementID", () => {
  it("namespaces the id so it cannot collide with a block anchor", () => {
    expect(headingElementID("intro")).toBe("h-intro");
  });
});
