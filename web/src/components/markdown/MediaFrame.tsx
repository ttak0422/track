import { createContext, type ReactNode, useContext, useEffect, useRef, useState } from "react";
import { initialPreviewBounds, type PreviewAnchor, type PreviewBounds } from "../preview/bounds";
import { InFloatingWindowContext, useFloating } from "../preview/floatingStore";
import { MediaWindow } from "../preview/MediaWindow";
import { nextPreviewStackOrder } from "../preview/stack";
import { NoteKindContext } from "./context";

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
  const floating = useFloating();
  const ref = useRef<HTMLDivElement>(null);

  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<PreviewAnchor | null>(null);
  const [stackOrder, setStackOrder] = useState(nextPreviewStackOrder);
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

  function frameAnchor(): PreviewAnchor {
    const rect = ref.current?.getBoundingClientRect();
    return rect
      ? { linkLeft: rect.left, linkRight: rect.right, linkTop: rect.top, linkBottom: rect.bottom }
      : { linkLeft: 0, linkRight: 0, linkTop: 0, linkBottom: 0 };
  }

  // The preview was asked for by a click, so it stays until its close button (or enlarging the
  // media) dismisses it, rather than evaporating when the pointer wanders off.
  function openPreview() {
    setStackOrder(nextPreviewStackOrder());
    setAnchor(frameAnchor());
    setOpen(true);
  }

  function closePreview() {
    setOpen(false);
  }

  // Pin promotes the preview popup into the persistent floating layer at its current position/size,
  // same as WikiLink promoting a note preview.
  function promote(bounds: PreviewBounds, collapsed: boolean) {
    floating.open({ kind: "media", src, alt, noteKind: kind }, bounds, collapsed, true);
    setOpen(false);
  }

  return (
    <div className="media-frame" ref={ref}>
      {children}
      <div className="media-controls">
        <button
          className="media-control"
          type="button"
          onClick={() => {
            if (enlarged) return;
            openPreview();
          }}
          aria-label="Preview"
          title="Preview"
        >
          {/* Picture-in-picture glyph: pop an enlarged copy up beside the media, on demand rather
              than on hover. The frame-with-inner-window shape (preferred over the eye it briefly
              was) reads as "opens a window". */}
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
          {/* Expand-to-corners glyph: enlarge in an in-window lightbox (a modal <dialog> over a dimmed
              backdrop), not display fullscreen. */}
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
            <path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3M3 16v3a2 2 0 0 0 2 2h3m8 0h3a2 2 0 0 0 2-2v-3" />
          </svg>
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
      {open && anchor ? (
        <MediaWindow
          src={src}
          alt={alt}
          kind={kind}
          initialBounds={initialPreviewBounds(anchor)}
          pinned={false}
          depth={0}
          stackOrder={stackOrder}
          onActivate={() => setStackOrder(nextPreviewStackOrder())}
          onClose={closePreview}
          onPinToggle={promote}
        />
      ) : null}
    </div>
  );
}
