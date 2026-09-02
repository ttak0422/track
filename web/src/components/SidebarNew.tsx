import { Link } from "@tanstack/react-router";
import { createPortal } from "react-dom";
import { useEffect, useRef, useState, type CSSProperties, type FocusEvent } from "react";
import { useNewNotesQuery } from "../queries";
import { hoverOpen } from "./hoverOpen";
import { railAnchor } from "./railAnchor";
import { IconPlus, RailIcon } from "./icons";

// SidebarNew is the rail's creation button plus the vault's recently-created notes. It deliberately
// differs from SidebarHistory, which remembers notes opened in this browser.
export function SidebarNew() {
  const query = useNewNotesQuery(100);
  const notes = query.data?.notes ?? [];
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<CSSProperties | undefined>(undefined);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);

  function cancelClose() {
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
  }

  function showPanel() {
    cancelClose();
    setAnchor(railAnchor(toggleRef.current));
    setOpen(true);
  }

  function scheduleClose() {
    cancelClose();
    closeTimer.current = window.setTimeout(() => setOpen(false), 160);
  }

  function focusPanel(event: FocusEvent<HTMLButtonElement>) {
    if (event.target.matches(":focus-visible")) showPanel();
  }

  useEffect(() => cancelClose, []);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: MouseEvent) {
      const target = event.target as Node;
      if (!toggleRef.current?.contains(target) && !panelRef.current?.contains(target)) {
        setOpen(false);
      }
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

  const panel = open ? (
    <div
      ref={panelRef}
      className="menu-panel note-menu-panel history-panel"
      style={anchor}
      {...hoverOpen(cancelClose, scheduleClose)}
    >
      <h2 className="rail-panel-title">New</h2>
      <div className="history-scroll" role="menu" aria-label="Recently created notes">
        {notes.length === 0 ? (
          <p className="history-empty">No recently created notes</p>
        ) : (
          <ul className="history-list">
            {notes.map((note) => (
              <li key={note.note_id}>
                <Link
                  className="backlink"
                  role="menuitem"
                  to="/notes/$noteId"
                  params={{ noteId: String(note.note_id) }}
                  title={note.title || note.note_id}
                  onClick={() => setOpen(false)}
                >
                  {note.title || note.note_id}
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  ) : null;

  return (
    <div className="rail-new" {...hoverOpen(showPanel, scheduleClose)}>
      <button
        ref={toggleRef}
        className="rail-button"
        type="button"
        aria-label="Recently created notes"
        aria-haspopup="menu"
        aria-expanded={open}
        onFocus={focusPanel}
        onClick={() => (open ? setOpen(false) : showPanel())}
      >
        <NewIcon />
      </button>
      {panel && typeof document !== "undefined" ? createPortal(panel, document.body) : null}
    </div>
  );
}

function NewIcon() {
  return <RailIcon Icon={IconPlus} />;
}
