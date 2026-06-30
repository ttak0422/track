import { useNavigate } from "@tanstack/react-router";
import { type MouseEvent, useEffect, useRef, type WheelEvent } from "react";
import type { NoteID } from "../../types";
import { tabRoute, useTabs } from "./tabsStore";

// TabBar is the VS Code-style strip of open notes above the reader. Tabs accumulate as notes are
// opened, scroll horizontally when they overflow, and each carries a hover-revealed close button (a
// dirty dot stands in for it while the note has unsaved edits).
export function TabBar() {
  const { tabs, activeID, dirtyID, close } = useTabs();
  const navigate = useNavigate();
  const stripRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef<HTMLDivElement>(null);

  // Keep the active tab in view when navigation (e.g. a backlink) selects an off-screen one.
  useEffect(() => {
    activeRef.current?.scrollIntoView({ inline: "nearest", block: "nearest" });
  }, [activeID]);

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
    <div className="tabbar" role="tablist" aria-label="Open notes" ref={stripRef} onWheel={onWheel}>
      {tabs.map((tab) => {
        const active = tab.id === activeID;
        const label = tab.title || "Untitled";
        return (
          <div
            key={tab.id}
            ref={active ? activeRef : undefined}
            className={`tab${active ? " active" : ""}${tab.id === dirtyID ? " dirty" : ""}`}
          >
            <button
              type="button"
              role="tab"
              aria-selected={active}
              className="tab-label"
              title={label}
              onClick={() => openTab(tab.id)}
              onAuxClick={(event) => onAuxClick(event, tab.id)}
            >
              <span className="tab-title">{label}</span>
            </button>
            <button
              type="button"
              className="tab-close"
              aria-label={`Close ${label}`}
              title="Close"
              onClick={() => close(tab.id)}
            >
              <span className="tab-close-glyph" aria-hidden="true">
                ×
              </span>
              <span className="tab-dirty-dot" aria-hidden="true" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
