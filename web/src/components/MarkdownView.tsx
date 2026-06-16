import { Link } from "@tanstack/react-router";
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
import { useNoteQuery, useResolveQuery } from "../queries";

// Nesting depth of the current markdown render. Each preview renders its body
// one level deeper so nested previews can stack in front of their parent.
const PreviewDepthContext = createContext(0);

// Base stacking order for previews; deeper levels sit in front.
const previewBaseZIndex = 100;

interface MarkdownViewProps {
  markdown: string;
}

export function MarkdownView({ markdown }: MarkdownViewProps) {
  const blocks = parseMarkdown(markdown);

  if (blocks.length === 0) {
    return <p className="muted">Empty note.</p>;
  }

  return (
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
          case "code":
            return <CodeBlock key={index} lang={block.lang} text={block.text} />;
        }
      })}
    </div>
  );
}

function renderInline(text: string) {
  return renderParts(parseInline(text));
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
        return <Fragment key={index}>{part.text}</Fragment>;
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
  const action = href.startsWith("<") && href.endsWith(">");
  const noteCandidate = action ? "" : noteCandidateFromHref(href);
  const resolved = useResolveQuery(noteCandidate);

  if (action) {
    return <span className="md-link action">{children}</span>;
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
              <MarkdownView markdown={note.data.note.body} />
            </PreviewDepthContext.Provider>
          </div>
        </>
      ) : null}
    </aside>
  );
}
