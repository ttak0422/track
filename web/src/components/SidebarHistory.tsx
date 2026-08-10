import { Link } from "@tanstack/react-router";
import { createPortal } from "react-dom";
import { useEffect, useRef, useState } from "react";
import { useTabs } from "./tabs/tabsStore";

// SidebarHistory is the rail's clock button plus the browser-local list of notes it recently opened.
// The panel is portalled because the fixed rail owns a stacking context below floating previews; a
// menu rendered inside it could never rise above those previews.
export function SidebarHistory() {
  const { recent } = useTabs();
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<{ top: number; left: number } | null>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);

  function cancelClose() {
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
  }

  function showPanel() {
    cancelClose();
    const rect = toggleRef.current?.getBoundingClientRect();
    setAnchor(rect ? { top: rect.top, left: rect.right + 12 } : null);
    setOpen(true);
  }

  function scheduleClose() {
    cancelClose();
    closeTimer.current = window.setTimeout(() => setOpen(false), 160);
  }

  useEffect(() => {
    return cancelClose;
  }, []);

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
    // The title stays outside the menu: a heading is not a menu item, and the panel opens away from
    // the glyph that summoned it, so it has to say which one that was.
    <div
      ref={panelRef}
      className="menu-panel note-menu-panel history-panel"
      style={anchor ?? undefined}
      onPointerEnter={cancelClose}
      onPointerLeave={scheduleClose}
    >
      <h2 className="rail-panel-title">History</h2>
      <div className="history-scroll" role="menu" aria-label="Recently opened notes">
      {recent.length === 0 ? (
        <p className="history-empty">No recently opened notes</p>
      ) : (
        <ul className="history-list">
          {recent.map((note) => (
            <li key={note.id}>
              <Link
                className="backlink"
                role="menuitem"
                to="/notes/$noteId"
                params={{ noteId: String(note.id) }}
                title={note.title || note.id}
                onClick={() => setOpen(false)}
              >
                {note.title || note.id}
              </Link>
            </li>
          ))}
        </ul>
      )}
      </div>
    </div>
  ) : null;

  return (
    <div className="rail-history" onPointerEnter={showPanel} onPointerLeave={scheduleClose}>
      <button
        ref={toggleRef}
        className="rail-button"
        type="button"
        aria-label="Recently opened notes"
        title="Recently opened notes"
        aria-haspopup="menu"
        aria-expanded={open}
        onFocus={showPanel}
        onClick={() => (open ? setOpen(false) : showPanel())}
      >
        <HistoryIcon />
      </button>
      {panel && typeof document !== "undefined" ? createPortal(panel, document.body) : null}
    </div>
  );
}

function HistoryIcon() {
  return (
    <svg
      className="rail-icon-svg"
      viewBox="0 0 24 24"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M3.5 8.5V4l4.5 0" />
      <path d="M3.7 4.6A8.5 8.5 0 1 1 3.5 12" />
      <path d="M12 7v5l3 2" />
    </svg>
  );
}
