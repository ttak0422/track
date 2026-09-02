import { createContext, type ReactNode, useContext, useEffect, useRef, useState } from "react";
import { elementAnchor, initialPreviewBounds } from "../preview/bounds";
import { InFloatingWindowContext, useFloating } from "../preview/floatingStore";
import { NoteKindContext, NoteVaultContext } from "./context";
import { IconMaximize, IconPictureInPicture, RailIcon } from "../icons";

// MediaFrame wraps a media embed (image, PDF) with hover-revealed controls: preview (an enlarged
// copy floating beside the media, the same FloatingWindow chrome a WikiLink note preview uses via
// MediaWindow) and enlarge (an in-window lightbox). Unlike a note link — whose target is hidden
// until previewed — the media is already fully visible, so the preview popup opens only from its
// button, never automatically on hover. Keeping the media in a persistent window is the preview's
// pin button (promote), not a separate control — one popup button, one path. Inside a floating
// window it renders the media bare, so a floated/previewed image offers neither control nor a
// nested preview of itself again.
// True inside the enlarge lightbox. The dialog sizes itself to its content, so lightbox children
// that fit themselves to their container (PdfDeck) must size from the viewport instead — measuring
// a content-sized container is circular.
export const InLightboxContext = createContext(false);

export function MediaFrame({ src, alt, children }: { src: string; alt: string; children: ReactNode }) {
  const inFloating = useContext(InFloatingWindowContext);
  const kind = useContext(NoteKindContext);
  const vault = useContext(NoteVaultContext);
  const floating = useFloating();
  const ref = useRef<HTMLDivElement>(null);

  // The preview this frame opened, so enlarging can take it down again. The window itself belongs to
  // the floating layer.
  const openedRef = useRef<string | null>(null);
  const [enlarged, setEnlarged] = useState(false);
  const dialogRef = useRef<HTMLDialogElement>(null);

  // The lightbox <dialog> mounts only while enlarged; showModal() must run after that mount, so it
  // lives in an effect rather than the click handler.
  useEffect(() => {
    if (enlarged) dialogRef.current?.showModal();
  }, [enlarged]);

  if (inFloating) {
    return <>{children}</>;
  }

  // The preview was asked for by a click, so it opens settled: it stays until its close button (or
  // enlarging the media) dismisses it, rather than evaporating when the pointer wanders off. Its pin
  // button is what makes it persist across navigation, same as any other window in the layer.
  function openPreview() {
    openedRef.current = floating.open(
      { kind: "media", src, alt, noteKind: kind, vault },
      initialPreviewBounds(elementAnchor(ref.current)),
      false,
    );
  }

  function closePreview() {
    if (openedRef.current) floating.remove(openedRef.current);
    openedRef.current = null;
  }

  return (
    <div className="media-frame" ref={ref}>
      {children}
      <div className="media-controls">
        <button
          className="media-control media-preview"
          type="button"
          onClick={() => {
            if (enlarged) return;
            openPreview();
          }}
          aria-label="Preview"
          title="Preview"
        >
          {/* Picture-in-picture glyph (tabler picture-in-picture): pop an enlarged copy up beside the
              media, on demand rather than on hover. */}
          <RailIcon Icon={IconPictureInPicture} size={15} />
        </button>
        <button
          className="media-control"
          type="button"
          onClick={() => {
            // Drop any open preview popup rather than leaving it behind the modal.
            closePreview();
            setEnlarged(true);
          }}
          aria-label="Enlarge"
          title="Enlarge"
        >
          {/* Expand-to-corners glyph (tabler maximize): enlarge in an in-window lightbox (a modal
              <dialog> over a dimmed backdrop), not display fullscreen. */}
          <RailIcon Icon={IconMaximize} size={15} />
        </button>
      </div>
      {enlarged ? (
        <dialog
          ref={dialogRef}
          className="media-lightbox"
          onClose={() => setEnlarged(false)}
          onClick={(event) => {
            // A backdrop click lands on the dialog element itself (content clicks land on children).
            if (event.target === dialogRef.current) dialogRef.current.close();
          }}
        >
          <InLightboxContext.Provider value={true}>{children}</InLightboxContext.Provider>
        </dialog>
      ) : null}
    </div>
  );
}
