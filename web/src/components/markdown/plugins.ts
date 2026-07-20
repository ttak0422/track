import type { Element, Root as HastRoot, Text as HastText } from "hast";
import type { Root as MdastRoot } from "mdast";
import { visit } from "unist-util-visit";
import type { TaskState } from "../../types";

// The [[target|display]] wiki-link grammar (target, optional |display alias). Shared with the portable
// export so both flatten the same construct. It carries the /g flag; reset lastIndex before manual exec.
export const wikiPattern = /\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g;

// Include directives (ADR 0031) reach the renderer as data, not syntax: the server resolves each
// ![[...]] line and reports its 0-based line number, so the client never re-implements the
// directive grammar. spliceIncludeTokens swaps those lines for placeholder paragraphs, and
// remarkInclude turns each placeholder into a custom "trackinclude" element that markdownComponents
// renders as an embed card. The token carries the include's array index.
const includeToken = "%%track-include-";
const includeTokenPattern = /^%%track-include-(\d+)%%$/;

export function spliceIncludeTokens(markdown: string, lineNumbers: number[]): string {
  const lines = markdown.split("\n");
  lineNumbers.forEach((line, i) => {
    // Trust but verify: a stale line number (body edited since the response) must not swallow an
    // unrelated line, so only a line that really is a directive is replaced.
    if (lines[line]?.trimStart().startsWith("![[")) {
      // Blank padding makes the token its own paragraph even mid-paragraph-block.
      lines[line] = `\n${includeToken}${i}%%\n`;
    }
  });
  return lines.join("\n");
}

export function remarkInclude() {
  return (tree: MdastRoot) => {
    visit(tree, "paragraph", (node, index, parent) => {
      if (!parent || index === undefined || node.children.length !== 1) return;
      const child = node.children[0];
      if (child.type !== "text") return;
      const match = includeTokenPattern.exec(child.value.trim());
      if (!match) return;
      parent.children[index] = {
        type: "trackinclude",
        data: { hName: "trackinclude", hProperties: { index: Number(match[1]) } },
        children: [],
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- custom mdast node
      } as any;
    });
  };
}

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

