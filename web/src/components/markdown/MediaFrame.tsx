import { createContext, type ReactNode, useContext, useEffect, useRef, useState } from "react";
import { initialPreviewBounds, type PreviewAnchor, type PreviewBounds } from "../preview/bounds";
import { InFloatingWindowContext, useFloating } from "../preview/floatingStore";
import { MediaWindow } from "../preview/MediaWindow";
import {
  activatePreview,
  bringPreviewToFront,
  createPreviewID,
  deactivatePreview,
  releasePreview,
  usePreviewStackOrder,
} from "../preview/stack";
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

  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<PreviewAnchor | null>(null);
  const [previewID] = useState(createPreviewID);
  const stackOrder = usePreviewStackOrder(previewID);
  const [enlarged, setEnlarged] = useState(false);
  const dialogRef = useRef<HTMLDialogElement>(null);

  // The lightbox <dialog> mounts only while enlarged; showModal() must run after that mount, so it
  // lives in an effect rather than the click handler.
  useEffect(() => {
    if (enlarged) dialogRef.current?.showModal();
  }, [enlarged]);

  useEffect(() => () => releasePreview(previewID), [previewID]);

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
    activatePreview(previewID);
    setAnchor(frameAnchor());
    setOpen(true);
  }

  function closePreview() {
    deactivatePreview(previewID);
    setOpen(false);
  }

  // Pin promotes the preview popup into the persistent floating layer at its current position/size,
  // same as WikiLink promoting a note preview.
  function promote(bounds: PreviewBounds, collapsed: boolean) {
    floating.open({ kind: "media", src, alt, noteKind: kind, vault }, bounds, collapsed, true);
    deactivatePreview(previewID);
    setOpen(false);
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
      {open && anchor ? (
        <MediaWindow
          src={src}
          alt={alt}
          kind={kind}
          vault={vault}
          initialBounds={initialPreviewBounds(anchor)}
          pinned={false}
          depth={0}
          stackOrder={stackOrder}
          onActivate={() => bringPreviewToFront(previewID)}
          onClose={closePreview}
          onPinToggle={promote}
        />
      ) : null}
    </div>
  );
}
