import { describe, expect, it } from "vitest";
import { splitSlides } from "./slides";

describe("splitSlides", () => {
  it("returns the whole body as one slide when there is no separator", () => {
    const slides = splitSlides("# Title\n\nsome text");
    expect(slides).toHaveLength(1);
    expect(slides[0]).toEqual({ text: "# Title\n\nsome text", startLine: 0, lineCount: 3 });
  });

  it("splits on --- thematic breaks", () => {
    const slides = splitSlides("one\n\n---\n\ntwo\n\n- - -\n\nthree");
    expect(slides.map((s) => s.text.trim())).toEqual(["one", "two", "three"]);
  });

  it("records line offsets so include line numbers can be rebased", () => {
    const slides = splitSlides("a\n\n---\n\nb\nc");
    expect(slides[0]).toMatchObject({ startLine: 0, lineCount: 2 });
    // Slide 2 starts on the line after the separator ("", "b", "c").
    expect(slides[1]).toMatchObject({ startLine: 3, lineCount: 3 });
  });

  it("ignores --- inside fenced code blocks", () => {
    const slides = splitSlides("before\n\n```\n---\n```\n\nafter\n\n---\n\nnext");
    expect(slides).toHaveLength(2);
    expect(slides[0].text).toContain("```\n---\n```");
  });

  it("treats --- under paragraph text as a setext underline, not a separator", () => {
    const slides = splitSlides("Heading text\n---\n\nbody");
    expect(slides).toHaveLength(1);
  });

  it("splits on a --- directly under an ATX heading", () => {
    const slides = splitSlides("# Title\n---\n\nbody");
    expect(slides.map((s) => s.text.trim())).toEqual(["# Title", "body"]);
  });

  it("does not split on *** or ___ rules", () => {
    expect(splitSlides("one\n\n***\n\ntwo\n\n___\n\nthree")).toHaveLength(1);
  });

  it("drops blank slides from leading, trailing, and doubled separators", () => {
    const slides = splitSlides("---\n\none\n\n---\n\n---\n\ntwo\n\n---\n");
    expect(slides.map((s) => s.text.trim())).toEqual(["one", "two"]);
  });

  it("does not treat table delimiter rows as separators", () => {
    const slides = splitSlides("| a | b |\n| --- | --- |\n| 1 | 2 |");
    expect(slides).toHaveLength(1);
  });
});
