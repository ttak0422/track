import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { isTypingTarget, keys } from "../keys";
import { SearchPanel } from "./SearchPanel";

// SidebarSearch is the rail's magnifier button plus the search palette it opens in the middle of the
// screen. The palette closes on Escape, on an outside click, and when a result is chosen; "/" opens
// it from anywhere, the way it opens a search in a pager or an editor.
export function SidebarSearch() {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function onOpenKey(event: KeyboardEvent) {
      if (open || !keys.openSearch(event) || isTypingTarget(event.target)) return;
      event.preventDefault();
      setOpen(true);
    }
    document.addEventListener("keydown", onOpenKey);
    return () => document.removeEventListener("keydown", onOpenKey);
  }, [open]);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: MouseEvent) {
      if (
        !containerRef.current?.contains(event.target as Node) &&
        !overlayRef.current?.contains(event.target as Node)
      ) {
        setOpen(false);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (keys.close(event)) setOpen(false);
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return (
    <div className="rail-search" ref={containerRef}>
      <button
        className="rail-button"
        type="button"
        aria-label="Search notes"
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Search notes (/)"
        onClick={() => setOpen((value) => !value)}
      >
        <SearchIcon />
      </button>
      {open
        ? createPortal(
            // The rail is a fixed stacking context; the palette must be a body sibling for its layer
            // to compete with previews, while the trigger remains in the rail.
            <div ref={overlayRef}>
              {/* The query is shared with the home hero's field, so without this the same result list is
                  also live behind the palette — two of them at once read as one broken one. Clicking it
                  closes, same as clicking anywhere else outside. */}
              <div className="search-backdrop" onMouseDown={() => setOpen(false)} />
              <div className="search-popup" role="dialog" aria-label="Search notes">
                <SearchPanel autoFocus onNavigate={() => setOpen(false)} />
              </div>
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

function SearchIcon() {
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
      <circle cx="10.75" cy="10.75" r="6.5" />
      <line x1="15.5" y1="15.5" x2="20.5" y2="20.5" />
    </svg>
  );
}
