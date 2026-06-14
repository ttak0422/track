import { Link } from "@tanstack/react-router";
import { Fragment, useState } from "react";
import { excerpt, parseInline, parseMarkdown } from "../markdown";
import { useNoteQuery, useResolveQuery } from "../queries";

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

function WikiLink({ target, display }: WikiLinkProps) {
  const [open, setOpen] = useState(false);
  const resolved = useResolveQuery(target);
  const noteID = resolved.data?.found ? resolved.data.note.note_id : undefined;

  if (resolved.isPending) {
    return <span className="wiki-link pending">{display}</span>;
  }

  if (!noteID) {
    return <span className="wiki-link unresolved">{display}</span>;
  }

  return (
    <span
      className="wiki-link-wrap"
      onBlur={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      <Link className="wiki-link" to="/notes/$noteId" params={{ noteId: String(noteID) }}>
        {display}
      </Link>
      {open ? <WikiPreview noteID={noteID} /> : null}
    </span>
  );
}

interface WikiPreviewProps {
  noteID: number;
}

function WikiPreview({ noteID }: WikiPreviewProps) {
  const note = useNoteQuery(noteID);

  return (
    <aside className="wiki-preview">
      {note.isPending ? <p className="muted">Loading...</p> : null}
      {note.isError ? <p className="error">{note.error.message}</p> : null}
      {note.data ? (
        <>
          <strong>{note.data.note.title}</strong>
          <p>{excerpt(note.data.note.body)}</p>
        </>
      ) : null}
    </aside>
  );
}
