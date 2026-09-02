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
  const { windows, setPinned, remove, bringToFront, hold, scheduleClose, settle } = useFloating();
  usePreviewStackVersion();

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
        return win.content.kind === "note" ? (
          <NoteWindow key={win.id} noteID={win.content.noteID} {...controls} />
        ) : (
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
