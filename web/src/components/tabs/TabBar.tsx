import { useNavigate } from "@tanstack/react-router";
import { type MouseEvent, useEffect, useRef, type WheelEvent } from "react";
import type { NoteID } from "../../types";
import { initialPreviewBounds } from "../preview/bounds";
import { useFloating } from "../preview/floatingStore";
import { isViewTab, tabRoute, useTabs } from "./tabsStore";

// TabBar is the VS Code-style strip of open notes above the reader. Tabs accumulate as notes are
// opened, scroll horizontally when they overflow, and each carries hover-revealed buttons: a close
// button (a dirty dot stands in for it while the note has unsaved edits), and on the open note a
// button that floats it.
export function TabBar() {
  const { tabs, activeID, dirtyID, close } = useTabs();
  const navigate = useNavigate();
  const stripRef = useRef<HTMLDivElement>(null);
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

  return (
    <div className="tabbar" role="list" aria-label="Open notes" ref={stripRef} onWheel={onWheel}>
      {tabs.map((tab) => {
        const active = tab.id === activeID;
        const label = tab.title || "Untitled";
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
              title={label}
              onClick={() => openTab(tab.id)}
              onAuxClick={(event) => onAuxClick(event, tab.id)}
            >
              <span className="tab-title">{label}</span>
            </button>
            {active && !isViewTab(tab.id) ? <FloatButton noteID={tab.id} /> : null}
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
