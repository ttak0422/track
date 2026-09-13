import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useFloating } from "./floatingStore";
import { MediaWindow } from "./MediaWindow";
import { NoteWindow } from "./NoteWindow";
import { getPreviewStackOrder, usePreviewStackVersion } from "./stack";

// FloatingLayer renders every floating window there is — the hover previews as well as the kept and
// pinned ones. It lives in Shell, above the router Outlet, so the layer is one stacking context: a
// window opened from inside another window is its sibling here, free to come to the front and to
// outlive the one it was opened from. Order is the stack's alone (see stack.ts): whatever was
// activated last is in front, whoever opened it.
export function FloatingLayer() {
  const { windows, setPinned, remove, replace, bringToFront, hold, scheduleClose, settle } = useFloating();
  usePreviewStackVersion();
  const navigate = useNavigate();
  // The note the page currently shows, when it shows one. The swap button trades a note window with
  // it, so on a route with no note to trade with (search, graph, voice) the button stays absent.
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const currentNoteID = /^\/notes\/([^/]+)$/.exec(pathname.replace(/\/$/, ""))?.[1] ?? null;

  // The popup↔page trade: the window's note becomes the page and the page's note takes the window's
  // place. replace() keeps the window's id, bounds, and stack slot and pins it, so the navigation
  // that follows keeps it; the unsaved-edits guard the editor already mounts intercepts the navigate
  // when the page note is dirty.
  function swapNote(winID: string, popupNoteID: string) {
    if (currentNoteID === null || currentNoteID === popupNoteID) return;
    replace(winID, { kind: "note", noteID: currentNoteID }, { pinned: true });
    void navigate({ to: "/notes/$noteId", params: { noteId: String(popupNoteID) } });
  }

  return (
    <>
      {windows.map((win) => {
        const controls = {
          initialBounds: win.initialBounds,
          // Only a window still owned by the pointer follows its opener; a settled one stays put.
          reanchor: win.transient ? win.anchor : undefined,
          initialCollapsed: win.initialCollapsed,
          pinned: win.pinned,
          stackOrder: getPreviewStackOrder(win.id),
          onActivate: () => bringToFront(win.id),
          onHold: win.transient ? () => hold(win.id) : undefined,
          onLeave: win.transient ? () => scheduleClose(win.id) : undefined,
          onDetach: win.transient ? () => settle(win.id) : undefined,
          onClose: () => remove(win.id),
          // The pin button toggles persistence (it does not close the window); × closes.
          onPinToggle: () => setPinned(win.id, !win.pinned),
        };
        if (win.content.kind === "note") {
          const popupNoteID = win.content.noteID;
          return (
            <NoteWindow
              key={win.id}
              noteID={popupNoteID}
              {...controls}
              // A note window swaps with the page note only when there is a different one to trade
              // with; swapping a note with itself would just re-open the page in a window.
              onSwap={
                currentNoteID !== null && currentNoteID !== popupNoteID
                  ? () => swapNote(win.id, popupNoteID)
                  : undefined
              }
            />
          );
        }
        return (
          <MediaWindow
            key={win.id}
            src={win.content.src}
            alt={win.content.alt}
            kind={win.content.noteKind}
            vault={win.content.vault}
            {...controls}
          />
        );
      })}
    </>
  );
}
