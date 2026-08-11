import type { Element, Root as HastRoot, Text as HastText } from "hast";
import type { Paragraph, Root as MdastRoot } from "mdast";
import { visit } from "unist-util-visit";
import { taskStates } from "../../taskStates";
import type { TaskState } from "../../types";
import { headingElementID, headingSlug } from "./toc";

// The [[target|display]] wiki-link grammar (target, optional |display alias). Shared with the portable
// export so both flatten the same construct. It carries the /g flag; reset lastIndex before manual exec.
export const wikiPattern = /\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g;

// Block anchors: a trailing " ^id" marks a paragraph or list item as a link target the engine
// resolves for [[Note#^id]] links and ![[Note#^id]] transclusions. The id grammar mirrors the
// engine's (a letter/digit then letters, digits, "-", "_").
const blockIDPattern = /^\^([A-Za-z0-9][A-Za-z0-9_-]*)$/;
const trailingMarkerPattern = /(?:^|[ \t])\^([A-Za-z0-9][A-Za-z0-9_-]*)\s*$/;

// blockElementID is the DOM id a marked block renders with, shared by remarkBlockID (which sets it)
// and WikiLink (which navigates to it via the URL hash).
export function blockElementID(blockID: string): string {
  return `block-${blockID}`;
}

// splitWikiTarget separates a wiki-link target into its resolution key and an optional block anchor,
// mirroring the engine's anchor parsing: "Note#^id" resolves by "Note" and navigates to the block,
// "Note#Heading" also resolves by "Note" (navigation lands at the note), and a "#" with nothing
// after it stays part of the key (e.g. "C#").
export function splitWikiTarget(target: string): { key: string; blockID: string; headingID: string } {
  const i = target.indexOf("#");
  if (i < 0) return { key: target, blockID: "", headingID: "" };
  const rest = target.slice(i + 1).trim();
  const block = blockIDPattern.exec(rest);
  if (block) return { key: target.slice(0, i).trim(), blockID: block[1], headingID: "" };
  if (rest.replace(/^#+/, "").trim() === "") return { key: target, blockID: "", headingID: "" };
  // A heading anchor resolves by the note key like any other link, and navigates to the heading's
  // own id — the same id the note's Contents outline links to (see toc.ts). Extra leading "#"s are
  // the level marker ("Note##Deeper"), which the level-agnostic slug ignores.
  // ponytail: like the engine's anchor resolution, the first heading with that text wins; a note
  // holding both "# X" and "## X" lands on the first. Match the level if that ever matters.
  return {
    key: target.slice(0, i).trim(),
    blockID: "",
    headingID: headingSlug(rest.replace(/^#+/, "").trim()),
  };
}

// remarkBlockID strips a trailing "^id" block marker from a paragraph or list item and gives the
// block a DOM id, so [[Note#^id]] hash navigation can scroll to and highlight it. The marker on a
// list item's own line attaches to the <li> (a tight list unwraps its paragraph, which would drop
// the id); a plain paragraph carries the id itself.
export function remarkBlockID() {
  return (tree: MdastRoot) => {
    visit(tree, "paragraph", (node, index, parent) => {
      if (!parent) return;
      const id = takeTrailingBlockID(node);
      if (!id) return;
      const owner = parent.type === "listItem" ? parent : node;
      const data = (owner.data ??= {});
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- hProperties is untyped mdast data
      const props = ((data as any).hProperties ??= {});
      props.id = blockElementID(id);
    });
  };
}

// takeTrailingBlockID removes a trailing block marker from a paragraph's last text node and returns
// its id, or null when the paragraph does not end with one. A marker alone in a paragraph (no other
// content) is left as prose, matching the engine: a marker needs content on its line.
function takeTrailingBlockID(node: Paragraph): string | null {
  const last = node.children[node.children.length - 1];
  if (!last || last.type !== "text") return null;
  const match = trailingMarkerPattern.exec(last.value);
  if (!match) return null;
  const stripped = last.value.slice(0, match.index).replace(/[ \t]+$/, "");
  if (stripped === "" && node.children.length === 1) return null;
  last.value = stripped;
  return match[1];
}

// remarkHeadingID gives each rendered heading the id its outline entry links to. The ids are
// computed from the note's source by tocEntries and handed in, so the outline and the headings can
// never disagree about which id belongs to which heading — matching them here by text would have to
// re-derive the dedupe counter and could drift.
//
// Setext headings ("Title" over "====") are skipped: remark parses them, but track's heading
// parsers are ATX-only, so counting one would shift every id after it onto the wrong heading.
export function remarkHeadingID(ids: string[]) {
  return (tree: MdastRoot) => {
    let i = 0;
    visit(tree, "heading", (node) => {
      const start = node.position?.start?.line;
      const end = node.position?.end?.line;
      if (start !== undefined && end !== undefined && start !== end) return; // setext
      const id = ids[i++];
      if (!id) return;
      const data = (node.data ??= {});
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- hProperties is untyped mdast data
      const props = ((data as any).hProperties ??= {});
      props.id = headingElementID(id);
    });
  };
}

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
      // One line in, one line out: every line below an include keeps its file line number, which
      // is what resolves a rendered task row back to the task the engine parsed (see the task
      // components in MarkdownView). Blank padding around the token — the old way to make it its
      // own block mid-paragraph — shifted everything below by two lines, so a row could resolve
      // to a different task and write to it. An ATX heading is the one leaf block that is exactly
      // one line, interrupts a paragraph, and does not absorb the next line.
      lines[line] = `###### ${includeToken}${i}%%`;
    }
  });
  return lines.join("\n");
}

