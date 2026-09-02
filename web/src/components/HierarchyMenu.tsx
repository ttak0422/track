import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { useHierarchyQuery } from "../queries";
import { hoverOpen } from "./hoverOpen";
import { railAnchor } from "./railAnchor";
import { IconSitemap, RailIcon } from "./icons";
import type { HierarchyNode } from "../types";

// The rail's hierarchy button: the vault's "up" tree, opened by hovering the glyph the way the
// display-mode menu is. The tree is built ahead of the browser — a prerendered file on the published
// site, one indexed query on the live server — and fetched only once this menu is first opened, so a
// reader who never consults it pays nothing for it. Notes the hierarchy does not place are absent,
// which on a vault using "up" for a handful of notes keeps the menu the size of that handful.
export function HierarchyMenu() {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<CSSProperties | undefined>(undefined);
  const menuRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);
  // Once opened the query stays enabled: a bundle never changes under the page, and the live server's
  // is one query behind a 30s stale window, so a second hover redraws from cache.
  const [asked, setAsked] = useState(false);
  const { data } = useHierarchyQuery(asked);
  const roots = data?.hierarchy ?? [];
  const [expanded, setExpanded] = useState<Set<string>>(storedExpanded);

  // Unfolding is written through on the click rather than in an effect: the set is what the reader
  // last left the tree looking like, and it should survive a reload, not just this menu being closed.
  function toggleBranch(noteID: string) {
    const next = new Set(expanded);
    if (!next.delete(noteID)) next.add(noteID);
    setExpanded(next);
    localStorage.setItem(expandedKey, JSON.stringify([...next]));
  }

  function cancelClose() {
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
  }

  function showMenu() {
    cancelClose();
    setAnchor(railAnchor(toggleRef.current));
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
    <div className="hierarchy-menu" ref={menuRef} {...hoverOpen(showMenu, scheduleClose)}>
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
        <RailIcon Icon={IconSitemap} />
      </button>
      {open ? (
        // The title stays outside the menu and outside the scroller: a heading is not a menu item,
        // and a panel that scrolls would carry its own name off the top.
        <div className="menu-panel note-menu-panel hierarchy-panel" style={anchor}>
          <h2 className="rail-panel-title">Hierarchy</h2>
          <div className="hierarchy-scroll" role="menu" aria-label="Hierarchy">
            {roots.length === 0 ? (
              <p className="hierarchy-empty">No hierarchy</p>
            ) : (
              <HierarchyList
                nodes={roots}
                root
                expanded={expanded}
                onToggle={toggleBranch}
                onNavigate={() => setOpen(false)}
              />
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}

// root marks the top level, whose branches are always open: a root only exists because something
// sits under it, and folding away every subject at once would leave a menu of nothing. So the roots
// carry no caret, and the caret column is not held open beside them either.
function HierarchyList({
  nodes,
  root = false,
  expanded,
  onToggle,
  onNavigate,
}: {
  nodes: HierarchyNode[];
  root?: boolean;
  expanded: Set<string>;
  onToggle: (noteID: string) => void;
  onNavigate: () => void;
}) {
  return (
    <ul className="hierarchy-list">
      {nodes.map((node) => {
        const children = node.children ?? [];
        const folded = !root && !expanded.has(node.note_id);
        return (
          <li key={node.note_id}>
            <div className="hierarchy-row">
              {/* Below the roots the caret column is held open on leaves too, so titles at one level
                  line up whether or not the branch beside them can be folded. */}
              {root ? null : children.length > 0 ? (
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
                <span className="hierarchy-toggle-placeholder" aria-hidden="true" />
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
                expanded={expanded}
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

// Which branches the reader unfolded, by note id. A tree opens showing its roots and their children
// and nothing deeper, so the menu is the vault's subjects rather than everything the hierarchy holds;
// only what the reader opened is stored, and stays open. Ids that outlive their note are inert set
// members, not a reason to prune on read.
const expandedKey = "track.hierarchyExpanded";

function storedExpanded(): Set<string> {
  // There is no localStorage during the prerender, and the menu is closed in that output anyway.
  if (typeof window === "undefined") return new Set();
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(expandedKey) ?? "[]");
    return new Set(Array.isArray(stored) ? stored.filter((id) => typeof id === "string") : []);
  } catch {
    return new Set();
  }
}
