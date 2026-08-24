import { describe, expect, it } from "vitest";
import { portableToHtml, toPortableMarkdown } from "./portable";

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

  it("leaves ordinary markdown and standard links untouched", () => {
    const md = "# Title\n\n- item\n\n[text](https://x.example) and `code`";
    expect(toPortableMarkdown(md)).toBe(md);
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
});
