// Mindmap model: parse Markdown headings into a tree, and lay the
// tree out as a left-to-right node/edge diagram. Pure data — no React, no DOM — so it is unit-tested
// directly and renders identically in the browser and in the static prerender.

export interface MindmapNode {
  label: string;
  link?: MindmapLink;
  children: MindmapNode[];
}

export type MindmapLink =
  | { kind: "external"; href: string }
  | { kind: "wiki"; target: string };

interface Item {
  depth: number;
  label: string;
  link?: MindmapLink;
}

// outlineTree parses an indented outline (one node per line, deeper indent = child; optional -/*/+
// bullets are stripped) into a tree. The first line is the root; if several lines share the top level,
// they become branches of an implicit unlabeled root. Returns null for an empty outline.
// headingTree builds the tree of a note's Markdown headings: "## Section" nests under "# Title" and so
// on. Headings inside fenced code blocks are ignored. Returns null when the note has no headings.
export function headingTree(markdown: string): MindmapNode | null {
  const items: Item[] = [];
  let fence: string | null = null;
  for (const line of markdown.split("\n")) {
    const fenceMark = /^\s{0,3}(`{3,}|~{3,})/.exec(line)?.[1];
    if (fence !== null) {
      if (fenceMark && fenceMark[0] === fence[0] && fenceMark.length >= fence.length) fence = null;
      continue;
    }
    if (fenceMark) {
      fence = fenceMark;
      continue;
    }
    const heading = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(line);
    if (heading) {
      const { label, link } = parseLabel(heading[2]);
      items.push({ depth: heading[1].length, label, link });
    }
  }
  return treeFromItems(items);
}

// markdownTree uses Markdown headings for stable hierarchy and list items for leaves.
export function markdownTree(text: string): MindmapNode | null {
  const items: Item[] = [];
  let headingDepth = 0;
  for (const raw of text.split("\n")) {
    const heading = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(raw);
    if (heading) {
      headingDepth = heading[1].length * 10;
      const { label, link } = parseLabel(heading[2]);
      items.push({ depth: headingDepth, label, link });
      continue;
    }
    const list = /^(\s*)[-*+]\s+(.+?)\s*$/.exec(raw);
    if (!list || headingDepth === 0) continue;
    const { label, link } = parseLabel(list[2]);
    items.push({ depth: headingDepth + 1 + list[1].replaceAll("\t", "  ").length, label, link });
  }
  return treeFromItems(items);
}

// treeFromItems folds a depth-annotated sequence into a tree: each item becomes a child of the nearest
// preceding item with a smaller depth. When the sequence has no single top item (several share the
// minimum depth, as in a note with multiple "##" and no "#"), an implicit unlabeled root holds them.
function treeFromItems(items: Item[]): MindmapNode | null {
  if (items.length === 0) return null;
  const minDepth = Math.min(...items.map((it) => it.depth));
  const single = items[0].depth === minDepth && items.filter((it) => it.depth === minDepth).length === 1;
  const root: MindmapNode = single
    ? { label: items[0].label, link: items[0].link, children: [] }
    : { label: "", children: [] };
  const rest = single ? items.slice(1) : items;

  const stack: { depth: number; node: MindmapNode }[] = [{ depth: minDepth - 1, node: root }];
  for (const item of rest) {
    while (stack.length > 1 && stack[stack.length - 1].depth >= item.depth) stack.pop();
    const node: MindmapNode = { label: item.label, link: item.link, children: [] };
    stack[stack.length - 1].node.children.push(node);
    stack.push({ depth: item.depth, node });
  }
  return root;
}

export interface MindmapPlacedNode {
  label: string;
  link?: MindmapLink;
  depth: number;
  x: number; // left edge
  y: number; // top edge
  w: number;
  h: number;
}

export interface MindmapEdge {
  from: number; // index into nodes
  to: number;
}

export interface MindmapLayout {
  width: number;
  height: number;
  nodes: MindmapPlacedNode[];
  edges: MindmapEdge[];
}

export const mindmapNodeHeight = 26;
const rowPitch = 36;
const columnGap = 40;
const labelPadding = 10;

// layoutMindmap places the tree left-to-right: every leaf gets its own row, an inner node centers on
// its children, and each depth forms a column wide enough for its widest label. Coordinates are SVG
// user units at a 13px label font.
export function layoutMindmap(root: MindmapNode): MindmapLayout {
  const nodes: MindmapPlacedNode[] = [];
  const edges: MindmapEdge[] = [];
  const columnWidth: number[] = [];

  measure(root, 0);
  const columnX: number[] = [];
  let x = 0;
  for (const w of columnWidth) {
    columnX.push(x);
    x += w + columnGap;
  }

  let row = 0;
  place(root, 0);

  const width = Math.max(...nodes.map((n) => n.x + n.w));
  const height = Math.max(...nodes.map((n) => n.y + n.h));
  return { width, height, nodes, edges };

  function measure(node: MindmapNode, depth: number) {
    columnWidth[depth] = Math.max(columnWidth[depth] ?? 0, nodeWidth(node.label));
    for (const child of node.children) measure(child, depth + 1);
  }

  // place appends node (after its subtree, so children hold lower indexes), records the edges to its
  // direct children, and returns the node's vertical center plus its index for the parent's edge.
  function place(node: MindmapNode, depth: number): { centerY: number; index: number } {
    let centerY: number;
    const childIndexes: number[] = [];
    if (node.children.length === 0) {
      centerY = row * rowPitch + mindmapNodeHeight / 2;
      row++;
    } else {
      const placed = node.children.map((child) => place(child, depth + 1));
      childIndexes.push(...placed.map((p) => p.index));
      centerY = (placed[0].centerY + placed[placed.length - 1].centerY) / 2;
    }
    const index = nodes.length;
    nodes.push({
      label: node.label,
      link: node.link,
      depth,
      x: columnX[depth],
      y: centerY - mindmapNodeHeight / 2,
      w: nodeWidth(node.label),
      h: mindmapNodeHeight,
    });
    for (const to of childIndexes) edges.push({ from: index, to });
    return { centerY, index };
  }
}

function parseLabel(source: string): { label: string; link?: MindmapLink } {
  const wiki = /^\[\[([^|\]]+)(?:\|([^\]]+))?\]\]$/.exec(source);
  if (wiki) return { label: wiki[2] ?? wiki[1], link: { kind: "wiki", target: wiki[1] } };
  const external = /^\[([^\]]+)\]\(([^\s)]+)\)$/.exec(source);
  if (external) return { label: external[1], link: { kind: "external", href: external[2] } };
  return { label: source };
}

// nodeWidth estimates the rendered width of a 13px label without a canvas: CJK and other wide
// codepoints count double. ponytail: heuristic metric; swap for canvas measureText if labels clip.
export function nodeWidth(label: string): number {
  if (label === "") return 14; // the implicit root renders as a small dot
  let units = 0;
  for (const ch of label) {
    units += ch.codePointAt(0)! > 0x2e7f ? 2 : 1;
  }
  return Math.max(34, units * 7.2 + labelPadding * 2);
}
