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
  const [collapsed, setCollapsed] = useState<Set<string>>(storedCollapsed);

  // Folding is written through on the click rather than in an effect: the set is what the reader last
  // left the tree looking like, and it should survive a reload, not just this menu being closed.
  function toggleBranch(noteID: string) {
    const next = new Set(collapsed);
    if (!next.delete(noteID)) next.add(noteID);
    setCollapsed(next);
    localStorage.setItem(collapsedKey, JSON.stringify([...next]));
  }

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
            <HierarchyList
              nodes={roots}
              collapsed={collapsed}
              onToggle={toggleBranch}
              onNavigate={() => setOpen(false)}
            />
          )}
        </div>
      ) : null}
    </div>
  );
}

function HierarchyList({
  nodes,
  collapsed,
  onToggle,
  onNavigate,
}: {
  nodes: HierarchyNode[];
  collapsed: Set<string>;
  onToggle: (noteID: string) => void;
  onNavigate: () => void;
}) {
  return (
    <ul className="hierarchy-list">
      {nodes.map((node) => {
        const children = node.children ?? [];
        const folded = collapsed.has(node.note_id);
        return (
          <li key={node.note_id}>
            <div className="hierarchy-row">
              {/* The caret column is held open on leaves too, so titles at one level line up whether
                  or not the branch beside them can be folded. */}
              {children.length > 0 ? (
                /* icon button */
                <button
                  className="hierarchy-toggle"
                  type="button"
                  role="menuitem"
                  aria-expanded={!folded}
                  aria-label={`${folded ? "Expand" : "Collapse"} ${node.title}`}
                  onClick={() => onToggle(node.note_id)}
                >
                  <span className="hierarchy-caret" aria-hidden="true" />
                </button>
              ) : (
                <span className="hierarchy-toggle" aria-hidden="true" />
              )}
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
            </div>
            {children.length > 0 && !folded ? (
              <HierarchyList
                nodes={children}
                collapsed={collapsed}
                onToggle={onToggle}
                onNavigate={onNavigate}
              />
            ) : null}
          </li>
        );
      })}
    </ul>
  );
}

// Which branches the reader folded, by note id. Only the folds are stored: a tree opens fully
// expanded, so a note that appears later is visible without anyone having to know about it. Ids that
// outlive their note are inert set members, not a reason to prune on read.
const collapsedKey = "track.hierarchyCollapsed";

function storedCollapsed(): Set<string> {
  // There is no localStorage during the prerender, and the menu is closed in that output anyway.
  if (typeof window === "undefined") return new Set();
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(collapsedKey) ?? "[]");
    return new Set(Array.isArray(stored) ? stored.filter((id) => typeof id === "string") : []);
  } catch {
    return new Set();
  }
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
