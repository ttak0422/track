import { Link } from "@tanstack/react-router";
import { loadDefaultJapaneseParser } from "budoux";
import type { Element, Root as HastRoot, Text as HastText } from "hast";
import type { Root as MdastRoot } from "mdast";
import {
  createContext,
  Fragment,
  type ReactNode,
  type PointerEvent,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { visit } from "unist-util-visit";
import { useNoteQuery, useOgpQuery, useRenderQuery, useResolveQuery } from "../queries";
import { tokenizeCode } from "./markdown/highlight";
import {
  assetHref,
  hostOf,
  isImageHref,
  isPdfHref,
  noteCandidateFromHref,
  safeFrameUrl,
  tweetIdFromUrl,
  webHref,
  youtubeEmbedUrl,
} from "./markdown/urls";
import {
  constrainPreviewBounds,
  initialPreviewBounds,
  type PreviewAnchor,
  type PreviewBounds,
  type PreviewResizeCorner,
  resizePreviewBounds,
} from "./preview/bounds";
import { PdfDeck } from "./PdfDeck";

// Nesting depth of the current markdown render. Each preview renders its body
// one level deeper so nested previews can stack in front of their parent.
const PreviewDepthContext = createContext(0);

// Kind ("note"/"journal") of the note being rendered, so relative "assets/<file>" references resolve to
// the right per-kind assets directory on the server. Defaults to "note".
const NoteKindContext = createContext<string>("note");

// Base stacking order for previews. Each interaction bumps a preview above the other open previews.
const previewBaseZIndex = 100;
// Hover intent: only open a preview once the pointer rests on a link, so sweeping the cursor down a
// column of links does not flash a popup under every one it crosses.
const previewOpenDelay = 260;
let previewStackOrder = 0;

function nextPreviewStackOrder(): number {
  previewStackOrder += 1;
  return previewStackOrder;
}

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
      return <CodeBlock lang={codeLanguage(code)} text={hastText(code)} />;
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

// isSoleImage reports whether a paragraph node wraps nothing but a single image (ignoring whitespace).
function isSoleImage(node?: Element): boolean {
  if (!node) return false;
  const kids = node.children.filter((c) => !(c.type === "text" && c.value.trim() === ""));
  return kids.length === 1 && kids[0].type === "element" && kids[0].tagName === "img";
}

const wikiPattern = /\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g;

// remarkWikiLink rewrites [[target|display]] text into a custom "wikilink" element carrying the target
// and display as properties, so markdownComponents can render it as a navigable, hover-previewable link.
function remarkWikiLink() {
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

// BudouX segments Japanese text at phrase boundaries. Paired with CSS `word-break: keep-all`, the
// inserted <wbr> markers let lines wrap between phrases instead of at arbitrary characters, so long
// Japanese paragraphs read naturally on wide viewports.
const jaParser = loadDefaultJapaneseParser();

// rehypeBudoux runs on the rendered tree (after wiki links and code are elements) and replaces each text
// node with its BudouX phrase segments separated by <wbr>. Text inside code/pre is left untouched.
function rehypeBudoux() {
  return (tree: HastRoot) => {
    visit(tree, "text", (node, index, parent) => {
      if (!parent || index === undefined) return;
      if (parent.type === "element" && (parent.tagName === "code" || parent.tagName === "pre")) {
        return;
      }
      const segments = jaParser.parse(node.value);
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
}

interface CodeBlockProps {
  lang: string;
  text: string;
}

// CodeBlock renders a fenced code block with a copy-to-clipboard button. The button briefly switches
// to a "Copied" state so the action is acknowledged, then resets.
function CodeBlock({ lang, text }: CodeBlockProps) {
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    return () => {
      if (resetTimer.current !== undefined) {
        window.clearTimeout(resetTimer.current);
      }
    };
  }, []);

  async function copy() {
    const ok = await copyText(text);
    if (!ok) return;
    setCopied(true);
    if (resetTimer.current !== undefined) {
      window.clearTimeout(resetTimer.current);
    }
    resetTimer.current = window.setTimeout(() => setCopied(false), 1500);
  }

  return (
    <div className="code-block" data-language={lang || undefined}>
      <button
        type="button"
        className="code-copy"
        onClick={copy}
        aria-label={copied ? "Copied" : "Copy code"}
      >
        {copied ? "Copied" : "Copy"}
      </button>
      <pre className="code-block-pre">
        <code>{highlightCode(text, lang)}</code>
      </pre>
    </div>
  );
}

function highlightCode(text: string, lang: string) {
  const tokens = tokenizeCode(text, lang);
  return tokens.map((token, index) =>
    token.className ? (
      <span className={token.className} key={index}>
        {token.text}
      </span>
    ) : (
      <Fragment key={index}>{token.text}</Fragment>
    ),
  );
}

// copyText writes to the clipboard, falling back to a hidden textarea + execCommand when the async
// Clipboard API is unavailable (older browsers or non-secure contexts). Returns whether it succeeded.
async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fall through to the legacy path
    }
  }
  try {
    const area = document.createElement("textarea");
    area.value = text;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    document.body.removeChild(area);
    return ok;
  } catch {
    return false;
  }
}

