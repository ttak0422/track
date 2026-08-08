import { describe, expect, it } from "vitest";
import { highlightSearchText } from "./searchHighlight";

describe("highlightSearchText", () => {
  it("highlights literal terms while leaving the display casing intact", () => {
    expect(highlightSearchText("Track the tracker", "TRAC")).toEqual([
      { text: "Trac", highlighted: true },
      { text: "k the ", highlighted: false },
      { text: "trac", highlighted: true },
      { text: "ker", highlighted: false },
    ]);
  });

  it("highlights every term but not uppercase boolean operators", () => {
    expect(highlightSearchText("alpha beta OR gamma", "alpha beta OR gamma")).toEqual([
      { text: "alpha", highlighted: true },
      { text: " ", highlighted: false },
      { text: "beta", highlighted: true },
      { text: " OR ", highlighted: false },
      { text: "gamma", highlighted: true },
    ]);
  });

  it("treats regex punctuation as plain search text", () => {
    expect(highlightSearchText("a+b [c]", "a+b")).toEqual([
      { text: "a+b", highlighted: true },
      { text: " [c]", highlighted: false },
    ]);
  });

  it("uses the search engine's Unicode fold", () => {
    expect(highlightSearchText("İSTANBUL", "istanbul")).toEqual([{ text: "İSTANBUL", highlighted: true }]);
  });
});
