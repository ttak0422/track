import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useHierarchyQuery } from "../queries";
import type { HierarchyNode } from "../types";

// The rail's hierarchy button: the vault's "up" tree, opened by hovering the glyph the way the
// display-mode menu is. The tree is built ahead of the browser — a prerendered file on the published
// site, one indexed query on the live server — and fetched only once this menu is first opened, so a
// reader who never consults it pays nothing for it. Notes the hierarchy does not place are absent,
// which on a vault using "up" for a handful of notes keeps the menu the size of that handful.
export function HierarchyMenu() {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<{ top: number; left: number } | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);
  // Once opened the query stays enabled: a bundle never changes under the page, and the live server's
  // is one query behind a 30s stale window, so a second hover redraws from cache.
  const [asked, setAsked] = useState(false);
  const { data } = useHierarchyQuery(asked);
  const roots = data?.hierarchy ?? [];

  function cancelClose() {
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
  }

  function showMenu() {
    cancelClose();
    const rect = toggleRef.current?.getBoundingClientRect();
    setAnchor(rect ? { top: rect.top, left: rect.right + 12 } : null);
    setAsked(true);
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

  return (
    <div className="hierarchy-menu" ref={menuRef} onPointerEnter={showMenu} onPointerLeave={scheduleClose}>
      <button
        ref={toggleRef}
        className="rail-button"
        type="button"
        aria-label="Hierarchy"
        title="Hierarchy"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => (open ? setOpen(false) : showMenu())}
      >
        <RailHierarchyIcon />
      </button>
      {open ? (
        <div
          className="menu-panel note-menu-panel hierarchy-panel"
          role="menu"
          aria-label="Hierarchy"
          style={anchor ?? undefined}
        >
          {roots.length === 0 ? (
            <p className="hierarchy-empty">No hierarchy</p>
          ) : (
            <HierarchyList nodes={roots} onNavigate={() => setOpen(false)} />
          )}
        </div>
      ) : null}
    </div>
  );
}

function HierarchyList({ nodes, onNavigate }: { nodes: HierarchyNode[]; onNavigate: () => void }) {
  return (
    <ul className="hierarchy-list">
      {nodes.map((node) => (
        <li key={node.note_id}>
          {/* text control */}
          <Link
            to="/notes/$noteId"
            params={{ noteId: node.note_id }}
            role="menuitem"
            title={node.title}
            onClick={onNavigate}
          >
            {node.title}
          </Link>
          {node.children?.length ? <HierarchyList nodes={node.children} onNavigate={onNavigate} /> : null}
        </li>
      ))}
    </ul>
  );
}

// An org chart: one node over two, connected. Outlines only, like every other rail glyph — it is the
// deliberate tree over the link graph, so it is drawn as ranks rather than as the graph's triangle.
function RailHierarchyIcon() {
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
      <rect x="9" y="3.5" width="6" height="4.5" rx="1" />
      <rect x="2.5" y="16" width="6" height="4.5" rx="1" />
      <rect x="15.5" y="16" width="6" height="4.5" rx="1" />
      <path d="M12 8v3.5" />
      <path d="M5.5 16v-4.5h13V16" />
    </svg>
  );
}