interface ExternalLinkProps {
  href: string;
  children: ReactNode;
}

// ExternalLink renders a standard markdown [text](href). Track action links are flattened to plain text
// by the server before the body reaches the frontend, so they never appear here. A link first tries to
// resolve as a track note; otherwise http(s) and domain-like links open in a new tab.
function ExternalLink({ href, children }: ExternalLinkProps) {
  const kind = useContext(NoteKindContext);
  const asset = assetHref(href, kind);
  const noteCandidate = asset ? "" : noteCandidateFromHref(href);
  const resolved = useResolveQuery(noteCandidate);

  // A link into the vault's assets/ goes straight to the server endpoint that serves the file, rather
  // than being resolved against the current /notes/<id> route.
  if (asset) {
    return (
      <a className="md-link" href={asset} target="_blank" rel="noreferrer noopener">
        {children}
      </a>
    );
  }
  if (noteCandidate !== "" && resolved.data?.found) {
    return (
      <Link
        className="md-link"
        to="/notes/$noteId"
        params={{ noteId: String(resolved.data.note.note_id) }}
      >
        {children}
      </Link>
    );
  }
  const target = webHref(href);
  const external = /^https?:\/\//i.test(target);
  return (
    <a
      className="md-link"
      href={target}
      {...(external ? { target: "_blank", rel: "noreferrer noopener" } : {})}
    >
      {children}
    </a>
  );
}

interface EmbedProps {
  src: string;
  alt: string;
}

// Embed renders a standalone ![alt](src), routing by the kind of target: YouTube links become an
// inline player, PDFs a slide-deck viewer, image URLs an <img>, and any other http(s) page an Open Graph
// card. Embedding stays opt-in via the ![...]() syntax so ordinary [text](url) links are never turned
// into noisy previews. The URL is normalized through webHref so bare domains still resolve, and only
// http(s)/relative URLs feed an iframe so a note cannot smuggle a javascript: document into the frame.
function Embed({ src, alt }: EmbedProps) {
  const kind = useContext(NoteKindContext);
  // A relative "assets/<file>" reference is served from the vault by the local server. Resolving it here
  // means it is never treated as a YouTube/tweet/OGP URL and never resolved against the /notes/<id>
  // route (which the SPA fallback would answer with index.html, rendering the app inside the embed).
  const asset = assetHref(src, kind);

  const youtube = asset ? null : youtubeEmbedUrl(src);
  if (youtube) {
    return (
      <div className="embed embed-video">
        <iframe
          src={youtube}
          title={alt || "YouTube video"}
          loading="lazy"
          allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
          allowFullScreen
        />
      </div>
    );
  }

  const target = asset ?? webHref(src);
  if (isPdfHref(src)) {
    const safe = safeFrameUrl(target);
    if (safe) {
      return <PdfDeck src={safe} alt={alt} />;
    }
  }

  const tweetId = asset ? null : tweetIdFromUrl(src);
  if (tweetId) {
    return <TweetEmbed tweetId={tweetId} url={target} alt={alt} />;
  }

  if (!asset && !isImageHref(src) && /^https?:\/\//i.test(target)) {
    return <OgpCard url={target} alt={alt} />;
  }

  return <img className="embed embed-image" src={target} alt={alt} loading="lazy" />;
}

