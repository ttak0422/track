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
        <code>{text}</code>
      </pre>
    </div>
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
// render their label as plain text. http(s) links open in a new tab; other hrefs are left as-is.
function ExternalLink({ href, children }: ExternalLinkProps) {
  if (href.startsWith("<") && href.endsWith(">")) {
    return <span className="md-link action">{children}</span>;
  }
  const external = /^https?:\/\//i.test(href);
  return (
    <a
      className="md-link"
      href={href}
      {...(external ? { target: "_blank", rel: "noreferrer noopener" } : {})}
    >
      {children}
    </a>
  );
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
