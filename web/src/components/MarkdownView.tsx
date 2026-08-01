import type { Element } from "hast";
import {
  type InputHTMLAttributes,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { NoteID, NoteInclude, TaskItem } from "../types";
import { qualify } from "../vaultId";
import { rehypeBudoux } from "./markdown/budouxEager";
import { CodeBlock } from "./markdown/CodeBlock";
import {
  IncludesContext,
  MarkdownSourceContext,
  NoteKindContext,
  NoteVaultContext,
  TaskBoardContext,
} from "./markdown/context";
import { TaskBoard } from "./markdown/TaskBoard";
import { Embed } from "./markdown/Embed";
import { ExternalLink } from "./markdown/ExternalLink";
import { D2Diagram } from "./markdown/D2Diagram";
import { DrawioDiagram } from "./markdown/DrawioDiagram";
import { GraphvizDiagram } from "./markdown/GraphvizDiagram";
import { loadMathPlugins, looksLikeMath, type MathPlugins, mathPluginsIfLoaded } from "./markdown/math";
import { MermaidDiagram } from "./markdown/MermaidDiagram";
import { MindmapDiagram } from "./markdown/MindmapDiagram";
import {
  remarkAlert,
  remarkBlockID,
  remarkEmbedOptions,
  remarkInclude,
  remarkHeadingID,
  remarkTaskLine,
  rehypeTaskCheck,
  remarkWikiLink,
  spliceIncludeTokens,
} from "./markdown/plugins";
import { tocEntries } from "./markdown/toc";
import { taskStates } from "../taskStates";
import { EChartsFence } from "./markdown/EChartsBlock";
import { QueryView } from "./markdown/QueryView";
import { ViewSpecChart } from "./markdown/ViewSpecChart";
import { WikiLink } from "./preview/WikiLink";
import { useNoteQuery, useSetTaskDateMutation, useSetTaskStateMutation } from "../queries";
import { STATIC_MODE } from "../runtime";

interface MarkdownViewProps {
  markdown: string;
  kind?: string;
  // Vault of the note this body belongs to (registry name; "" for the launch vault). Everything the
  // body refers to lives in that vault, so it is what attachments, links, includes, and chart data
  // sources resolve against.
  vault?: string;
  // Resolved ![[...]] includes for this body (ADR 0031), from /api/render live or the static
  // bundle. Absent or empty, include lines render as ordinary text (their [[...]] stays a link).
  includes?: NoteInclude[];
}

// The markdown is parsed by react-markdown (CommonMark + GFM tables/strikethrough/task lists, plus
// $...$/$$...$$ math via remark-math + rehype-katex). The body arrives already sanitized by the server's
// /api/render (action links flattened); the track-specific construct is [[...]] wiki links (remarkWikiLink).
// KaTeX is loaded lazily (see ./markdown/math), so a note without math never pulls in its bundle; while a
// math note's first render waits for that chunk, the "$…$" briefly shows as source, then typesets.
export function MarkdownView({ markdown, kind = "note", vault = "", includes }: MarkdownViewProps) {
  const hasMath = looksLikeMath(markdown);
  const [math, setMath] = useState<MathPlugins | null>(() => (hasMath ? mathPluginsIfLoaded() : null));

  useEffect(() => {
    if (!hasMath || math) return;
    let cancelled = false;
    void loadMathPlugins().then((plugins) => {
      if (!cancelled) setMath(plugins);
    });
    return () => {
      cancelled = true;
    };
  }, [hasMath, math]);

  // Every hook runs before the empty-note return below: a preview mounts with "" and gets its body a
  // moment later, so a hook placed after the return would change the hook count between renders and
  // React would throw — which the router catches and shows as a bare "Something went wrong!".
  //
  // The ids come from the note's own source, not the spliced copy: splicing rewrites include lines
  // and the outline in the aside reads the same source, so both sides agree on which heading is which.
  const headingIDs = useMemo(() => tocEntries(markdown).map((entry) => entry.id), [markdown]);

  if (markdown.trim() === "") {
    return <p className="muted">Empty note.</p>;
  }

  const hasIncludes = includes !== undefined && includes.length > 0;
  const source = hasIncludes
    ? spliceIncludeTokens(
        markdown,
        includes.map((inc) => inc.line),
      )
    : markdown;
  const remarkPlugins = [
    remarkGfm,
    remarkAlert,
    remarkBlockID,
    remarkEmbedOptions,
    ...(math ? [math.remark] : []),
    remarkWikiLink,
    ...(hasIncludes ? [remarkInclude] : []),
    // After remarkInclude: the include splice writes each directive line as a one-line ATX heading
    // token, so running this first would spend a real heading's id on a token and shift every
    // heading below an include onto the wrong one. remarkInclude has replaced them by now.
    [remarkHeadingID, headingIDs] as [typeof remarkHeadingID, string[]],
    // After remarkWikiLink, so a [[link]] in task text is consumed before token extraction.
    remarkTaskLine,
  ];
  // BudouX (Japanese word-break) is gated behind __TRACK_STATIC__, a build-time literal, so the static
  // help site tree-shakes its ~190KB model away (English content is never segmented) while the live
  // workspace keeps it eager.
  const rehypePlugins = [
    ...(math ? [math.rehype] : []),
    // Stamps each checklist item's source line onto its native checkbox, so a plain "- [ ]" list
    // can be ticked (see TaskCheck). Cheap and static-safe: the static site just never wires it up.
    rehypeTaskCheck,
    ...(__TRACK_STATIC__ ? [] : [rehypeBudoux]),
  ];

  return (
    <NoteKindContext.Provider value={kind}>
      <NoteVaultContext.Provider value={vault}>
        <IncludesContext.Provider value={includes ?? []}>
        <MarkdownSourceContext.Provider value={markdown}>
          <div className="markdown-view">
            <Markdown remarkPlugins={remarkPlugins} rehypePlugins={rehypePlugins} components={markdownComponents}>
              {source}
            </Markdown>
          </div>
        </MarkdownSourceContext.Provider>
        </IncludesContext.Provider>
      </NoteVaultContext.Provider>
    </NoteKindContext.Provider>
  );
}

// IncludeEmbed renders one resolved ![[...]] include as an embed card: a caption header linking to
// the source note, and the extracted lines rendered as markdown. It lives here (not its own module)
// because it renders through MarkdownView recursively — the nested render gets no includes, so an
// include inside embedded content shows as text, matching the spec's no-recursion rule.
function IncludeEmbed({ include }: { include: NoteInclude }) {
  const vault = useContext(NoteVaultContext);
  // A task shown through an include belongs to the note it was written in, so the excerpt's task
  // context points there: the source note's id and tasks, plus the offset that turns a line of the
  // excerpt into a line of that file. Without a usable offset (several :lines ranges) or on the
  // published site the rows stay inert, as they were before.
  const offset = include.source_line ?? -1;
  const sourceID =
    !STATIC_MODE && include.note_id && offset >= 0 ? qualify(vault, include.note_id) : "";
  const source = useNoteQuery(sourceID, { enabled: sourceID !== "" });
  if (include.error) {
    return <div className="note-include note-include-error">⚠ {include.error}</div>;
  }
  return (
    <section className="note-include">
      <div className="note-include-header">
        {include.title ? (
          <WikiLink target={include.title} display={include.caption} />
        ) : (
          include.caption
        )}
      </div>
      {/* Never the host note's board: an excerpt's tasks are the source note's, addressed through
          its own line offset. */}
      <TaskBoardContext.Provider
        value={{ noteID: sourceID, tasks: source.data?.note.tasks, lineOffset: offset }}
      >
        <MarkdownView markdown={include.lines.join("\n")} kind={include.kind ?? "note"} vault={vault} />
      </TaskBoardContext.Provider>
      {(include.bad_options ?? []).map((bad) => (
        <div key={bad} className="note-include-warning">
          ⚠ unknown option: {bad}
        </div>
      ))}
    </section>
  );
}


// TaskRowState is the state cell of a task-table row, and doubles as the state control: in the
// live workspace it renders as a select stripped down to the badge's text look, writing through
// the same engine path as the board's cards. Its source line resolves the row to the engine-parsed
// task (rendered bodies are line-aligned with the note file — the invariant includes rely on); on
// static sites and hover previews (no note id) it stays a plain badge.
// useTaskAtLine resolves a rendered line back to the task the engine parsed. It yields nothing on
// the published static site, inside a preview that carries no note, or while the editor buffer is
// dirty (noteID is blanked then) — every surface where a write would either be refused or land on a
// line that no longer means what it did. Reading the context costs no query client, so a read-only
// render (a preview, a test) never needs one; the control below mounts the mutation only once there
// is something to write.
function useTaskAtLine(line: number) {
  const { noteID, tasks, lineOffset = 0 } = useContext(TaskBoardContext);
  const item =
    !STATIC_MODE && noteID !== "" && tasks && line > 0
      ? tasks.items.find((t) => t.line === line + lineOffset)
      : undefined;
  return { noteID, item };
}

// TaskCheck makes a plain GFM checklist ("- [ ] foo", no task notation) tickable: the engine has
// always parsed those lines as tasks, only the frontend left the native checkbox disabled. The line
// comes from rehypeTaskCheck; a box it cannot resolve stays exactly as it renders today.
function TaskCheck({ line, checked }: { line: number; checked: boolean }) {
  const { noteID, item } = useTaskAtLine(line);
  if (!item) {
    return <input type="checkbox" checked={checked} disabled readOnly />;
  }
  return <TaskCheckControl noteID={noteID} item={item} />;
}

function TaskCheckControl({ noteID, item }: { noteID: NoteID; item: TaskItem }) {
  const mutation = useSetTaskStateMutation(noteID);
  const target = taskStates.find((state) => state.done !== item.done);
  // While the write is in flight, show where it is going rather than snapping back.
  const shown = mutation.isPending ? !item.done : item.done;
  return (
    <input
      type="checkbox"
      checked={shown}
      disabled={mutation.isPending || !target}
      aria-label={`Toggle task: ${item.text}`}
      onChange={() => {
        if (target) mutation.mutate({ line: item.line, state: target.name, expect: item.state });
      }}
    />
  );
}

// TaskRowDate is the scheduled/due cell. Read-only it is the marked date as written; where the note
// can be written it is a native date input styled down to look like that same text, so picking a
// date is a click on what it shows rather than a separate editing mode. An empty cell shows nothing
// until the row is hovered or focused (the CSS reveals it), so an untouched table stays quiet.
function TaskRowDate({ field, value, line }: { field: "sched" | "due"; value: string; line: number }) {
  const { noteID, item } = useTaskAtLine(line);
  const marker = field === "sched" ? "▷" : "!";
  if (!item) {
    return <>{value ? `${marker} ${value}` : ""}</>;
  }
  return <TaskRowDateControl noteID={noteID} item={item} field={field} value={value} />;
}

function TaskRowDateControl({
  noteID,
  item,
  field,
  value,
}: {
  noteID: NoteID;
  item: TaskItem;
  field: "sched" | "due";
  value: string;
}) {
  const mutation = useSetTaskDateMutation(noteID);
  return (
    <input
      type="date"
      className="task-row-date-input"
      aria-label={field === "sched" ? "Scheduled date" : "Due date"}
      value={value}
      disabled={mutation.isPending}
      data-empty={value === "" || undefined}
      // The cell wears the note's own type and hides the browser's picker indicator, so a click would
      // otherwise land in the date segments. showPicker opens the calendar the indicator would have
      // opened, which keeps picking a date one click on what the cell already shows.
      //
      // The indicator we hide is a -webkit- pseudo, so Gecko still draws its own calendar button and
      // toggles the picker from a system-group click listener — a second dispatch pass, after this
      // handler. It would find the picker already open and close it. Cancelling the click makes that
      // listener stand down (it returns early on defaultPrevented) and costs nothing elsewhere: no
      // engine focuses a date segment on click, only on mousedown.
      onClick={(event) => {
        event.preventDefault();
        event.currentTarget.showPicker?.();
      }}
      onChange={(event) => mutation.mutate({ line: item.line, field, date: event.currentTarget.value })}
    />
  );
}

function TaskRowState({ name, done, line }: { name: string; done: boolean; line: number }) {
  const { noteID, item } = useTaskAtLine(line);
  const className = `task-row-state${done ? " task-row-state-done" : ""}`;
  if (!item) {
    return <span className={className}>{name}</span>;
  }
  return <TaskRowStateControl noteID={noteID} item={item} className={className} />;
}

function TaskRowStateControl({
  noteID,
  item,
  className,
}: {
  noteID: NoteID;
  item: TaskItem;
  className: string;
}) {
  const mutation = useSetTaskStateMutation(noteID);
  return (
    <select
      className={className}
      aria-label="Task state"
      value={item.state}
      disabled={mutation.isPending}
      onChange={(event) =>
        mutation.mutate({ line: item.line, state: event.currentTarget.value, expect: item.state })
      }
    >
      {taskStates.map((state) => (
        <option key={state.name} value={state.name}>
          {state.name}
        </option>
      ))}
    </select>
  );
}

type TaskRowProps = { line?: unknown; state?: unknown; done?: unknown; sched?: unknown; due?: unknown; depth?: unknown };

// TaskTable renders a notation-bearing checklist as one sortable table. Sorting is view-only (the
// note keeps its order); STATE sorts by the state-set order, the date columns sort empties last, and
// a third click on a header returns to the source order.
function TaskTable({ node, children }: ElementProps) {
  const [sort, setSort] = useState<{ key: "state" | "sched" | "due"; asc: boolean } | null>(null);
  const rowNodes = (node?.children ?? []).filter((c): c is Element => c.type === "element");
  const rowEls = (Array.isArray(children) ? children : [children]).filter((c) => typeof c !== "string");
  let order = rowNodes.map((_, i) => i);
  if (sort) {
    const { key, asc } = sort;
    const valueOf = (i: number): number | string => {
      const p = (rowNodes[i].properties ?? {}) as TaskRowProps;
      if (key === "state") {
        return taskStates.findIndex((s) => s.name === String(p.state ?? ""));
      }
      return String(p[key] ?? "");
    };
    order = [...order].sort((a, b) => {
      const va = valueOf(a);
      const vb = valueOf(b);
      const emptyA = va === "";
      const emptyB = vb === "";
      if (emptyA !== emptyB) {
        return emptyA ? 1 : -1; // rows without the date always sink to the bottom
      }
      const cmp = va < vb ? -1 : va > vb ? 1 : a - b;
      return asc ? cmp : -cmp;
    });
  }
  const header = (key: "state" | "sched" | "due", label: string) => (
    <th>
      <button
        type="button"
        className="task-table-sort"
        onClick={() =>
          setSort(sort?.key !== key ? { key, asc: true } : sort.asc ? { key, asc: false } : null)
        }
      >
        {label}
        {sort?.key === key ? (sort.asc ? " ▲" : " ▼") : ""}
      </button>
    </th>
  );
  return (
    <table className="task-table">
      <thead>
        <tr>
          {header("state", "STATE")}
          <th className="task-table-label">TASK</th>
          {header("sched", "SCHED")}
          {header("due", "DUE")}
        </tr>
      </thead>
      <tbody>{order.map((i) => rowEls[i])}</tbody>
    </table>
  );
}

// TaskRow is one table row: the state cell (select where editable), the task text with its chips,
// and the date columns.
function TaskRow({ node, children }: ElementProps) {
  const props = (node?.properties ?? {}) as TaskRowProps;
  const done = Boolean(props.done);
  const depth = Number(props.depth ?? 0);
  return (
    <tr className={`task-row${done ? " task-row-done" : ""}`}>
      <td className="task-row-state-cell">
        <TaskRowState name={String(props.state ?? "")} done={done} line={Number(props.line ?? 0)} />
      </td>
      {/* Nesting from the source is an indent, not a nested table: the rows are flat so the whole
          checklist stays one sortable table. */}
      <td className="task-row-text" style={depth > 0 ? { paddingLeft: `${depth * 16}px` } : undefined}>
        {children}
      </td>
      <td className="task-row-date">
        <TaskRowDate field="sched" value={String(props.sched ?? "")} line={Number(props.line ?? 0)} />
      </td>
      <td className="task-row-date task-row-due">
        <TaskRowDate field="due" value={String(props.due ?? "")} line={Number(props.line ?? 0)} />
      </td>
    </tr>
  );
}

// TrackInclude resolves the placeholder's index against the includes of the note being rendered.
function TrackInclude({ node }: ElementProps) {
  const includes = useContext(IncludesContext);
  const index = Number((node?.properties as { index?: unknown } | undefined)?.index);
  const include = includes[index];
  return include ? <IncludeEmbed include={include} /> : null;
}

// markdownComponents maps the rendered HTML elements to track's interactive presentation: links resolve
// to notes/assets/external pages, standalone images become rich embeds, fenced code gets the copy button
// and highlighter, and [[...]] wiki links (from remarkWikiLink) get hover previews. The object carries a
// custom "wikilink" element key, so it is cast to Components.
interface ElementProps {
  node?: Element;
  children?: ReactNode;
}

const markdownComponents = {
  a: ({ node, href, children }: ElementProps & { href?: string; children?: ReactNode }) => {
    // GFM footnote anchors (reference ↔ back-link) must keep their generated ids so the jumps land;
    // ExternalLink would drop them, and a footnote href is a pure in-page hash anyway.
    const props = (node?.properties ?? {}) as Record<string, unknown>;
    if (props.dataFootnoteRef !== undefined || props.dataFootnoteBackref !== undefined) {
      const isRef = props.dataFootnoteRef !== undefined;
      const label = typeof props.ariaLabel === "string" ? props.ariaLabel : undefined;
      return (
        <a
          id={typeof props.id === "string" ? props.id : undefined}
          href={href}
          className={isRef ? "footnote-ref" : "footnote-backref"}
          aria-label={label}
          title={label}
        >
          {children}
        </a>
      );
    }
    return <ExternalLink href={href ?? ""}>{children}</ExternalLink>;
  },
  img: ({ node, src, alt }: ElementProps & { src?: string; alt?: string }) => {
    const height = (node?.properties as { embedHeight?: unknown } | undefined)?.embedHeight;
    return (
      <Embed
        src={typeof src === "string" ? src : ""}
        alt={alt ?? ""}
        height={typeof height === "string" ? height : undefined}
      />
    );
  },
  // A standalone image is a block embed (player/PDF/OGP card), so unwrap the paragraph that would
  // otherwise nest a block element inside a <p>. The id (a ^block anchor, see remarkBlockID) is
  // forwarded so hash navigation still finds the paragraph.
  p: ({ node, children, id }: ElementProps & { id?: string }) =>
    isSoleImage(node) ? <>{children}</> : <p id={id}>{children}</p>,
  pre: ({ node, children }: ElementProps) => {
    const code = node?.children?.[0];
    if (code && code.type === "element" && code.tagName === "code") {
      const lang = codeLanguage(code);
      const text = hastText(code);
      const normalized = normalizeCodeLanguage(lang);
      if (normalized === "mermaid") {
        return <MermaidDiagram text={text} />;
      }
      if (normalized === "dot" || normalized === "graphviz") {
        return <GraphvizDiagram text={text} />;
      }
      if (normalized === "d2") {
        return <D2Diagram text={text} />;
      }
      if (normalized === "drawio") {
        return <DrawioDiagram text={text} />;
      }
      if (normalized === "mindmap") {
        return <MindmapDiagram text={text} />;
      }
      if (normalized === "viewspec") {
        return <ViewSpecChart text={text} />;
      }
      if (normalized === "taskboard") {
        return <TaskBoard />;
      }
      if (normalized === "echarts") {
        return <EChartsFence text={text} />;
      }
      if (normalized === "track-view") {
        return <QueryView text={text} />;
      }
      return <CodeBlock lang={lang} text={text} />;
    }
    return <pre>{children}</pre>;
  },
  code: ({ children }: { children?: ReactNode }) => <code className="inline-code">{children}</code>,
  wikilink: ({ node }: ElementProps) => {
    const props = (node?.properties ?? {}) as { target?: unknown; display?: unknown };
    return <WikiLink target={String(props.target ?? "")} display={String(props.display ?? "")} />;
  },
  tasktable: TaskTable,
  taskrow: TaskRow,
  // A checklist checkbox carrying a source line (rehypeTaskCheck) becomes tickable; every other
  // input renders as react-markdown produced it.
  input: ({ node, ...props }: ElementProps & InputHTMLAttributes<HTMLInputElement>) => {
    const line = (node?.properties as { dataTaskLine?: unknown } | undefined)?.dataTaskLine;
    if (typeof line === "number" && props.type === "checkbox") {
      return <TaskCheck line={line} checked={props.checked === true} />;
    }
    return <input {...props} />;
  },
  taskchip: ({ node }: ElementProps) => {
    const props = (node?.properties ?? {}) as { kind?: unknown; value?: unknown };
    const value = String(props.value ?? "");
    switch (String(props.kind ?? "")) {
      case "priority":
        return <span className="task-chip task-chip-priority">#{value}</span>;
      case "sched":
        return <span className="task-chip">▷ {value}</span>;
      case "due":
        return <span className="task-chip task-chip-due">! {value}</span>;
      case "done":
        return <span className="task-chip">✓ {value}</span>;
      default:
        return <span className="task-chip">{value}</span>;
    }
  },
  trackinclude: TrackInclude,
} as Components;

// hastText concatenates the text content of a hast element, dropping the single trailing newline that a
// fenced code block carries, so the code is shown exactly as written.
function hastText(node: Element): string {
  let out = "";
  for (const child of node.children) {
    if (child.type === "text") out += child.value;
    else if (child.type === "element") out += hastText(child);
  }
  return out.replace(/\n$/, "");
}

// codeLanguage reads the "language-xxx" class react-markdown puts on a fenced code element.
function codeLanguage(node: Element): string {
  const className = node.properties?.className;
  const classes = Array.isArray(className) ? className : className == null ? [] : [className];
  for (const c of classes) {
    const match = /^language-(.+)$/.exec(String(c));
    if (match) return match[1];
  }
  return "";
}

function normalizeCodeLanguage(lang: string): string {
  return lang.trim().toLowerCase();
}

// isSoleImage reports whether a paragraph node wraps nothing but a single image (ignoring whitespace).
function isSoleImage(node?: Element): boolean {
  if (!node) return false;
  const kids = node.children.filter((c) => !(c.type === "text" && c.value.trim() === ""));
  return kids.length === 1 && kids[0].type === "element" && kids[0].tagName === "img";
}
