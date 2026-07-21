import { describe, expect, it } from "vitest";
import { splitWikiTarget } from "./plugins";

describe("splitWikiTarget", () => {
  it("passes a plain key through", () => {
    expect(splitWikiTarget("Note")).toEqual({ key: "Note", blockID: "" });
  });

  it("splits a block anchor off the key", () => {
    expect(splitWikiTarget("Note#^intro-1")).toEqual({ key: "Note", blockID: "intro-1" });
  });

  it("resolves a heading anchor by its key, without a block id", () => {
    expect(splitWikiTarget("Note#Heading")).toEqual({ key: "Note", blockID: "" });
    expect(splitWikiTarget("Note##Deeper")).toEqual({ key: "Note", blockID: "" });
  });

  it("keeps a trailing # in the key when nothing follows it", () => {
    expect(splitWikiTarget("C#")).toEqual({ key: "C#", blockID: "" });
  });

  it("treats an invalid block id as a heading anchor", () => {
    expect(splitWikiTarget("Note#^not an id")).toEqual({ key: "Note", blockID: "" });
  });
});
