import { fromMarkdown } from "mdast-util-from-markdown";
import { describe, expect, it } from "vitest";
import { remarkBlockEmbed, remarkWikiLink, spliceIncludeTokens, splitWikiTarget } from "./plugins";
import { wikiLinks } from "./wikiLinks";

describe("wikiLinks", () => {
  it.each([
    ["[[[test]]]", "[test]"],
    ["[[Plan [draft]]]", "Plan [draft]"],
    ["[[outer [inner [nested]]]]", "outer [inner [nested]]"],
    ["[[日本語 [下書き]]]", "日本語 [下書き]"],
    [String.raw`[[path\[draft]]]`, String.raw`path\[draft]`],
    ["[[Plan [draft]##Review [notes]]]", "Plan [draft]##Review [notes]"],
  ])("keeps the whole balanced target in %s", (source, target) => {
    expect(wikiLinks(source)).toEqual([{ start: 0, end: source.length, target, alias: undefined }]);
  });

  it("preserves offsets, aliases and adjacent links", () => {
    const first = "[[Plan [draft]|review [now]|later]]";
    const second = "[[[test]]]";
    const source = `前 ${first}${second} 後`;
    expect(wikiLinks(source)).toEqual([
      { start: 2, end: 2 + first.length, target: "Plan [draft]", alias: "review [now]|later" },
      { start: 2 + first.length, end: 2 + first.length + second.length, target: "[test]", alias: undefined },
    ]);
    expect(wikiLinks("[[Plan [draft]|]]")[0].alias).toBe("");
  });

  it.each(["[[a]b]]", "[[a[b]]", "[[]]", "[[  ]]", "[[|alias]]", "[[a\nb]]"])(
    "ignores malformed or empty candidates: %s", (source) => {
      expect(wikiLinks(source)).toEqual([]);
    },
  );

  it("recovers later links after a malformed opening", () => {
    expect(wikiLinks("[[bad]x]] [[Good]]\n[[broken [\n[[Next]]").map((link) => link.target))
      .toEqual(["Good", "Next"]);
    expect(wikiLinks("[[broken [ [[Good]]")).toEqual([]);
  });

  it("handles a large unterminated opening run without rescanning each bracket", () => {
    expect(wikiLinks("[".repeat(100_000))).toEqual([]);
  });
});

describe("remarkWikiLink", () => {
  it("marks only unaliased ID links for current-title display", () => {
    const tree = fromMarkdown("[[id:123]] [[work:id:123#Heading]] [[id:123|id:123]] [[123]]");
    remarkWikiLink()(tree);
    expect(tree.children[0]).toMatchObject({ children: [
      { data: { hProperties: { target: "id:123", resolveTitle: true } } },
      { type: "text" },
      { data: { hProperties: { target: "work:id:123#Heading", resolveTitle: true } } },
      { type: "text" },
      { data: { hProperties: { target: "id:123", display: "id:123" } } },
      { type: "text" },
      { data: { hProperties: { target: "123", display: "123" } } },
    ] });
    const children = (tree.children[0] as unknown as { children: { data?: { hProperties?: { resolveTitle?: boolean } } }[] }).children;
    expect(children[4].data?.hProperties?.resolveTitle).toBeUndefined();
    expect(children[6].data?.hProperties?.resolveTitle).toBeUndefined();
  });

  it("renders nested bracket targets and aliases without truncating their text", () => {
    const tree = fromMarkdown("before [[[test]]] and [[outer [inner [nested]]|alias [here]]] after");
    remarkWikiLink()(tree);
    expect(tree.children[0]).toMatchObject({
      type: "paragraph",
      children: [
        { type: "text", value: "before " },
        { type: "wikilink", data: { hProperties: { target: "[test]", display: "[test]" } } },
        { type: "text", value: " and " },
        { type: "wikilink", data: { hProperties: { target: "outer [inner [nested]]", display: "alias [here]" } } },
        { type: "text", value: " after" },
      ],
    });
  });

  it("leaves code and malformed input literal and falls back from an empty alias", () => {
    const tree = fromMarkdown("`[[Plan [draft]]]` [[a]b]] [[Plan [draft]|]]");
    remarkWikiLink()(tree);
    expect(tree.children[0]).toMatchObject({
      type: "paragraph",
      children: [
        { type: "inlineCode", value: "[[Plan [draft]]]" },
        { type: "text", value: " [[a]b]] " },
        { type: "wikilink", data: { hProperties: { target: "Plan [draft]", display: "Plan [draft]" } } },
      ],
    });
  });
});

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

describe("remarkBlockEmbed", () => {
  // Each top-level block as "<type>:<text>", so a stray empty paragraph shows up as "paragraph:".
  function blocks(markdown: string): string[] {
    const tree = fromMarkdown(markdown);
    remarkBlockEmbed()(tree);
    return tree.children.map((node) => {
      const kids = "children" in node ? node.children : [];
      const kind = kids.length === 1 ? kids[0].type : node.type;
      const text = kids
        .map((kid) => ("value" in kid ? kid.value : ""))
        .join("")
        .trim();
      return `${kind}:${text}`;
    });
  }

  it("splits the paragraph around an image sharing it with text", () => {
    expect(blocks("foo\n![y](https://example.com)\nbar")).toEqual([
      "text:foo",
      "image:",
      "text:bar",
    ]);
  });

  it("stacks images without leaving the line breaks between them as empty paragraphs", () => {
    expect(blocks("![a](a.png)\n![b](b.png)")).toEqual(["image:", "image:"]);
  });

  it("leaves a paragraph that is already a sole image, or has no image at all, alone", () => {
    expect(blocks("![a](a.png)")).toEqual(["image:"]);
    expect(blocks("foo\nbar")).toEqual(["text:foo\nbar"]);
  });
});