// remarkTaskLine upgrades checklists that use task notation (docs/help/tasks.md) into rich task
// rows, deciding per list block: when any item of a list carries notation beyond a bare GFM
// checkbox — a custom state marker, or a [#A]/[sched:]/[due:]/[done:]/cookie token — every task
// line in that list renders as a row with a state badge, the text, metadata chips, and (live) the
// same state select the board's cards carry. A checklist of plain "- [ ]"/"- [x]" lines keeps its
// native checkboxes. A marker outside the state set is not a task and stays exactly as written.
const taskTokenPattern = /\[(?:#([A-Za-z])|(sched|due|done):(\d{4}-\d{2}-\d{2})|(\d+\/\d+|\d+%))\]/g;

interface TaskItemParse {
  state: TaskState;
  // custom is true when the marker was a literal "[c]" in the text, not a GFM-parsed checkbox.
  custom: boolean;
  hasTokens: boolean;
  prefixLen: number;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function parseTaskItem(item: any, byChar: Map<string, TaskState>): TaskItemParse | null {
  const para = item.children?.[0];
  if (!para || para.type !== "paragraph") return null;
  let state: TaskState | undefined;
  let custom = false;
  let prefixLen = 0;
  if (typeof item.checked === "boolean") {
    state = byChar.get(item.checked ? "x" : " ");
    if (!state) return null; // this vault gives those markers no meaning — keep the GFM checkbox
  } else {
    const first = para.children?.[0];
    if (!first || first.type !== "text") return null;
    const m = /^\[(.)\][ \t]+/.exec(first.value);
    if (!m) return null;
    state = byChar.get(m[1]);
    if (!state) return null;
    custom = true;
    prefixLen = m[0].length;
  }
  let hasTokens = false;
  for (const child of para.children) {
    if (child.type !== "text") continue;
    taskTokenPattern.lastIndex = 0;
    if (taskTokenPattern.test(child.value)) {
      hasTokens = true;
      break;
    }
  }
  return { state, custom, hasTokens, prefixLen };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function upgradeTaskItem(item: any, p: TaskItemParse) {
  const para = item.children[0];
  if (p.custom) {
    para.children[0].value = para.children[0].value.slice(p.prefixLen);
  }
  item.checked = null; // drop the GFM checkbox; the state cell takes its place

  // Scheduled and due move into their own (sortable) table columns; priority, cookies, and the
  // completion stamp stay in the task cell as chips.
  let sched = "";
  let due = "";
  for (let i = 0; i < para.children.length; i++) {
    const child = para.children[i];
    if (child.type !== "text") continue;
    taskTokenPattern.lastIndex = 0;
    if (!taskTokenPattern.test(child.value)) continue;
    taskTokenPattern.lastIndex = 0;
    const parts: unknown[] = [];
    let last = 0;
    let match: RegExpExecArray | null;
    while ((match = taskTokenPattern.exec(child.value)) !== null) {
      if (match.index > last) {
        parts.push({ type: "text", value: child.value.slice(last, match.index) });
      }
      if (match[2] === "sched") {
        sched = match[3];
      } else if (match[2] === "due") {
        due = match[3];
      } else {
        const kind = match[1] ? "priority" : (match[2] ?? "cookie");
        const value = match[1] ?? match[3] ?? match[4];
        parts.push({ type: "taskchip", data: { hName: "taskchip", hProperties: { kind, value } }, children: [] });
      }
      last = taskTokenPattern.lastIndex;
    }
    if (last < child.value.length) {
      parts.push({ type: "text", value: child.value.slice(last) });
    }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    para.children.splice(i, 1, ...(parts as any[]));
    i += parts.length - 1;
  }

  // The item's source line resolves the row to the engine-parsed task. Rendered bodies are
  // line-aligned with the note file — BlankFieldLines blanks rather than removes, and the include
  // splice swaps lines 1:1 — the same invariant the includes feature already relies on.
  const line = item.position?.start?.line ?? 0;

  const data = (item.data ??= {});
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (data as any).hName = "taskrow";
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (data as any).hProperties = { line, state: p.state.name, done: p.state.done, sched, due };
}

export function remarkTaskLine(options: { states: TaskState[] }) {
  const byChar = new Map(options.states.map((s) => [s.char, s]));
  return (tree: MdastRoot) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    visit(tree, "list", (list: any) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const parsed = list.children.map((item: any) => parseTaskItem(item, byChar));
      if (!parsed.some((p: TaskItemParse | null) => p && (p.custom || p.hasTokens))) {
        return; // plain GFM checklist (or no tasks at all): leave it alone
      }
      if (parsed.some((p: TaskItemParse | null) => !p)) {
        return; // a plain bullet mixed into the block: an <li> cannot live in a <table>, stay plain
      }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      list.children.forEach((item: any, i: number) => {
        upgradeTaskItem(item, parsed[i] as TaskItemParse);
      });
      const data = (list.data ??= {});
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (data as any).hName = "tasktable";
    });
  };
}

// remarkAlert turns a GitHub-style callout blockquote — one whose first line is `[!NOTE]` (or TIP,
// IMPORTANT, WARNING, CAUTION) — into a titled admonition: the marker is stripped and the blockquote
// is rendered as `<div class="md-alert md-alert-<type>">` with a title paragraph prepended. A
// blockquote without the marker is left untouched, so ordinary quotes stay blockquotes.
const alertTitles: Record<string, string> = {
  NOTE: "Note",
  TIP: "Tip",
  IMPORTANT: "Important",
  WARNING: "Warning",
  CAUTION: "Caution",
};
const alertPattern = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/i;

export function remarkAlert() {
  return (tree: MdastRoot) => {
    visit(tree, "blockquote", (node) => {
      const first = node.children[0];
      if (!first || first.type !== "paragraph") return;
      const marker = first.children[0];
      if (!marker || marker.type !== "text") return;
      const match = alertPattern.exec(marker.value);
      if (!match) return;
      const type = match[1].toUpperCase();
      // Drop the marker (and the newline/space before the body it consumed); if that empties the
      // paragraph — marker alone on its line — remove it so only the body and title remain.
      marker.value = marker.value.slice(match[0].length);
      if (marker.value === "" && first.children.length === 1) {
        node.children.shift();
      }
      const data = (node.data ??= {});
      data.hName = "div";
      data.hProperties = { className: ["md-alert", `md-alert-${type.toLowerCase()}`] };
      node.children.unshift({
        type: "paragraph",
        data: { hProperties: { className: ["md-alert-title"] } },
        children: [{ type: "text", value: alertTitles[type] }],
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- injected title node
      } as any);
    });
  };
}

// remarkEmbedOptions reads a trailing Org-style ":key value" tail after a standalone image embed — the
// same option shape includes and babel use (e.g. `![x](y) :height 360`). Only `:height` is defined: a
// bare number is px, and `%`/`vh` are treated as viewport height (an iframe in normal flow has no
// percentage-height basis). The parsed height is attached to the image via hProperties for the Embed
// component to apply, and the option tail is stripped so the paragraph stays a sole-image block embed.
const embedHeightPattern = /^:height\s+(\d+)(px|vh|%)?$/i;

function normalizeEmbedHeight(value: string, unit: string): string | null {
  const n = Number(value);
  if (!Number.isFinite(n)) return null;
  if (unit === "%" || unit === "vh") {
    return `${Math.min(100, Math.max(10, n))}vh`;
  }
  return `${Math.min(4000, Math.max(80, n))}px`;
}

export function remarkEmbedOptions() {
  return (tree: MdastRoot) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- mdast image/text access
    visit(tree, "paragraph", (node: any) => {
      const kids = node.children;
      // A standalone embed with options parses as [image, text(" :height 360")].
      if (kids.length !== 2) return;
      const [img, tail] = kids;
      if (img.type !== "image" || tail.type !== "text") return;
      const m = embedHeightPattern.exec(tail.value.trim());
      if (!m) return; // an unrecognized ":..." tail is left as visible text rather than silently dropped
      const height = normalizeEmbedHeight(m[1], (m[2] ?? "").toLowerCase());
      if (!height) return;
      const data = (img.data ??= {});
      const props = (data.hProperties ??= {});
      props.embedHeight = height;
      node.children = [img]; // drop the tail so the paragraph is a sole image again
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