interface TweetEmbedProps {
  tweetId: string;
  url: string;
  alt: string;
}

type TweetStatus = "loading" | "ready" | "error";

// TweetEmbed renders the actual Twitter/X post (not just a card) via Twitter's official widgets.js,
// matching how Obsidian embeds tweets. While the widget loads it shows a plain link, and if the tweet
// cannot be rendered (deleted, blocked, offline) it falls back to the generic OGP card.
function TweetEmbed({ tweetId, url, alt }: TweetEmbedProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<TweetStatus>("loading");

  useEffect(() => {
    let cancelled = false;
    setStatus("loading");
    loadTwitterWidgets()
      .then((twttr) => {
        if (cancelled) return;
        const container = containerRef.current;
        if (!twttr || !container) {
          setStatus("error");
          return;
        }
        container.replaceChildren();
        return twttr.widgets
          .createTweet(tweetId, container, { dnt: true, theme: currentTheme(), conversation: "none" })
          .then((el) => {
            if (cancelled) return;
            setStatus(el ? "ready" : "error");
          });
      })
      .catch(() => {
        if (!cancelled) setStatus("error");
      });
    return () => {
      cancelled = true;
    };
  }, [tweetId]);

  if (status === "error") {
    return <OgpCard url={url} alt={alt} />;
  }
  return (
    <div className="embed embed-tweet">
      <div ref={containerRef} />
      {status === "loading" ? (
        <a className="md-link embed-fallback" href={url} target="_blank" rel="noreferrer noopener">
          {alt || url}
        </a>
      ) : null}
    </div>
  );
}

interface TwitterWidgets {
  createTweet: (
    id: string,
    container: HTMLElement,
    options?: Record<string, unknown>,
  ) => Promise<HTMLElement | undefined>;
}

interface Twttr {
  widgets: TwitterWidgets;
}

let twitterWidgetsPromise: Promise<Twttr | null> | null = null;

// loadTwitterWidgets injects Twitter's widgets.js once and resolves the global twttr API. Subsequent
// calls reuse the same promise so the script is never loaded twice.
function loadTwitterWidgets(): Promise<Twttr | null> {
  if (typeof window === "undefined" || typeof document === "undefined") {
    return Promise.resolve(null);
  }
  const existing = (window as unknown as { twttr?: Twttr }).twttr;
  if (existing?.widgets) {
    return Promise.resolve(existing);
  }
  if (twitterWidgetsPromise) {
    return twitterWidgetsPromise;
  }
  twitterWidgetsPromise = new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "https://platform.twitter.com/widgets.js";
    script.async = true;
    script.onload = () => resolve((window as unknown as { twttr?: Twttr }).twttr ?? null);
    script.onerror = () => reject(new Error("failed to load twitter widgets"));
    document.head.appendChild(script);
  });
  return twitterWidgetsPromise;
}

