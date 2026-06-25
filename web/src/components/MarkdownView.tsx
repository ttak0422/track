import { Link } from "@tanstack/react-router";
import { loadDefaultJapaneseParser } from "budoux";
import {
  createContext,
  Fragment,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { parseInline, parseMarkdown, type InlinePart } from "../markdown";
import { useNoteQuery, useOgpQuery, useResolveQuery } from "../queries";
import { PdfDeck } from "./PdfDeck";

// Nesting depth of the current markdown render. Each preview renders its body
// one level deeper so nested previews can stack in front of their parent.
const PreviewDepthContext = createContext(0);

// Kind ("note"/"journal") of the note being rendered, so relative "assets/<file>" references resolve to
// the right per-kind assets directory on the server. Defaults to "note".
const NoteKindContext = createContext<string>("note");

// Base stacking order for previews; deeper levels sit in front.
const previewBaseZIndex = 100;

interface MarkdownViewProps {
  markdown: string;
  kind?: string;
}

export function MarkdownView({ markdown, kind = "note" }: MarkdownViewProps) {
  const blocks = parseMarkdown(markdown);

  if (blocks.length === 0) {
    return <p className="muted">Empty note.</p>;
  }

  return (
    <NoteKindContext.Provider value={kind}>
    <div className="markdown-view">
      {blocks.map((block, index) => {
        switch (block.type) {
          case "heading": {
            const Heading = `h${block.level}` as "h1" | "h2" | "h3";
            return <Heading key={index}>{renderInline(block.text)}</Heading>;
          }
          case "paragraph":
            return <p key={index}>{renderInline(block.text)}</p>;
          case "list":
            return (
              <ul key={index}>
                {block.items.map((item, itemIndex) => (
                  <li key={itemIndex}>{renderInline(item)}</li>
                ))}
              </ul>
            );
          case "task":
            return (
              <label className="task-item" key={index}>
                <input type="checkbox" checked={block.checked} readOnly />
                <span>{renderInline(block.text)}</span>
              </label>
            );
          case "embed":
            return <Embed key={index} src={block.src} alt={block.alt} />;
          case "code":
            return <CodeBlock key={index} lang={block.lang} text={block.text} />;
        }
      })}
    </div>
    </NoteKindContext.Provider>
  );
}

function renderInline(text: string) {
  return renderParts(parseInline(text));
}

// BudouX segments Japanese text at phrase boundaries. Paired with CSS `word-break: keep-all`, the
// inserted <wbr> markers let lines wrap between phrases instead of at arbitrary characters, so long
// Japanese paragraphs read naturally on wide viewports.
const jaParser = loadDefaultJapaneseParser();

function renderWordBreaks(text: string): ReactNode {
  if (text === "") {
    return text;
  }
  const segments = jaParser.parse(text);
  if (segments.length <= 1) {
    return text;
  }
  return segments.map((segment, index) => (
    <Fragment key={index}>
      {index > 0 ? <wbr /> : null}
      {segment}
    </Fragment>
  ));
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

interface HighlightToken {
  text: string;
  className?: string;
}

const keywordSets: Record<string, Set<string>> = {
  css: new Set(["important", "from", "to"]),
  go: new Set([
    "break",
    "case",
    "chan",
    "const",
    "continue",
    "default",
    "defer",
    "else",
    "fallthrough",
    "for",
    "func",
    "go",
    "goto",
    "if",
    "import",
    "interface",
    "map",
    "package",
    "range",
    "return",
    "select",
    "struct",
    "switch",
    "type",
    "var",
  ]),
  js: new Set([
    "await",
    "break",
    "case",
    "catch",
    "class",
    "const",
    "continue",
    "default",
    "else",
    "export",
    "extends",
    "finally",
    "for",
    "from",
    "function",
    "if",
    "import",
    "let",
    "new",
    "return",
    "switch",
    "throw",
    "try",
    "typeof",
    "var",
    "while",
  ]),
  lua: new Set([
    "and",
    "break",
    "do",
    "else",
    "elseif",
    "end",
    "false",
    "for",
    "function",
    "if",
    "in",
    "local",
    "nil",
    "not",
    "or",
    "repeat",
    "return",
    "then",
    "true",
    "until",
    "while",
  ]),
  sh: new Set(["case", "do", "done", "elif", "else", "esac", "fi", "for", "function", "if", "in", "then", "while"]),
};

keywordSets.ts = keywordSets.js;
keywordSets.tsx = keywordSets.js;
keywordSets.jsx = keywordSets.js;
keywordSets.javascript = keywordSets.js;
keywordSets.typescript = keywordSets.js;
keywordSets.bash = keywordSets.sh;
keywordSets.shell = keywordSets.sh;
keywordSets.zsh = keywordSets.sh;

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

function tokenizeCode(text: string, lang: string): HighlightToken[] {
  const normalized = normalizeCodeLang(lang);
  if (normalized === "") return [{ text }];
  if (normalized === "json") return tokenizeGeneric(text, normalized, jsonKeyword);
  if (normalized === "yaml" || normalized === "yml") return tokenizeYaml(text);
  if (normalized === "html" || normalized === "xml") return tokenizeHtml(text);
  if (normalized === "md" || normalized === "markdown") return tokenizeMarkdownCode(text);
  return tokenizeGeneric(text, normalized, keywordSets[normalized]);
}

function normalizeCodeLang(lang: string): string {
  const first = lang.trim().split(/\s+/)[0] ?? "";
  return first.replace(/^language-/, "").toLowerCase();
}

function tokenizeGeneric(text: string, lang: string, keywords?: Set<string>): HighlightToken[] {
  const out: HighlightToken[] = [];
  let i = 0;
  while (i < text.length) {
    const rest = text.slice(i);
    const comment = matchComment(rest, lang);
    if (comment) {
      out.push({ text: comment, className: "syntax-comment" });
      i += comment.length;
      continue;
    }
    const string = matchPrefix(rest, /^`(?:\\.|[^`\\])*`|^"(?:\\.|[^"\\])*"|^'(?:\\.|[^'\\])*'/);
    if (string) {
      out.push({ text: string, className: "syntax-string" });
      i += string.length;
      continue;
    }
    const number = matchPrefix(rest, /^\b\d+(?:\.\d+)?\b/);
    if (number) {
      out.push({ text: number, className: "syntax-number" });
      i += number.length;
      continue;
    }
    const word = matchPrefix(rest, /^[A-Za-z_][\w-]*/);
    if (word) {
      if (keywords?.has(word)) {
        out.push({ text: word, className: "syntax-keyword" });
      } else if (/^\s*\(/.test(text.slice(i + word.length))) {
        out.push({ text: word, className: "syntax-function" });
      } else {
        out.push({ text: word });
      }
      i += word.length;
      continue;
    }
    out.push({ text: text[i] });
    i += 1;
  }
  return out;
}

function matchComment(rest: string, lang: string): string {
  if (rest.startsWith("/*")) {
    const end = rest.indexOf("*/", 2);
    return end === -1 ? rest : rest.slice(0, end + 2);
  }
  if (lang === "lua" && rest.startsWith("--")) {
    return rest.slice(0, lineEnd(rest));
  }
  if ((lang === "sh" || lang === "bash" || lang === "shell" || lang === "zsh") && rest.startsWith("#")) {
    return rest.slice(0, lineEnd(rest));
  }
  if (rest.startsWith("//")) {
    return rest.slice(0, lineEnd(rest));
  }
  return "";
}

function lineEnd(text: string): number {
  const next = text.indexOf("\n");
  return next === -1 ? text.length : next;
}

function matchPrefix(text: string, pattern: RegExp): string {
  return pattern.exec(text)?.[0] ?? "";
}

const jsonKeyword = new Set(["false", "null", "true"]);

function tokenizeYaml(text: string): HighlightToken[] {
  return text.split(/(\n)/).flatMap((line) => {
    if (line === "\n") return [{ text: line }];
    const match = /^(\s*)([-\w.]+)(\s*:)/.exec(line);
    if (!match) return tokenizeGeneric(line, "yaml");
    const rest = line.slice(match[0].length);
    return [
      { text: match[1] },
      { text: match[2], className: "syntax-property" },
      { text: match[3] },
      ...tokenizeGeneric(rest, "yaml"),
    ];
  });
}

function tokenizeHtml(text: string): HighlightToken[] {
  const out: HighlightToken[] = [];
  const pattern = /(<!--[\s\S]*?-->|<\/?[A-Za-z][^>]*>)/g;
  let last = 0;
  for (const match of text.matchAll(pattern)) {
    if (match.index > last) out.push({ text: text.slice(last, match.index) });
    out.push({ text: match[0], className: match[0].startsWith("<!--") ? "syntax-comment" : "syntax-keyword" });
    last = match.index + match[0].length;
  }
  if (last < text.length) out.push({ text: text.slice(last) });
  return out;
}

function tokenizeMarkdownCode(text: string): HighlightToken[] {
  return text.split(/(\n)/).flatMap((line) => {
    if (/^\s*#{1,6}\s/.test(line)) return [{ text: line, className: "syntax-keyword" }];
    if (/^\s*[-*]\s/.test(line)) return [{ text: line, className: "syntax-property" }];
    return [{ text: line }];
  });
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

function renderParts(parts: InlinePart[]) {
  return parts.map((part, index) => {
    switch (part.type) {
      case "text":
        return <Fragment key={index}>{renderWordBreaks(part.text)}</Fragment>;
      case "wiki":
        return <WikiLink display={part.display} key={index} target={part.target} />;
      case "link":
        return (
          <ExternalLink href={part.href} key={index}>
            {renderParts(part.children)}
          </ExternalLink>
        );
      case "code":
        return (
          <code className="inline-code" key={index}>
            {part.text}
          </code>
        );
      case "strong":
        return <strong key={index}>{renderParts(part.children)}</strong>;
      case "em":
        return <em key={index}>{renderParts(part.children)}</em>;
      case "del":
        return <del key={index}>{renderParts(part.children)}</del>;
    }
  });
}

interface ExternalLinkProps {
  href: string;
  children: ReactNode;
}

// ExternalLink renders a standard markdown [text](href). Track action links wrap the destination in
// angle brackets (e.g. [今日](<journal?offset=0>)); those are editor-only and not web-navigable, so we
// render their label as plain text. Non-action links first try to resolve as track notes; otherwise
// http(s) and domain-like links open in a new tab.
function ExternalLink({ href, children }: ExternalLinkProps) {
  const kind = useContext(NoteKindContext);
  const asset = assetHref(href, kind);
  const action = href.startsWith("<") && href.endsWith(">");
  const noteCandidate = action || asset ? "" : noteCandidateFromHref(href);
  const resolved = useResolveQuery(noteCandidate);

  if (action) {
    return <>{children}</>;
  }
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

function noteCandidateFromHref(href: string): string {
  const trimmed = href.trim();
  if (
    trimmed === "" ||
    trimmed.startsWith("#") ||
    /^[a-z][a-z0-9+.-]*:/i.test(trimmed)
  ) {
    return "";
  }
  const withoutHash = trimmed.split("#", 1)[0] ?? "";
  const withoutQuery = withoutHash.split("?", 1)[0] ?? "";
  const withoutExt = withoutQuery.replace(/\.md$/i, "");
  try {
    return decodeURIComponent(withoutExt).trim();
  } catch {
    return withoutExt.trim();
  }
}

function webHref(href: string): string {
  const trimmed = href.trim();
  if (/^www\./i.test(trimmed) || /^[\w.-]+\.[a-z]{2,}(?:[/:?#]|$)/i.test(trimmed)) {
    return `https://${trimmed}`;
  }
  return href;
}

// assetHref maps a note-relative attachment reference ("assets/<file>", optionally written "./assets/…")
// to the local server endpoint that serves it from the vault's per-kind assets directory. It returns
// null for anything that is not such a reference (absolute URLs, schemes, anchors, root-absolute paths),
// leaving those to the normal link/embed handling.
function assetHref(src: string, kind: string): string | null {
  const trimmed = src.trim();
  if (
    trimmed === "" ||
    trimmed.startsWith("/") ||
    trimmed.startsWith("#") ||
    /^[a-z][a-z0-9+.-]*:/i.test(trimmed)
  ) {
    return null;
  }
  const rel = trimmed.replace(/^\.\//, "");
  if (!rel.startsWith("assets/")) {
    return null;
  }
  const name = rel.slice("assets/".length);
  if (name === "") {
    return null;
  }
  const params = new URLSearchParams({ kind: kind || "note", name });
  return `/api/asset?${params}`;
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

// tweetIdFromUrl returns the numeric status id of a Twitter/X post URL, or null for any other URL.
function tweetIdFromUrl(src: string): string | null {
  let url: URL;
  try {
    url = new URL(webHref(src));
  } catch {
    return null;
  }
  const host = url.hostname.replace(/^www\./i, "").replace(/^mobile\./i, "").toLowerCase();
  if (host !== "twitter.com" && host !== "x.com") {
    return null;
  }
  return /^\/[^/]+\/status(?:es)?\/(\d+)/.exec(url.pathname)?.[1] ?? null;
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

function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

function isImageHref(src: string): boolean {
  const path = src.split(/[?#]/, 1)[0] ?? "";
  return /\.(png|jpe?g|gif|webp|avif|svg|bmp|ico)$/i.test(path.trim());
}

// youtubeEmbedUrl turns a YouTube watch/share/shorts/embed URL into a privacy-enhanced embed URL,
// carrying a start time when the original had one (t= or start=). It returns null for non-YouTube URLs
// so the caller can fall back to a PDF/image embed.
function youtubeEmbedUrl(src: string): string | null {
  let url: URL;
  try {
    url = new URL(webHref(src));
  } catch {
    return null;
  }
  const host = url.hostname.replace(/^www\./i, "").toLowerCase();
  let id = "";
  if (host === "youtu.be") {
    id = url.pathname.slice(1).split("/")[0] ?? "";
  } else if (host === "youtube.com" || host === "m.youtube.com" || host === "youtube-nocookie.com") {
    if (url.pathname === "/watch") {
      id = url.searchParams.get("v") ?? "";
    } else {
      id = /^\/(?:embed|shorts|live|v)\/([^/?#]+)/.exec(url.pathname)?.[1] ?? "";
    }
  }
  if (!/^[\w-]{6,}$/.test(id)) {
    return null;
  }
  const start = youtubeStartSeconds(url.searchParams.get("t") ?? url.searchParams.get("start"));
  const query = start > 0 ? `?start=${start}` : "";
  return `https://www.youtube-nocookie.com/embed/${id}${query}`;
}

// youtubeStartSeconds parses a YouTube timestamp, accepting plain seconds ("90") and the 1h2m3s form.
function youtubeStartSeconds(raw: string | null): number {
  if (!raw) {
    return 0;
  }
  if (/^\d+$/.test(raw)) {
    return Number(raw);
  }
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(raw);
  if (!match || (!match[1] && !match[2] && !match[3])) {
    return 0;
  }
  return Number(match[1] ?? 0) * 3600 + Number(match[2] ?? 0) * 60 + Number(match[3] ?? 0);
}

function isPdfHref(src: string): boolean {
  const path = src.split(/[?#]/, 1)[0] ?? "";
  return /\.pdf$/i.test(path.trim());
}

// safeFrameUrl returns the URL only when it is safe to load in an iframe: http(s) or a same-origin
// relative path. It rejects javascript:/data: and other schemes that could run script in the frame.
function safeFrameUrl(target: string): string | null {
  const trimmed = target.trim();
  if (/^https?:\/\//i.test(trimmed) || trimmed.startsWith("/") || trimmed.startsWith("./")) {
    return trimmed;
  }
  return null;
}

interface WikiLinkProps {
  target: string;
  display: string;
}

interface PreviewAnchor {
  left: number;
  top: number;
}

function WikiLink({ target, display }: WikiLinkProps) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<PreviewAnchor | null>(null);
  const linkRef = useRef<HTMLAnchorElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);
  const depth = useContext(PreviewDepthContext);
  const resolved = useResolveQuery(target);
  const noteID = resolved.data?.found ? resolved.data.note.note_id : undefined;

  useEffect(() => {
    return () => {
      if (closeTimer.current !== undefined) {
        window.clearTimeout(closeTimer.current);
      }
    };
  }, []);

  function openPreview() {
    if (closeTimer.current !== undefined) {
      window.clearTimeout(closeTimer.current);
    }
    const rect = linkRef.current?.getBoundingClientRect();
    if (rect) {
      setAnchor({ left: rect.left, top: rect.bottom + 8 });
    }
    setOpen(true);
  }

  function scheduleClose() {
    if (closeTimer.current !== undefined) {
      window.clearTimeout(closeTimer.current);
    }
    closeTimer.current = window.setTimeout(() => setOpen(false), 220);
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
      onMouseEnter={openPreview}
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
      {open && anchor ? <WikiPreview noteID={noteID} anchor={anchor} depth={depth} /> : null}
    </span>
  );
}

interface WikiPreviewProps {
  noteID: number;
  anchor: PreviewAnchor;
  depth: number;
}

function WikiPreview({ noteID, anchor, depth }: WikiPreviewProps) {
  const note = useNoteQuery(noteID);

  return (
    <aside
      className="wiki-preview"
      style={{ left: anchor.left, top: anchor.top, zIndex: previewBaseZIndex + depth }}
    >
      {note.isPending ? <p className="muted">Loading...</p> : null}
      {note.isError ? <p className="error">{note.error.message}</p> : null}
      {note.data ? (
        <>
          <strong>{note.data.note.title}</strong>
          <div className="wiki-preview-body">
            <PreviewDepthContext.Provider value={depth + 1}>
              <MarkdownView markdown={note.data.note.body} kind={note.data.note.file_kind} />
            </PreviewDepthContext.Provider>
          </div>
        </>
      ) : null}
    </aside>
  );
}
