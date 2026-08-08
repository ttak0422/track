import { useNavigate } from "@tanstack/react-router";
import { type MouseEvent, type PointerEvent, useEffect, useRef, useState, type WheelEvent } from "react";
import type { NoteID } from "../../types";
import { vaultOf } from "../../vaultId";
import { initialPreviewBounds } from "../preview/bounds";
import { useFloating } from "../preview/floatingStore";
import { isViewTab, type NoteTab, tabRoute, useTabs } from "./tabsStore";

// The strip shows this many titles and sends the rest to the +N menu (design.md, Tab strip). Past
// four, every title truncates to a date prefix and the strip stops telling you what is open.
const SHOWN = 4;

// TabBar is the VS Code-style strip of open notes above the reader. Tabs accumulate as notes are
// opened, the four newest-in-order stay on the strip while the rest wait behind the +N button, and
// each carries hover-revealed buttons: a close button (a dirty dot stands in for it while the note
// has unsaved edits), and on the open note a button that floats it.
export function TabBar() {
  const { tabs, activeID, dirtyID, close } = useTabs();
  const navigate = useNavigate();
  const stripRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ pointerId: number; x: number; left: number; moved: boolean } | null>(null);
  // Set by a pan that moved, and consumed by the click it produces.
  const draggedRef = useRef(false);
  const activeRef = useRef<HTMLDivElement>(null);

  // Keep the active tab in view when navigation (e.g. a backlink) selects an off-screen one. Also
  // re-run when the tab count changes: opening a note with no tab yet appends one in a separate effect
  // (tabsStore's), so on the render where activeID first changes the new tab isn't in `tabs` yet and
  // activeRef is still unattached — the length dependency catches the follow-up render where it is.
  // Depending on `tabs.length` rather than `tabs` itself avoids re-scrolling on unrelated updates (e.g.
  // a title resolving) that produce a new array without changing the count.
  useEffect(() => {
    activeRef.current?.scrollIntoView({ inline: "nearest", block: "nearest" });
  }, [activeID, tabs.length]);

  if (tabs.length === 0) return null;

  // Translate vertical wheel into horizontal scroll so a plain mouse can reach overflowed tabs.
  function onWheel(event: WheelEvent<HTMLDivElement>) {
    const strip = stripRef.current;
    if (!strip || event.deltaY === 0) return;
    strip.scrollLeft += event.deltaY;
  }

  // Dragging the strip pans it, the second way to reach an overflowed tab with a plain mouse (the
  // wheel above is the first). Touch already pans natively, so this is mouse-only. A drag that
  // actually moved swallows the click that ends it, or letting go over a tab would open it.
  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    const strip = stripRef.current;
    if (!strip || event.pointerType !== "mouse" || event.button !== 0) return;
    // Cleared here rather than only in the click it suppresses: a pan that ends over something other
    // than a tab produces no click, and a stale flag would swallow the next real one.
    draggedRef.current = false;
    dragRef.current = { pointerId: event.pointerId, x: event.clientX, left: strip.scrollLeft, moved: false };
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    const drag = dragRef.current;
    const strip = stripRef.current;
    if (!drag || !strip || drag.pointerId !== event.pointerId) return;
    const dx = event.clientX - drag.x;
    if (!drag.moved) {
      if (Math.abs(dx) <= 4) return; // a click has a little travel in it; that is not a drag
      drag.moved = true;
      event.currentTarget.setPointerCapture(event.pointerId);
      strip.classList.add("dragging");
    }
    strip.scrollLeft = drag.left - dx;
  }

  function endDrag(event: PointerEvent<HTMLDivElement>) {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    dragRef.current = null;
    stripRef.current?.classList.remove("dragging");
    if (drag.moved) {
      event.currentTarget.releasePointerCapture?.(event.pointerId);
      draggedRef.current = true;
    }
  }

  // The click that ends a pan is not a request to open — or close — anything, so it is swallowed
  // here, on the strip, in the capture phase. It cannot be swallowed by the tab's own handler: the
  // pan holds pointer capture, which retargets that click to the strip, so a handler on the tab
  // never runs — and so never cleared the flag either, which then sat there until some later
  // activation with no pointer of its own, a keyboard Enter, was swallowed in its place.
  function onClickCapture(event: MouseEvent<HTMLDivElement>) {
    if (!draggedRef.current) return;
    draggedRef.current = false;
    event.stopPropagation();
  }

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

  // The strip always holds the open note: it takes the last slot when it would otherwise have been
  // pushed into the overflow, so "what you are reading" never hides behind a menu.
  const head = tabs.slice(0, SHOWN);
  const shown =
    tabs.length <= SHOWN || head.some((tab) => tab.id === activeID)
      ? head
      : [...tabs.slice(0, SHOWN - 1), ...tabs.filter((tab) => tab.id === activeID)];
  const hidden = tabs.filter((tab) => !shown.includes(tab));

  return (
    <div className="tabstrip">
      <div
        className="tabbar"
        role="list"
        aria-label="Open notes"
        ref={stripRef}
        onWheel={onWheel}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onClickCapture={onClickCapture}
      >
        {shown.map((tab) => {
          const active = tab.id === activeID;
          const label = tab.title || "Untitled";
          // Two vaults can hold notes with the same title (and the same id), so a tab from a named
          // vault says which one it is instead of leaving two identical-looking tabs side by side.
          const vault = vaultOf(tab.id);
          return (
            <div
              key={tab.id}
              ref={active ? activeRef : undefined}
              role="listitem"
              className={`tab${active ? " active" : ""}${tab.id === dirtyID ? " dirty" : ""}`}
            >
              <button
                type="button"
                aria-current={active ? "page" : undefined}
                className="tab-label"
                title={vault ? `${label} — ${vault}` : label}
                onClick={() => openTab(tab.id)}
                onAuxClick={(event) => onAuxClick(event, tab.id)}
              >
                {vault ? <span className="tab-vault">{vault}</span> : null}
                <span className="tab-title">{label}</span>
              </button>
              {!isViewTab(tab.id) ? <FloatButton noteID={tab.id} /> : null}
              <button
                type="button"
                className="tab-close"
                aria-label={`Close ${label}`}
                title="Close"
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
                <span className="tab-dirty-dot" aria-hidden="true" />
              </button>
            </div>
          );
        })}
      </div>
      {hidden.length > 0 ? <TabOverflow tabs={hidden} onOpen={openTab} /> : null}
    </div>
  );
}

// TabOverflow lists the open notes the strip has no room for. Opening one puts it in the strip's last
// slot (it becomes the active tab), so the menu is a way back to a note, not a second tab bar.
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
// away), anchored to the button. It sits inside the tab beside the close button: it used to live in a
// popup hanging under the tab, which had to be placed in JS against a strip that scrolls and reflows and
// which the pointer had to cross a gap to reach, so it drifted or vanished as often as it worked.
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
      title="Float this note"
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
