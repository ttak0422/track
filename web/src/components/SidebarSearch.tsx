import { useEffect, useRef, useState } from "react";
import { SearchHome } from "./SearchHome";

// SidebarSearch is the rail's magnifier button plus the floating search popup it toggles open beside the
// rail. The popup closes on Escape, on an outside click, and when a result is chosen.
export function SidebarSearch() {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: MouseEvent) {
      if (!containerRef.current?.contains(event.target as Node)) {
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

  return (
    <div className="rail-search" ref={containerRef}>
      <button
        className="rail-button"
        type="button"
        aria-label="Search notes"
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Search notes"
        onClick={() => setOpen((value) => !value)}
      >
        <SearchIcon />
      </button>
      {open ? (
        <div className="search-popup home-hero" role="dialog" aria-label="Search notes">
          <SearchHome autoFocus onNavigate={() => setOpen(false)} />
        </div>
      ) : null}
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
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="11" cy="11" r="7" />
      <line x1="21" y1="21" x2="16.65" y2="16.65" />
    </svg>
  );
}
