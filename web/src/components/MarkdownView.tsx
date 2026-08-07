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
import type { NoteInclude, NoteID } from "../types";
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
import { TaskCheck, TaskRow, TaskTable } from "./markdown/TaskControls";
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
import { EChartsFence } from "./markdown/EChartsBlock";
import { QueryView } from "./markdown/QueryView";
import { ViewSpecChart } from "./markdown/ViewSpecChart";
import { WikiLink } from "./preview/WikiLink";
import { TitleCopyButton } from "./TitleCopyButton";
import { useNoteQuery } from "../queries";
import { STATIC_MODE } from "../runtime";

interface MarkdownViewProps {
  markdown: string;
  // The canonical note title is the document heading in full-page readers. A matching leading body
  // h1 is blanked (not deleted, so include/task source line numbers stay stable).
  title?: string;
  // The note's ID — when set, the title gets a copy button.
  noteId?: NoteID;
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
export function MarkdownView({ markdown, title, noteId, kind = "note", vault = "", includes }: MarkdownViewProps) {
  const bodyMarkdown = useMemo(() => withoutDuplicateTitle(markdown, title), [markdown, title]);
  const hasMath = looksLikeMath(bodyMarkdown);
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
  const headingIDs = useMemo(() => tocEntries(bodyMarkdown).map((entry) => entry.id), [bodyMarkdown]);

  if (bodyMarkdown.trim() === "" && !title) {
    return <p className="muted">Empty note.</p>;
  }

  const hasIncludes = includes !== undefined && includes.length > 0;
  const source = hasIncludes
    ? spliceIncludeTokens(
        bodyMarkdown,
        includes.map((inc) => inc.line),
      )
    : bodyMarkdown;
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
        <MarkdownSourceContext.Provider value={bodyMarkdown}>
          <div className="markdown-view">
            {title ? (
              <h1 className="note-title">
                {title}
                {noteId ? <TitleCopyButton title={title} /> : null}
              </h1>
            ) : null}
            {bodyMarkdown.trim() === "" ? <p className="muted">Empty note.</p> : null}
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

// withoutDuplicateTitle removes only the first visible block when it is an h1 whose rendered text
// exactly matches the sidecar title. Replacing source lines with blanks preserves every later line
// number, which includes and interactive task controls use to address the original note.
export function withoutDuplicateTitle(markdown: string, title?: string): string {
  const wanted = title?.trim();
  if (!wanted) return markdown;
  const lines = markdown.split("\n");
  let first = 0;
  while (first < lines.length && lines[first].trim() === "") first++;
  const atx = /^#\s+(.+?)\s*#*\s*$/.exec(lines[first] ?? "");
  if (atx && plainHeadingText(atx[1]) === wanted) {
    lines[first] = "";
    return lines.join("\n");
  }
  if (/^\s{0,3}=+\s*$/.test(lines[first + 1] ?? "") && plainHeadingText(lines[first] ?? "") === wanted) {
    lines[first] = "";
    lines[first + 1] = "";
    return lines.join("\n");
  }
  return markdown;
}

function plainHeadingText(text: string): string {
  return text
    .replace(/\[\[([^|\]]+)(?:\|([^\]]+))?\]\]/g, (_, target: string, alias?: string) => alias ?? target)
    .replace(/\[([^\]]+)\]\([^\s)]*\)/g, "$1")
    .replace(/[*_~`]/g, "")
    .trim();
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
  const sourceMatchesExcerpt = source.data?.note.etag === include.etag;
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
        value={{
          noteID: sourceID,
          tasksRef: {
            current: {
              tasks: sourceMatchesExcerpt ? source.data?.note.tasks ?? { items: [] } : { items: [] },
              etag: sourceMatchesExcerpt ? include.etag ?? "" : "",
            },
          },
          lineOffset: offset,
        }}
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
