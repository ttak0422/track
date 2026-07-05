import { type ReactNode, useContext, useEffect, useRef, useState } from "react";
import { initialPreviewBounds, type PreviewAnchor, type PreviewBounds } from "../preview/bounds";
import { InFloatingWindowContext, useFloating } from "../preview/floatingStore";
import { MediaWindow } from "../preview/MediaWindow";
import { nextPreviewStackOrder, previewOpenDelay } from "../preview/stack";
import { NoteKindContext } from "./context";

// MediaFrame wraps a media embed (image, PDF) so hovering it shows the same hover-preview popup a
// WikiLink gives a note link: rest the pointer and an enlarged copy floats up beside it (mirrors
// WikiLink.tsx's open-delay/sticky-on-drag logic, reusing the same FloatingWindow chrome via
// MediaWindow). Hovering also still reveals the fullscreen/float controls. Inside a floating window it
// renders the media bare, so a floated/previewed image offers neither control nor a nested preview of
// itself again.
export function MediaFrame({ src, alt, children }: { src: string; alt: string; children: ReactNode }) {
  const inFloating = useContext(InFloatingWindowContext);
  const kind = useContext(NoteKindContext);
  const floating = useFloating();
  const ref = useRef<HTMLDivElement>(null);

  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<PreviewAnchor | null>(null);
  const [sticky, setSticky] = useState(false);
  const [stackOrder, setStackOrder] = useState(nextPreviewStackOrder);
  const closeTimer = useRef<number | undefined>(undefined);
  const openTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    return () => {
      if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
      if (openTimer.current !== undefined) window.clearTimeout(openTimer.current);
    };
  }, []);

  if (inFloating) {
    return <>{children}</>;
  }

  function frameAnchor(): PreviewAnchor {
    const rect = ref.current?.getBoundingClientRect();
    return rect
      ? { linkLeft: rect.left, linkRight: rect.right, linkTop: rect.top, linkBottom: rect.bottom }
      : { linkLeft: 0, linkRight: 0, linkTop: 0, linkBottom: 0 };
  }

  // scheduleOpen defers opening the preview until the pointer has rested on the media, so a cursor
  // sweeping down a note full of images does not flash a popup under each one (same intent as WikiLink).
  function scheduleOpen() {
    holdPreview();
    if (open || openTimer.current !== undefined) return;
    openTimer.current = window.setTimeout(() => {
      openTimer.current = undefined;
      openPreview();
    }, previewOpenDelay);
  }

  function cancelOpen() {
    if (openTimer.current !== undefined) {
      window.clearTimeout(openTimer.current);
      openTimer.current = undefined;
    }
  }

  function openPreview() {
    holdPreview();
    cancelOpen();
    setStackOrder(nextPreviewStackOrder());
    setAnchor(frameAnchor());
    setOpen(true);
  }

  function holdPreview() {
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
  }

  function scheduleClose() {
    cancelOpen();
    if (sticky) return;
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = window.setTimeout(() => setOpen(false), 220);
  }

  function detachPreview() {
    holdPreview();
    setSticky(true);
  }

  function closePreview() {
    setSticky(false);
    setOpen(false);
  }

  // Pin promotes the transient hover preview into the persistent floating layer at its current
  // position/size, same as WikiLink promoting a note preview.
  function promote(bounds: PreviewBounds, collapsed: boolean) {
    floating.open({ kind: "media", src, alt, noteKind: kind }, bounds, collapsed, true);
    setSticky(false);
    setOpen(false);
  }

  function floatMedia() {
    floating.open({ kind: "media", src, alt, noteKind: kind }, initialPreviewBounds(frameAnchor()), false, false);
  }

  function enterFullscreen() {
    ref.current?.requestFullscreen?.().catch(() => {});
  }

  return (
    <div className="media-frame" ref={ref} onMouseEnter={scheduleOpen} onMouseLeave={scheduleClose}>
      {children}
      <div className="media-controls">
        <button
          className="media-control"
          type="button"
          onClick={enterFullscreen}
          aria-label="Fullscreen"
          title="Fullscreen"
        >
          {/* Expand-to-corners glyph: view the media full-screen (native Fullscreen API). */}
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
        <button
          className="media-control"
          type="button"
          onClick={floatMedia}
          aria-label="Float media"
          title="Float"
        >
          {/* Picture-in-picture glyph: pop the media out into the floating layer. Deliberately not the
              pushpin PinIcon, which means "keep this preview open" on a floating window. */}
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
      </div>
      {open && anchor ? (
        <MediaWindow
          src={src}
          alt={alt}
          kind={kind}
          initialBounds={initialPreviewBounds(anchor)}
          reanchor={sticky ? undefined : anchor}
          pinned={false}
          depth={0}
          stackOrder={stackOrder}
          onActivate={() => setStackOrder(nextPreviewStackOrder())}
          onHold={holdPreview}
          onLeave={scheduleClose}
          onDetach={detachPreview}
          onClose={closePreview}
          onPinToggle={promote}
        />
      ) : null}
    </div>
  );
}