export function remarkInclude() {
  return (tree: MdastRoot) => {
    visit(tree, "heading", (node, index, parent) => {
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

// remarkTaskLine upgrades checklists that use task notation (the "Tasks" help note) into rich task
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
    // The text after the marker may be empty ("- [/]" alone), which the engine still counts as a
    // task; requiring text here would make one such line drop the whole list back to checkboxes.
    const m = /^\[(.)\](?:[ \t]+|$)/.exec(first.value);
    if (!m) return null;
    state = byChar.get(m[1]);
    if (!state) return null;
    custom = true;
    prefixLen = m[0].length;
  }
  // A [done:] stamp does not count as authored notation: the engine writes it when a box is
  // ticked, so counting it would rebuild a plain checklist as the four-column table the moment
  // someone checks something.
  let hasTokens = false;
  for (const child of para.children) {
    if (child.type !== "text") continue;
    taskTokenPattern.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = taskTokenPattern.exec(child.value)) !== null) {
      if (match[2] !== "done") {
        hasTokens = true;
        break;
      }
    }
    if (hasTokens) break;
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

  // The item's source line resolves the row to the engine-parsed task, so the rendered body has to
  // stay line-aligned with the note file: WebBody rewrites lines in place and the include splice is
  // 1:1 (see spliceIncludeTokens). A fence that expands into several lines — a dashboard or
  // track-query block — still shifts everything below it.
  // ponytail: known ceiling; the fix is a server-emitted line map, worth it only if someone puts a
  // task list under an expanding fence and notices.
  const line = item.position?.start?.line ?? 0;

  const data = (item.data ??= {});
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (data as any).hName = "taskrow";
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (data as any).hProperties = { line, state: p.state.name, done: p.state.done, sched, due };
}

// collectTaskItems walks a list and every list nested inside it, in document order, pairing each
// item with its nesting depth. An indented sub-list is its own mdast list node, so deciding per
// list node would split one checklist the reader sees as a whole: notation on a child would upgrade
// the children and leave their parents as bare checkboxes.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function collectTaskItems(list: any, byChar: Map<string, TaskState>, depth = 0): { item: any; parse: TaskItemParse | null; depth: number }[] {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const out: { item: any; parse: TaskItemParse | null; depth: number }[] = [];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  for (const item of list.children ?? []) {
    out.push({ item, parse: parseTaskItem(item, byChar), depth });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    for (const child of item.children ?? []) {
      if (child.type === "list") {
        out.push(...collectTaskItems(child, byChar, depth + 1));
      }
    }
  }
  return out;
}