// currentTheme resolves the embed theme from the app's data-theme attribute, falling back to the OS
// preference when it is unset or set to "system".
function currentTheme(): "light" | "dark" {
  const attr = document.documentElement.getAttribute("data-theme");
  if (attr === "dark" || attr === "light") {
    return attr;
  }
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

interface OgpCardProps {
  url: string;
  alt: string;
}

// OgpCard fetches the link's Open Graph metadata through the local server and renders it as a card. It
// degrades gracefully: while loading it shows the host and label, and on a failed/blocked fetch it
// falls back to a plain link so the embed is never a dead end.
function OgpCard({ url, alt }: OgpCardProps) {
  const ogp = useOgpQuery(url);
  const host = hostOf(url);

  if (ogp.isError) {
    return (
      <a className="embed md-link ogp-fallback" href={url} target="_blank" rel="noreferrer noopener">
        {alt || url}
      </a>
    );
  }

  const data = ogp.data;
  const title = data?.title || alt || url;
  return (
    <a className="embed ogp-card" href={url} target="_blank" rel="noreferrer noopener">
      {data?.image ? (
        <img className="ogp-card-image" src={data.image} alt="" loading="lazy" />
      ) : null}
      <span className="ogp-card-body">
        <span className="ogp-card-site">{data?.site_name || host}</span>
        <span className="ogp-card-title">{title}</span>
        {data?.description ? <span className="ogp-card-desc">{data.description}</span> : null}
      </span>
    </a>
  );
}

interface WikiLinkProps {
  target: string;
  display: string;
}

function WikiLink({ target, display }: WikiLinkProps) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<PreviewAnchor | null>(null);
  const [pinned, setPinned] = useState(false);
  const [stackOrder, setStackOrder] = useState(nextPreviewStackOrder);
  const pinnedRef = useRef(false);
  const linkRef = useRef<HTMLAnchorElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);
  const openTimer = useRef<number | undefined>(undefined);
  const depth = useContext(PreviewDepthContext);
  const resolved = useResolveQuery(target);
  const noteID = resolved.data?.found ? resolved.data.note.note_id : undefined;

  useEffect(() => {
    return () => {
      if (closeTimer.current !== undefined) {
        window.clearTimeout(closeTimer.current);
      }
      if (openTimer.current !== undefined) {
        window.clearTimeout(openTimer.current);
      }
    };
  }, []);

  // scheduleOpen defers opening on hover until the pointer has rested on the link, so a cursor passing
  // over a column of links does not flash a preview under each one.
  function scheduleOpen() {
    holdPreview();
    if (pinnedRef.current || open || openTimer.current !== undefined) return;
    openTimer.current = window.setTimeout(() => {
      openTimer.current = undefined;
      openPreview();
    }, previewOpenDelay);
  }

  function cancelOpen() {
    if (openTimer.current !== undefined) {
      window.clearTimeout(openTimer.current);
      openTimer.current = undefined;
    }
  }

  function openPreview() {
    holdPreview();
    cancelOpen();
    // A pinned preview is a persisted window: like an OS window it is only raised by selecting it
    // (pointer down / drag), not by hovering its link, so leave its stacking order untouched here.
    if (pinnedRef.current) return;
    bringPreviewToFront();
    const rect = linkRef.current?.getBoundingClientRect();
    if (rect) {
      setAnchor({
        linkLeft: rect.left,
        linkRight: rect.right,
        linkTop: rect.top,
        linkBottom: rect.bottom,
      });
    }
    setOpen(true);
  }

  function holdPreview() {
    if (closeTimer.current !== undefined) {
      window.clearTimeout(closeTimer.current);
    }
  }

  function bringPreviewToFront() {
    setStackOrder(nextPreviewStackOrder());
  }

  function scheduleClose() {
    cancelOpen();
    if (pinnedRef.current) return;
    if (closeTimer.current !== undefined) {
      window.clearTimeout(closeTimer.current);
    }
    closeTimer.current = window.setTimeout(() => setOpen(false), 220);
  }

  function pinPreview() {
    holdPreview();
    bringPreviewToFront();
    pinnedRef.current = true;
    setPinned(true);
  }

  function closePreview() {
    holdPreview();
    pinnedRef.current = false;
    setPinned(false);
    setOpen(false);
  }

  if (resolved.isPending) {
    return <span className="wiki-link pending">{display}</span>;
  }

  if (!noteID) {
    return <span className="wiki-link unresolved">{display}</span>;
  }

  return (
    <span
      className="wiki-link-wrap"
      onBlur={scheduleClose}
      onFocus={openPreview}
      onMouseEnter={scheduleOpen}
      onMouseLeave={scheduleClose}
    >
      <Link
        className="wiki-link"
        ref={linkRef}
        to="/notes/$noteId"
        params={{ noteId: String(noteID) }}
      >
        {display}
      </Link>
      {open && anchor ? (
        <WikiPreview
          noteID={noteID}
          anchor={anchor}
          depth={depth}
          pinned={pinned}
          stackOrder={stackOrder}
          onActivate={bringPreviewToFront}
          onClose={closePreview}
          onHold={holdPreview}
          onPin={pinPreview}
        />
      ) : null}
    </span>
  );
}

