import type { Element } from "hast";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Plugin } from "unified";
import { rehypeBudoux } from "./budouxEager";
import { CodeBlock } from "./CodeBlock";
import {
  externalMarkdownLink,
  type ExternalMarkdownImage,
  isExternalMarkdownImage,
  isRelativeMarkdownImage,
  remarkExternalHeadingIDs,
  scrollExternalMarkdownFragment,
} from "./externalMarkdown";
import { loadMathPlugins, looksLikeMath, type MathPlugins, mathPluginsIfLoaded } from "./math";
import { remarkAlert, remarkBreakHTML } from "./plugins";

export type { ExternalMarkdownImage } from "./externalMarkdown";
export { scrollExternalMarkdownFragment } from "./externalMarkdown";

export interface ExternalMarkdownOptions {
  // Receives the original relative href or same-document #fragment, only after an explicit click.
  // The caller resolves it against the selected files and owns router/hash history, never vault or
  // network resolution. Without a callback, fragments scroll locally and file links stay inert.
  onOpenLink?: (relativeHref: string) => void;
  // Called lazily for relative images only. The caller validates the selected file's size/signature
  // and returns an owned blob URL with its verified raster MIME. No SVG or network URLs render.
  resolveImage?: (
    relativeSrc: string,
  ) => ExternalMarkdownImage | undefined | Promise<ExternalMarkdownImage | undefined>;
}

interface ExternalMarkdownViewProps {
  markdown: string;
  title?: string;
  showTitle?: boolean;
  options?: ExternalMarkdownOptions;
}

// An explicit allowlist of presentation plugins/components is the trust boundary. Do not spread
// vault markdownComponents or mount vault contexts here: even a read-only query leaks local data.
export function ExternalMarkdownView({ markdown, title, showTitle = true, options }: ExternalMarkdownViewProps) {
  const root = useRef<HTMLDivElement>(null);
  const hasMath = looksLikeMath(markdown);
  const [math, setMath] = useState<MathPlugins | null>(() => hasMath ? mathPluginsIfLoaded() : null);
  const onOpenLink = options?.onOpenLink;
  const resolveImage = options?.resolveImage;

  useEffect(() => {
    if (!hasMath || math) return;
    let cancelled = false;
    void loadMathPlugins().then((plugins) => {
      if (!cancelled) setMath(plugins);
    }).catch(() => { /* Leave the original math readable if its optional bundle cannot load. */ });
    return () => { cancelled = true; };
  }, [hasMath, math]);

  const components = useMemo<Components>(() => ({
    a: ({ node, href = "", children }) => {
      const link = externalMarkdownLink(href);
      if (!link || (link.kind === "file" && !onOpenLink)) return <span>{children}</span>;
      const properties = node?.properties ?? {};
      const footnote = properties.dataFootnoteRef !== undefined || properties.dataFootnoteBackref !== undefined;
      const id = footnote && typeof properties.id === "string" ? properties.id : undefined;
      const label = footnote && typeof properties.ariaLabel === "string" ? properties.ariaLabel : undefined;
      // Ordinary web links never mount ExternalLink's note lookup, popup, or OGP machinery.
      if (link.kind === "web") {
        return <a className="md-link" href={href} target="_blank" rel="noreferrer noopener">{children}</a>;
      }
      return (
        <a
          className="md-link"
          id={id}
          aria-label={label}
          // A local filename must not become a browser request on a middle click/context menu.
          // The fragment fallback stays on this page; all ordinary activation is handled below.
          href={link.kind === "fragment" ? href : "#"}
          onAuxClick={(event) => event.preventDefault()}
          onClick={(event) => {
            event.preventDefault();
            // Scroll immediately even if this is the current hash: the router may see no state
            // change, but the reader may have manually scrolled away since the last activation.
            if (link.kind === "fragment" && root.current) {
              scrollExternalMarkdownFragment(root.current, href);
            }
            onOpenLink?.(href);
          }}
        >
          {children}
        </a>
      );
    },
    img: ({ src, alt }) => <ExternalImage source={typeof src === "string" ? src : ""} alt={alt ?? ""} resolve={resolveImage} />,
    pre: ({ node, children }) => {
      const code = node?.children[0];
      if (code?.type !== "element" || code.tagName !== "code") return <pre>{children}</pre>;
      const classes = code.properties.className;
      const lang = (Array.isArray(classes) ? classes : [classes])
        .map((value) => /^language-(.+)$/.exec(String(value))?.[1])
        .find((value) => value !== undefined) ?? "";
      return <CodeBlock lang={lang} text={codeText(code).replace(/\n$/, "")} />;
    },
    code: ({ children }) => <code className="inline-code">{children}</code>,
    input: ({ type, checked }) => type === "checkbox" ? <input type="checkbox" checked={checked === true} disabled readOnly /> : null,
  }), [onOpenLink, resolveImage]);

  return (
    <div ref={root} className="markdown-view">
      {title && showTitle ? <h1 className="note-title">{title}</h1> : null}
      {markdown.trim() === "" ? <p className="muted">Empty document.</p> : null}
      <Markdown
        remarkPlugins={[
          remarkGfm,
          remarkBreakHTML,
          remarkAlert,
          ...(math ? [math.remark] : []),
          remarkExternalHeadingIDs,
        ]}
        rehypePlugins={[
          // KaTeX's trust option controls URL/HTML commands such as \\includegraphics and \\href.
          // Explicitly refuse them even if the shared math loader changes its defaults later.
          ...(math ? [[math.rehype as Plugin, { trust: false, maxExpand: 1000 }] as [Plugin, { trust: false; maxExpand: number }]] : []),
          ...(__TRACK_STATIC__ ? [] : [rehypeBudoux]),
        ]}
        components={components}
      >
        {markdown}
      </Markdown>
    </div>
  );
}

function codeText(node: Element): string {
  return node.children.map((child) => child.type === "text" ? child.value : child.type === "element" ? codeText(child) : "").join("");
}

function ExternalImage({ source, alt, resolve }: {
  source: string;
  alt: string;
  resolve: ExternalMarkdownOptions["resolveImage"];
}): ReactNode {
  const [loaded, setLoaded] = useState<{
    source: string;
    resolve: ExternalMarkdownOptions["resolveImage"];
    image: ExternalMarkdownImage;
  } | null>(null);
  useEffect(() => {
    if (!resolve || !isRelativeMarkdownImage(source)) return;
    let cancelled = false;
    // Starting with a promise handles both sync resolver errors and rejected async reads.
    void Promise.resolve().then(() => resolve(source)).then((image) => {
      if (!cancelled && isExternalMarkdownImage(image)) setLoaded({ source, resolve, image });
    }).catch(() => { /* Unreadable/unapproved assets stay inert. */ });
    return () => { cancelled = true; };
  }, [source, resolve]);

  const image = loaded?.source === source && loaded.resolve === resolve ? loaded.image : undefined;
  return image ? (
    <img src={image.src} alt={alt} loading="lazy" decoding="async" referrerPolicy="no-referrer" />
  ) : (
    <span className="muted" role="img" aria-label={alt || "Image unavailable"}>[Image unavailable{alt ? `: ${alt}` : ""}]</span>
  );
}