export function remarkTaskLine() {
  const byChar = new Map(taskStates.map((s) => [s.char, s]));
  return (tree: MdastRoot) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    visit(tree, "list", (list: any, index, parent: any) => {
      // Only the outermost list decides: a nested one is part of its parent's checklist and is
      // handled with it (and skipped here, since visiting it again would flatten it twice).
      if (parent?.type === "listItem") return;
      const entries = collectTaskItems(list, byChar);
      if (!entries.some((e) => e.parse && (e.parse.custom || e.parse.hasTokens))) {
        return; // plain GFM checklist (or no tasks at all): leave it alone
      }
      if (entries.some((e) => !e.parse)) {
        return; // a plain bullet mixed into the block: an <li> cannot live in a <table>, stay plain
      }
      // The table is flat, so the nesting the source expressed with indentation is carried as a
      // depth property and drawn as an indent (see TaskRow). The rows keep document order, which is
      // the order the reader wrote them in — parent, then its children.
      entries.forEach(({ item, parse, depth }) => {
        upgradeTaskItem(item, parse as TaskItemParse);
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        ((item.data as any).hProperties as Record<string, unknown>).depth = depth;
      });
      list.children = entries.map((e) => e.item);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      entries.forEach(({ item }: any) => {
        item.children = (item.children ?? []).filter((child: { type: string }) => child.type !== "list");
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
// same option shape includes and babel use. The parsed values are attached to the image via hProperties
// for the Embed component to apply, and the option tail is stripped so the paragraph stays a sole-image
// block embed.
const embedOptionPattern = /:([a-z-]+)\s+([^\s:]+)/gi;
const embedHeightPattern = /^(\d+)(px|vh|%)?$/i;

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
      const value = tail.value.trim();
      let cursor = 0;
      let height: string | undefined;
      let frame: "none" | undefined;
      let match: RegExpExecArray | null;
      let found = false;
      // A malformed tail can return before exec reaches null, so do not carry its cursor into the
      // next paragraph visited by this plugin.
      embedOptionPattern.lastIndex = 0;
      while ((match = embedOptionPattern.exec(value)) !== null) {
        found = true;
        if (value.slice(cursor, match.index).trim() !== "") return;
        cursor = embedOptionPattern.lastIndex;
        const key = match[1].toLowerCase();
        if (key === "height") {
          const heightMatch = embedHeightPattern.exec(match[2]);
          if (!heightMatch) return;
          height = normalizeEmbedHeight(heightMatch[1], (heightMatch[2] ?? "").toLowerCase()) ?? undefined;
          if (!height) return;
        } else if (key === "frame" && match[2].toLowerCase() === "none") {
          frame = "none";
        } else {
          // An unrecognized or malformed option is left as visible text rather than silently dropped.
          return;
        }
      }
      if (!found || value.slice(cursor).trim() !== "" || (!height && !frame)) return;
      const data = (img.data ??= {});
      const props = (data.hProperties ??= {});
      if (height) props.embedHeight = height;
      if (frame) props.embedFrame = frame;
      node.children = [img]; // drop the tail so the paragraph is a sole image again
    });
  };
}

// remarkBlockEmbed lifts every image out of its paragraph, splitting the paragraph around it. An
// ![...]() is always a block embed in track (a card, a player, a framed image — never an inline
// glyph), so one written next to text on the same line — "foo\n![x](url)\nbar", with no blank line
// around it — parses as one paragraph and renders as a block box inside a <p>: the text around it
// falls into anonymous blocks that can carry no margin (so the embed ends up flush against the line
// below it), the embed is capped at the prose measure instead of the column, and the prerendered
// static HTML nests a <div> inside a <p>, which the parser then reshuffles. Hoisting it makes the
// embed a sibling, so it renders exactly like the blank-line-separated form.
//
// Runs after remarkEmbedOptions, which needs the sole-image paragraph its `:height` tail sits in.
export function remarkBlockEmbed() {
  return (tree: MdastRoot) => {
    visit(tree, "paragraph", (node, index, parent) => {
      if (!parent || index === undefined) return;
      if (!node.children.some((child) => child.type === "image")) return;
      const parts: Paragraph[] = [];
      let run: Paragraph["children"] = [];
      const flush = () => {
        // Whitespace-only runs are the line breaks between stacked images; they would render as
        // empty paragraphs carrying the paragraph lead.
        if (run.some((child) => child.type !== "text" || child.value.trim() !== "")) {
          parts.push({ type: "paragraph", children: run });
        }
        run = [];
      };
      for (const child of node.children) {
        if (child.type !== "image") {
          run.push(child);
          continue;
        }
        flush();
        parts.push({ type: "paragraph", children: [child] });
      }
      flush();
      // A sole image is already a block: markdownComponents unwraps that paragraph on its own.
      if (parts.length < 2) return;
      // A block marker is trailing text, so its id belongs to the last part (see remarkBlockID).
      if (node.data) parts[parts.length - 1].data = node.data;
      parent.children.splice(index, 1, ...parts);
      return index + parts.length;
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

// rehypeTaskCheck stamps a GFM checklist item's source line onto its checkbox, so a plain
// "- [ ] foo" list — one with no task notation, left as native checkboxes by remarkTaskLine — can
// still be ticked in the workspace. mdast-util-to-hast synthesizes the <input> without a position
// of its own, so the line comes from the enclosing <li>. The markup is otherwise untouched: the
// task-list-item classes the stylesheet keys on stay exactly as GFM emitted them.
export function rehypeTaskCheck() {
  return (tree: HastRoot) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    visit(tree, "element", (node: any) => {
      if (node.tagName !== "li") return;
      const line = node.position?.start?.line;
      if (!line) return;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const box = (node.children ?? []).find(
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (c: any) => c.type === "element" && c.tagName === "input" && c.properties?.type === "checkbox",
      );
      if (box) box.properties.dataTaskLine = line;
    });
  };
}

// rehypeCopyLine puts source spans on rendered top-level blocks. Marking inline nodes would make a
// long paragraph more precise, but would turn every link, emphasis run, and word-break wrapper into
// selection bookkeeping; the block span is the quieter tradeoff for this action.
export function rehypeCopyLine() {
  return (tree: HastRoot) => {
    for (const node of tree.children) {
      if (node.type !== "element") continue;
      const start = node.position?.start?.line;
      const end = node.position?.end?.line;
      if (start === undefined || end === undefined) continue;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const properties = (node.properties ??= {}) as any;
      properties.dataCopyLineStart = start;
      properties.dataCopyLineEnd = end;
    }
  };
}
