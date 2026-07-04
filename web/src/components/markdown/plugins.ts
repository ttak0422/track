import type { Element, Root as HastRoot, Text as HastText } from "hast";
import type { Root as MdastRoot } from "mdast";
import { visit } from "unist-util-visit";

const wikiPattern = /\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g;

// remarkWikiLink rewrites [[target|display]] text into a custom "wikilink" element carrying the target
// and display as properties, so markdownComponents can render it as a navigable, hover-previewable link.
export function remarkWikiLink() {
  return (tree: MdastRoot) => {
    visit(tree, "text", (node, index, parent) => {
      if (!parent || index === undefined) return;
      const value = node.value;
      wikiPattern.lastIndex = 0;
      if (!wikiPattern.test(value)) return;
      wikiPattern.lastIndex = 0;
      const replacement: unknown[] = [];
      let last = 0;
      let match: RegExpExecArray | null;
      while ((match = wikiPattern.exec(value)) !== null) {
        if (match.index > last) {
          replacement.push({ type: "text", value: value.slice(last, match.index) });
        }
        const target = match[1].trim();
        const display = (match[2] ?? match[1]).trim();
        replacement.push({
          type: "wikilink",
          data: { hName: "wikilink", hProperties: { target, display } },
          children: [],
        });
        last = wikiPattern.lastIndex;
      }
      if (last < value.length) {
        replacement.push({ type: "text", value: value.slice(last) });
      }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      parent.children.splice(index, 1, ...(replacement as any[]));
      return index + replacement.length;
    });
  };
}

// makeRehypeBudoux builds a rehype plugin that segments Japanese text at BudouX phrase boundaries.
// Paired with CSS `word-break: keep-all`, the inserted <wbr> markers let lines wrap between phrases
// instead of at arbitrary characters. The BudouX parser is injected (not imported here) so its ~190KB
// Japanese model is loaded only when this plugin is used — never in the English static help site (see
// ./budoux). It runs on the rendered tree (after wiki links and code are elements); text inside code/pre
// is left untouched.
export function makeRehypeBudoux(parse: (text: string) => string[]) {
  return function rehypeBudoux() {
    return (tree: HastRoot) => {
      visit(tree, "text", (node, index, parent) => {
        if (!parent || index === undefined) return;
        if (parent.type === "element" && (parent.tagName === "code" || parent.tagName === "pre")) {
          return;
        }
        const segments = parse(node.value);
        if (segments.length <= 1) return;
        const replacement: (HastText | Element)[] = [];
        segments.forEach((segment, i) => {
          if (i > 0) {
            replacement.push({ type: "element", tagName: "wbr", properties: {}, children: [] });
          }
          replacement.push({ type: "text", value: segment });
        });
        parent.children.splice(index, 1, ...replacement);
        return index + replacement.length;
      });
    };
  };
}
