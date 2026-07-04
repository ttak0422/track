import { type CSSProperties, type RefObject, useLayoutEffect, useRef, useState } from "react";
import { initialPreviewBounds } from "../preview/bounds";
import { useFloating } from "../preview/floatingStore";
import type { NoteID } from "../../types";

interface TabActionsProps {
  noteID: NoteID;
  // The active tab element, used to place this popup just below it.
  tabRef: RefObject<HTMLDivElement | null>;
}

// TabActions is the hover popup under the active tab: a row of actions on the currently open note. It is
// position:fixed (measured from the tab) so the tab strip's horizontal-scroll overflow does not clip it,
// while staying a DOM child of the tab so the tab's :hover keeps it open. New actions are added to the
// list below.
export function TabActions({ noteID, tabRef }: TabActionsProps) {
  const [style, setStyle] = useState<CSSProperties>({ visibility: "hidden" });

  // Track the tab's position (it moves as the strip scrolls or the window resizes) so the fixed popup
  // stays anchored under it.
  useLayoutEffect(() => {
    function place() {
      const rect = tabRef.current?.getBoundingClientRect();
      if (rect) setStyle({ left: rect.left, top: rect.bottom });
    }
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [tabRef]);

  return (
    <div className="tab-actions" style={style} role="group" aria-label="Tab actions">
      <FloatAction noteID={noteID} />
    </div>
  );
}

// FloatAction pops the current note into the persistent floating layer (pinned so it survives navigating
// away), anchored to the button.
function FloatAction({ noteID }: { noteID: NoteID }) {
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
      className="tab-action"
      type="button"
      onClick={float}
      aria-label="Float this note"
      title="Float this note"
    >
      <svg
        viewBox="0 0 24 24"
        width="15"
        height="15"
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
