import { describe, expect, it } from "vitest";
import { headingTree, layoutMindmap, markdownTree } from "./mindmap";

describe("headingTree", () => {
  it("builds the heading hierarchy and skips a level jump gracefully", () => {
    const tree = headingTree("# Title\n\ntext\n\n## A\n\n#### A-deep\n\n## B");
    expect(tree?.label).toBe("Title");
    expect(tree?.children.map((c) => c.label)).toEqual(["A", "B"]);
    expect(tree?.children[0].children[0].label).toBe("A-deep");
  });

  it("ignores headings inside fenced code blocks", () => {
    const tree = headingTree("# T\n```md\n## not a heading\n```\n## real");
    expect(tree?.children.map((c) => c.label)).toEqual(["real"]);
  });

  it("keeps a four-backtick fence open across an inner three-backtick fence", () => {
    // The pattern help pages use to show a fence inside a fence.
    const md = "# T\n````markdown\n```mindmap\n## not real\n```\n````\n## real";
    const tree = headingTree(md);
    expect(tree?.children.map((c) => c.label)).toEqual(["real"]);
  });

  it("holds multiple top-level headings under an implicit root", () => {
    const tree = headingTree("## One\n## Two");
    expect(tree?.label).toBe("");
    expect(tree?.children.map((c) => c.label)).toEqual(["One", "Two"]);
  });

  it("returns null when there are no headings", () => {
    expect(headingTree("just prose\n\n- list")).toBeNull();
  });
});

describe("markdownTree", () => {
  it("uses headings for hierarchy and list items for linked leaves", () => {
    const tree = markdownTree("# Mindmap\n\n## Links\n- [GitHub](https://github.com/ttak0422/track)\n\n## Notes\n- [[sample]]");
    expect(tree).toEqual({
      label: "Mindmap",
      children: [
        { label: "Links", children: [{ label: "GitHub", link: { kind: "external", href: "https://github.com/ttak0422/track" }, children: [] }] },
        { label: "Notes", children: [{ label: "sample", link: { kind: "wiki", target: "sample" }, children: [] }] },
      ],
    });
  });
});

describe("layoutMindmap", () => {
  const tree = markdownTree("# Root\n## Left\n### Deep\n## Right");

  it("places children one column right of their parent", () => {
    const layout = layoutMindmap(tree!);
    const byLabel = Object.fromEntries(layout.nodes.map((n) => [n.label, n]));
    expect(byLabel.Left.x).toBeGreaterThan(byLabel.Root.x + byLabel.Root.w);
    expect(byLabel.Deep.x).toBeGreaterThan(byLabel.Left.x + byLabel.Left.w);
    expect(byLabel.Left.x).toBe(byLabel.Right.x); // same depth, same column
  });

  it("gives each leaf its own row and centers the parent on its children", () => {
    const layout = layoutMindmap(tree!);
    const byLabel = Object.fromEntries(layout.nodes.map((n) => [n.label, n]));
    expect(byLabel.Deep.y).not.toBe(byLabel.Right.y);
    const center = (n: { y: number; h: number }) => n.y + n.h / 2;
    expect(center(byLabel.Root)).toBeCloseTo((center(byLabel.Left) + center(byLabel.Right)) / 2);
  });

  it("records one edge per parent-child pair, resolvable by index", () => {
    const layout = layoutMindmap(tree!);
    expect(layout.edges).toHaveLength(3);
    const pairs = layout.edges.map((e) => `${layout.nodes[e.from].label}->${layout.nodes[e.to].label}`);
    expect(pairs.sort()).toEqual(["Left->Deep", "Root->Left", "Root->Right"]);
  });

  it("sizes the canvas to enclose every node", () => {
    const layout = layoutMindmap(tree!);
    for (const n of layout.nodes) {
      expect(n.x + n.w).toBeLessThanOrEqual(layout.width);
      expect(n.y + n.h).toBeLessThanOrEqual(layout.height);
      expect(n.y).toBeGreaterThanOrEqual(0);
    }
  });
});
