import { describe, expect, it } from "vitest";
import { confluencePlainText, portableToHtml, stripTableDelimiterRows, toPortableMarkdown } from "./portable";

describe("toPortableMarkdown", () => {
  it("flattens a bare wiki link to its key", () => {
    expect(toPortableMarkdown("see [[a]] here")).toBe("see a here");
  });

  it("flattens an aliased wiki link to its alias", () => {
    expect(toPortableMarkdown("see [[a|b]] here")).toBe("see b here");
  });

  it("drops the heading anchor from a bare wiki link", () => {
    expect(toPortableMarkdown("[[note##bar]]")).toBe("note");
  });

  it("reduces an include line to the referenced title, dropping the marker and options", () => {
    expect(toPortableMarkdown("![[a]]")).toBe("a");
    expect(toPortableMarkdown("![[Note##h|excerpt]] :only-contents :lines 1-3")).toBe("excerpt");
  });

  it("copies bracketed titles, nested brackets, aliases and heading targets in full", () => {
    expect(toPortableMarkdown("[[[test]]] [[outer [inner [nested]]]][[Plan [draft]|review [now]]]"))
      .toBe("[test] outer [inner [nested]]review [now]");
    expect(toPortableMarkdown("[[Plan [draft]##Review]] [[Plan [draft]|]]")).toBe("Plan [draft] Plan [draft]");
  });

  it("flattens bracketed include titles without copying the directive options", () => {
    expect(toPortableMarkdown("  ![[[test]]] :only-contents\n![[Plan [draft]|review [now]]] :lines 1-3\n"))
      .toBe("  [test]\nreview [now]\n");
    expect(toPortableMarkdown("inline ![[Plan [draft]]] stays")).toBe("inline !Plan [draft] stays");
  });

  it("leaves malformed bracket candidates and their surrounding text unchanged", () => {
    expect(toPortableMarkdown("[[a]b]] ![[Plan [draft]] :lines 1-3\n[[Good]]"))
      .toBe("[[a]b]] ![[Plan [draft]] :lines 1-3\nGood");
  });

  it("leaves ordinary markdown and standard links untouched", () => {
    const md = "# Title\n\n- item\n\n[text](https://x.example) and `code`";
    expect(toPortableMarkdown(md)).toBe(md);
  });

  it("removes GFM table delimiter rows for copied values", () => {
    expect(stripTableDelimiterRows("| a | b |\n|---|---|\n| 1 | 2 |\n")).toBe("| a | b |\n| 1 | 2 |\n");
  });

  it("turns HTML breaks into actual line breaks in the Confluence text flavor", () => {
    expect(confluencePlainText("| a<br>b | c |\n| --- | --- |\n")).toBe("| a\nb | c |\n");
  });
});

describe("portableToHtml", () => {
  it("renders portable markdown as semantic HTML with GFM tables", async () => {
    const html = await portableToHtml("| a | b |\n| --- | --- |\n| 1 | 2 |");
    expect(html).toContain("<table>");
    expect(html).toContain("<th>a</th>");
    expect(html).toContain("<td>2</td>");
  });

  it("renders a wiki link already flattened by toPortableMarkdown as plain text", async () => {
    const html = await portableToHtml(toPortableMarkdown("see [[a|b]] here"));
    expect(html).toBe("<p>see b here</p>");
  });

  it("keeps table-cell breaks as HTML breaks", async () => {
    const html = await portableToHtml("| a<br>b | c |\n| --- | --- |\n| 1 | 2 |");
    expect(html).toContain("<th>a<br/>\nb</th>");
  });
});