interface WikiPreviewProps {
  noteID: number;
  anchor: PreviewAnchor;
  depth: number;
  pinned: boolean;
  stackOrder: number;
  onActivate: () => void;
  onClose: () => void;
  onHold: () => void;
  onPin: () => void;
}

interface PreviewDragState {
  pointerId: number;
  mode: "move" | PreviewResizeCorner;
  startX: number;
  startY: number;
  startBounds: PreviewBounds;
}

function WikiPreview({
  noteID,
  anchor,
  depth,
  pinned,
  stackOrder,
  onActivate,
  onClose,
  onHold,
  onPin,
}: WikiPreviewProps) {
  const note = useNoteQuery(noteID);
  const [bounds, setBounds] = useState(() => initialPreviewBounds(anchor));
  const dragRef = useRef<PreviewDragState | null>(null);
  // Sanitize the previewed body the same way as the main reader, so action links are flattened here too.
  const rendered = useRenderQuery(note.data?.note.body ?? "");

  useEffect(() => {
    if (!pinned) {
      setBounds(initialPreviewBounds(anchor));
    }
  }, [anchor.linkLeft, anchor.linkRight, anchor.linkTop, anchor.linkBottom, pinned]);

  function startMove(event: PointerEvent<HTMLElement>) {
    startDrag(event, "move");
  }

  function startResize(corner: PreviewResizeCorner) {
    return (event: PointerEvent<HTMLElement>) => startDrag(event, corner);
  }

  function startDrag(event: PointerEvent<HTMLElement>, mode: PreviewDragState["mode"]) {
    event.preventDefault();
    event.stopPropagation();
    onActivate();
    onPin();
    dragRef.current = {
      pointerId: event.pointerId,
      mode,
      startX: event.clientX,
      startY: event.clientY,
      startBounds: bounds,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function dragPreview(event: PointerEvent<HTMLElement>) {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    const dx = event.clientX - drag.startX;
    const dy = event.clientY - drag.startY;
    setBounds(
      drag.mode === "move"
        ? constrainPreviewBounds({
            ...drag.startBounds,
            left: drag.startBounds.left + dx,
            top: drag.startBounds.top + dy,
          })
        : resizePreviewBounds(drag.mode, drag.startBounds, dx, dy),
    );
  }

  function endDrag(event: PointerEvent<HTMLElement>) {
    const drag = dragRef.current;
    dragRef.current = null;
    if (!drag || drag.pointerId !== event.pointerId) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
  }

  const title = note.data?.note.title ?? "Preview";

  return (
    <aside
      className={`wiki-preview${pinned ? " pinned" : ""}`}
      onFocusCapture={onActivate}
      onMouseEnter={onHold}
      onPointerDownCapture={onActivate}
      style={{
        left: bounds.left,
        top: bounds.top,
        width: bounds.width,
        height: bounds.height,
        zIndex: previewBaseZIndex + depth + stackOrder,
      }}
    >
      <div
        className="wiki-preview-chrome"
        onPointerDown={startMove}
        onPointerMove={dragPreview}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        <span className="wiki-preview-title">{title}</span>
        <button
          className="wiki-preview-close"
          type="button"
          onClick={onClose}
          onPointerDown={(event) => event.stopPropagation()}
          aria-label="Close preview"
        >
          ×
        </button>
      </div>
      <div className="wiki-preview-body">
        {note.isPending ? <p className="muted">Loading...</p> : null}
        {note.isError ? <p className="error">{note.error.message}</p> : null}
        {note.data ? (
          <PreviewDepthContext.Provider value={depth + 1}>
            <MarkdownView markdown={rendered.data?.markdown ?? ""} kind={note.data.note.file_kind} />
          </PreviewDepthContext.Provider>
        ) : null}
      </div>
      {(["nw", "ne", "sw", "se"] as const).map((corner) => (
        <button
          aria-label="Resize preview"
          className={`wiki-preview-resize wiki-preview-resize-${corner}`}
          key={corner}
          onPointerCancel={endDrag}
          onPointerDown={startResize(corner)}
          onPointerMove={dragPreview}
          onPointerUp={endDrag}
          title="Resize"
          type="button"
        />
      ))}
    </aside>
  );
}
