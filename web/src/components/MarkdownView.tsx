import type { Element } from "hast";
import { type ReactNode, useContext, useEffect, useState } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { NoteInclude } from "../types";
import { rehypeBudoux } from "./markdown/budouxEager";
import { CodeBlock } from "./markdown/CodeBlock";
import { IncludesContext, MarkdownSourceContext, NoteKindContext } from "./markdown/context";
import { Embed } from "./markdown/Embed";
import { ExternalLink } from "./markdown/ExternalLink";
import { GraphvizDiagram } from "./markdown/GraphvizDiagram";
import { loadMathPlugins, looksLikeMath, type MathPlugins, mathPluginsIfLoaded } from "./markdown/math";
import { MermaidDiagram } from "./markdown/MermaidDiagram";
import { MindmapDiagram } from "./markdown/MindmapDiagram";
import {
  remarkAlert,
  remarkBlockID,
  remarkEmbedOptions,
  remarkInclude,
  remarkWikiLink,
  spliceIncludeTokens,
} from "./markdown/plugins";
import { EChartsFence } from "./markdown/EChartsBlock";
import { ViewSpecChart } from "./markdown/ViewSpecChart";
import { WikiLink } from "./preview/WikiLink";

interface MarkdownViewProps {
  markdown: string;
  kind?: string;
  // Resolved ![[...]] includes for this body (ADR 0031), from /api/render live or the static
  // bundle. Absent or empty, include lines render as ordinary text (their [[...]] stays a link).
  includes?: NoteInclude[];
}

// The markdown is parsed by react-markdown (CommonMark + GFM tables/strikethrough/task lists, plus
// $...$/$$...$$ math via remark-math + rehype-katex). The body arrives already sanitized by the server's
// /api/render (action links flattened); the track-specific construct is [[...]] wiki links (remarkWikiLink).
// KaTeX is loaded lazily (see ./markdown/math), so a note without math never pulls in its bundle; while a
// math note's first render waits for that chunk, the "$…$" briefly shows as source, then typesets.
export function MarkdownView({ markdown, kind = "note", includes }: MarkdownViewProps) {
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
  ];
  // BudouX (Japanese word-break) is gated behind __TRACK_STATIC__, a build-time literal, so the static
  // help site tree-shakes its ~190KB model away (English content is never segmented) while the live
  // workspace keeps it eager.
  const rehypePlugins = [...(math ? [math.rehype] : []), ...(__TRACK_STATIC__ ? [] : [rehypeBudoux])];

  return (
    <NoteKindContext.Provider value={kind}>
      <IncludesContext.Provider value={includes ?? []}>
        <MarkdownSourceContext.Provider value={markdown}>
          <div className="markdown-view">
            <Markdown remarkPlugins={remarkPlugins} rehypePlugins={rehypePlugins} components={markdownComponents}>
              {source}
            </Markdown>
          </div>
        </MarkdownSourceContext.Provider>
      </IncludesContext.Provider>
    </NoteKindContext.Provider>
  );
}

// IncludeEmbed renders one resolved ![[...]] include as an embed card: a caption header linking to
// the source note, and the extracted lines rendered as markdown. It lives here (not its own module)
// because it renders through MarkdownView recursively — the nested render gets no includes, so an
// include inside embedded content shows as text, matching the spec's no-recursion rule.
function IncludeEmbed({ include }: { include: NoteInclude }) {
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
      <MarkdownView markdown={include.lines.join("\n")} kind={include.kind ?? "note"} />
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
      if (normalized === "mindmap") {
        return <MindmapDiagram text={text} />;
      }
      if (normalized === "viewspec") {
        return <ViewSpecChart text={text} />;
      }
      if (normalized === "echarts") {
        return <EChartsFence text={text} />;
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
