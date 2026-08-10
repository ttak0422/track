import { useNavigate } from "@tanstack/react-router";
import { type MouseEvent, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { NoteID } from "../../types";
import { vaultOf } from "../../vaultId";
import { initialPreviewBounds } from "../preview/bounds";
import { useFloating } from "../preview/floatingStore";
import { isViewTab, type NoteTab, tabRoute, useTabs } from "./tabsStore";

// TabBar is the strip of open notes above the reader, most recent first (tabsStore keeps that order),
// so the note being read is always the leftmost tab and never in the overflow. The strip shows every
// tab that fits — the count is measured, not fixed — and sends the rest to the +N menu at its right
// end rather than scrolling sideways. Each tab's controls (float, close) hang under it on hover; only
// the unsaved-changes dot stays inline.
export function TabBar() {
  const { tabs, activeID, dirtyID, close } = useTabs();
  const navigate = useNavigate();
  const stripRef = useRef<HTMLDivElement>(null);
  // How many tabs the strip has room for, found by measuring: grow while the row fits, shrink while
  // it overflows. The two cannot chase each other, because a shrink caps the count until the geometry
  // it was measured against changes.
  const [shown, setShown] = useState(1);
  const capRef = useRef(Number.POSITIVE_INFINITY);
  const [width, setWidth] = useState(0);

  useEffect(() => {
    const strip = stripRef.current;
    if (!strip || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(strip);
    return () => observer.disconnect();
  }, []);

  // The cap belongs to one geometry: a resize, or a change to the open set, retries from scratch.
  useLayoutEffect(() => {
    capRef.current = Number.POSITIVE_INFINITY;
  }, [width, tabs.length]);

  useLayoutEffect(() => {
    const strip = stripRef.current;
    if (!strip) return;
    // The +1 absorbs sub-pixel widths, which would otherwise read as a permanent overflow.
    if (strip.scrollWidth > strip.clientWidth + 1) {
      if (shown > 1) {
        capRef.current = shown - 1;
        setShown(shown - 1);
      }
    } else if (shown < tabs.length && shown < capRef.current) {
      setShown(shown + 1);
    }
  });

  if (tabs.length === 0) return null;

  function openTab(id: NoteID) {
    void navigate(tabRoute(id));
  }

  // Middle-click closes, matching editor conventions.
  function onAuxClick(event: MouseEvent<HTMLButtonElement>, id: NoteID) {
    if (event.button === 1) {
      event.preventDefault();
      close(id);
    }
  }

  const visible = tabs.slice(0, shown);
  const hidden = tabs.slice(shown);

  return (
    <div className="tabstrip">
      <div className="tabbar" role="list" aria-label="Open notes" ref={stripRef}>
        {visible.map((tab) => {
          const active = tab.id === activeID;
          const label = tab.title || "Untitled";
          // Two vaults can hold notes with the same title (and the same id), so a tab from a named
          // vault says which one it is instead of leaving two identical-looking tabs side by side.
          const vault = vaultOf(tab.id);
          return (
            <div
              key={tab.id}
              role="listitem"
              className={`tab${active ? " active" : ""}${tab.id === dirtyID ? " dirty" : ""}`}
            >
              <button
                type="button"
                aria-current={active ? "page" : undefined}
                className="tab-label"
                onClick={() => openTab(tab.id)}
                onAuxClick={(event) => onAuxClick(event, tab.id)}
              >
                {vault ? <span className="tab-vault">{vault}</span> : null}
                <span className="tab-title">{label}</span>
              </button>
              {/* Close rides the tab's own right end so a run of tabs can be dismissed in place: from
                  the popup, every one cost a trip down and back. It overlays the title's tail on
                  hover rather than holding a slot, which would pad every tab and lengthen the strip.
                  The unsaved-changes dot sits in that same place until then. */}
              <button
                type="button"
                className="tab-close"
                aria-label={`Close ${label}`}
                onClick={() => close(tab.id)}
              >
                <svg
                  className="tab-close-glyph"
                  viewBox="0 0 24 24"
                  width="14"
                  height="14"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  aria-hidden="true"
                >
                  <line x1="6" y1="6" x2="18" y2="18" />
                  <line x1="18" y1="6" x2="6" y2="18" />
                </svg>
                {tab.id === dirtyID ? <span className="tab-dirty-dot" aria-hidden="true" /> : null}
              </button>
              {/* The tab's own popup: the full title (the tab itself only has room for its head) and
                  the float control. The title used to come from the browser's own tooltip, which
                  opened at the pointer and landed on top of that button. */}
              <div className="tab-tools">
                <span className="tab-tools-title">
                  {vault ? <span className="tab-vault">{vault}</span> : null}
                  {label}
                </span>
                <div className="tab-tools-actions">
                  {!isViewTab(tab.id) ? <FloatButton noteID={tab.id} /> : null}
                </div>
              </div>
            </div>
          );
        })}
      </div>
      {hidden.length > 0 ? <TabOverflow tabs={hidden} onOpen={openTab} /> : null}
    </div>
  );
}

// TabOverflow lists the open notes the strip had no room for. Opening one makes it the active note,
// which puts it at the front of the strip — the menu is a way back to a note, not a second tab bar.
function TabOverflow({ tabs, onOpen }: { tabs: NoteTab[]; onOpen: (id: NoteID) => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: globalThis.MouseEvent) {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
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
    <div className="tab-overflow" ref={ref}>
      <button
        className="tab-overflow-toggle"
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`${tabs.length} more open notes`}
        title={`${tabs.length} more open notes`}
        onClick={() => setOpen((value) => !value)}
      >
        +{tabs.length}
        <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
          <polyline points="6,9 12,15 18,9" />
        </svg>
      </button>
      {open ? (
        <div className="tab-overflow-panel" role="menu">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              type="button"
              role="menuitem"
              title={tab.title || "Untitled"}
              onClick={() => {
                setOpen(false);
                onOpen(tab.id);
              }}
            >
              {tab.title || "Untitled"}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

// FloatButton pops the tab's note into the persistent floating layer (pinned, so it survives navigating
// away), anchored to the button.
function FloatButton({ noteID }: { noteID: NoteID }) {
  const floating = useFloating();
  const ref = useRef<HTMLButtonElement>(null);

  function float() {
    const rect = ref.current?.getBoundingClientRect();
    const anchor = rect
      ? { linkLeft: rect.left, linkRight: rect.right, linkTop: rect.top, linkBottom: rect.bottom }
      : { linkLeft: 0, linkRight: 0, linkTop: 0, linkBottom: 0 };
    floating.open({ kind: "note", noteID }, initialPreviewBounds(anchor), false, true);
  }

  return (
    <button
      ref={ref}
      type="button"
      className="tab-float"
      aria-label="Float this note"
      onClick={float}
    >
      <svg
        viewBox="0 0 24 24"
        width="14"
        height="14"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <rect x="3" y="5" width="18" height="14" rx="2" />
        <rect x="12" y="11" width="7" height="6" rx="1" fill="currentColor" stroke="none" />
      </svg>
    </button>
  );
}
