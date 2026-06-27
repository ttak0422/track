import { useFloating } from "./floatingStore";
import { MediaWindow } from "./MediaWindow";
import { NoteWindow } from "./NoteWindow";

// FloatingLayer renders the pinned floating windows. It lives in Shell, above the router Outlet, so its
// windows persist across note navigation until closed.
export function FloatingLayer() {
  const { windows, setPinned, remove, bringToFront } = useFloating();

  return (
    <>
      {windows.map((win) => {
        const controls = {
          initialBounds: win.initialBounds,
          initialCollapsed: win.initialCollapsed,
          pinned: win.pinned,
          depth: 0,
          stackOrder: win.stackOrder,
          onActivate: () => bringToFront(win.id),
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
            {...controls}
          />
        );
      })}
    </>
  );
}
