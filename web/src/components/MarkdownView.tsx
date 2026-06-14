import { Link } from "@tanstack/react-router";
import { createContext, Fragment, useContext, useEffect, useRef, useState } from "react";
import { parseInline, parseMarkdown } from "../markdown";
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
        }
      })}
    </div>
  );
}

function renderInline(text: string) {
  return parseInline(text).map((part, index) => {
    if (part.type === "text") {
      return <Fragment key={index}>{part.text}</Fragment>;
    }
    return <WikiLink display={part.display} key={`${part.target}-${index}`} target={part.target} />;
  });
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
