import { useEffect, useRef, useState } from "react";
import { copyRich, copyText } from "./markdown/clipboard";
import { toPortableMarkdown } from "./markdown/portable";

interface NoteActionsMenuProps {
  body: string;
  onMeta: () => void;
  onDelete: () => void;
}

// portableToHtml renders portable Markdown to a static HTML string for the rich (Confluence) copy. It
// reuses the app's react-markdown pipeline (default HTML tags + GFM tables/strikethrough/task lists), so
// the output is semantic HTML a rich editor can paste. react-dom/server and the markdown deps are
// dynamically imported so they load only when the action is used, staying out of the main chunk.
async function portableToHtml(portable: string): Promise<string> {
  const [{ renderToStaticMarkup }, { default: Markdown }, { default: remarkGfm }] = await Promise.all([
    import("react-dom/server"),
    import("react-markdown"),
    import("remark-gfm"),
  ]);
  return renderToStaticMarkup(<Markdown remarkPlugins={[remarkGfm]}>{portable}</Markdown>);
}

// NoteActionsMenu collapses the note's infrequent actions — Copy MD, Copy for Confluence, Meta, Delete —
// behind a single overflow trigger so the control bar keeps only Follow and the mode switch inline. The
// two copy items own their clipboard handlers (portable Markdown as plain text, rich HTML for Confluence)
// and briefly acknowledge with "Copied"; Meta and Delete defer to the editor's existing dialogs.
export function NoteActionsMenu({ body, onMeta, onDelete }: NoteActionsMenuProps) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState<"md" | "html" | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    return () => {
      if (resetTimer.current !== undefined) window.clearTimeout(resetTimer.current);
    };
  }, []);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: MouseEvent) {
      if (!menuRef.current?.contains(event.target as Node)) setOpen(false);
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  function acknowledge(which: "md" | "html") {
    setCopied(which);
    if (resetTimer.current !== undefined) window.clearTimeout(resetTimer.current);
    resetTimer.current = window.setTimeout(() => setCopied(null), 1500);
  }

  async function copyMarkdown() {
    if (await copyText(toPortableMarkdown(body))) acknowledge("md");
  }

  async function copyConfluence() {
    const portable = toPortableMarkdown(body);
    const html = await portableToHtml(portable);
    if (await copyRich(html, portable)) acknowledge("html");
  }

  return (
    <div className="note-menu" ref={menuRef}>
      <button
        className="note-menu-toggle"
        type="button"
        aria-label="More actions"
        title="More actions"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        ⋯
      </button>
      {open ? (
        <div className="menu-panel note-menu-panel" role="menu">
          <button type="button" role="menuitem" onClick={copyMarkdown}>
            {copied === "md" ? "Copied" : "Copy MD"}
          </button>
          <button type="button" role="menuitem" onClick={copyConfluence}>
            {copied === "html" ? "Copied" : "Copy for Confluence"}
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              setOpen(false);
              onMeta();
            }}
          >
            Meta…
          </button>
          <button
            type="button"
            role="menuitem"
            className="danger-item"
            onClick={() => {
              setOpen(false);
              onDelete();
            }}
          >
            Delete…
          </button>
        </div>
      ) : null}
    </div>
  );
}
