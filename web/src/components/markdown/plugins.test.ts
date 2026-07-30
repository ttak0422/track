import { describe, expect, it } from "vitest";
import { splitWikiTarget } from "./plugins";

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
