import type { Element } from "hast";
import { type ReactNode } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { CodeBlock } from "./markdown/CodeBlock";
import { NoteKindContext } from "./markdown/context";
import { Embed } from "./markdown/Embed";
import { ExternalLink } from "./markdown/ExternalLink";
import { MermaidDiagram } from "./markdown/MermaidDiagram";
import { rehypeBudoux, remarkWikiLink } from "./markdown/plugins";
import { WikiLink } from "./preview/WikiLink";

interface MarkdownViewProps {
  markdown: string;
  kind?: string;
}

// The markdown is parsed by react-markdown (CommonMark + GFM tables/strikethrough/task lists). The body
// arrives already sanitized by the server's /api/render (action links flattened), so the only
// track-specific construct the frontend still parses is [[...]] wiki links, handled by remarkWikiLink.
export function MarkdownView({ markdown, kind = "note" }: MarkdownViewProps) {
  if (markdown.trim() === "") {
    return <p className="muted">Empty note.</p>;
  }

  return (
    <NoteKindContext.Provider value={kind}>
      <div className="markdown-view">
        <Markdown
          remarkPlugins={[remarkGfm, remarkWikiLink]}
          rehypePlugins={[rehypeBudoux]}
          components={markdownComponents}
        >
          {markdown}
        </Markdown>
      </div>
    </NoteKindContext.Provider>
  );
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
  a: ({ href, children }: { href?: string; children?: ReactNode }) => (
    <ExternalLink href={href ?? ""}>{children}</ExternalLink>
  ),
  img: ({ src, alt }: { src?: string; alt?: string }) => (
    <Embed src={typeof src === "string" ? src : ""} alt={alt ?? ""} />
  ),
  // A standalone image is a block embed (player/PDF/OGP card), so unwrap the paragraph that would
  // otherwise nest a block element inside a <p>.
  p: ({ node, children }: ElementProps) => (isSoleImage(node) ? <>{children}</> : <p>{children}</p>),
  pre: ({ node, children }: ElementProps) => {
    const code = node?.children?.[0];
    if (code && code.type === "element" && code.tagName === "code") {
      const lang = codeLanguage(code);
      const text = hastText(code);
      if (normalizeCodeLanguage(lang) === "mermaid") {
        return <MermaidDiagram text={text} />;
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
