import { type ReactNode, useContext, useRef } from "react";
import { initialPreviewBounds, type PreviewAnchor } from "../preview/bounds";
import { InFloatingWindowContext, useFloating } from "../preview/floatingStore";
import { NoteKindContext } from "./context";

// MediaFrame wraps a media embed (image, PDF) so hovering reveals controls: view it full-screen (native
// Fullscreen API) or float it into the persistent floating layer, the same windows wiki previews use.
// Inside a floating window it renders the media bare, so a floated image offers neither control again.
export function MediaFrame({ src, alt, children }: { src: string; alt: string; children: ReactNode }) {
  const inFloating = useContext(InFloatingWindowContext);
  const kind = useContext(NoteKindContext);
  const floating = useFloating();
  const ref = useRef<HTMLDivElement>(null);

  if (inFloating) {
    return <>{children}</>;
  }

  function floatMedia() {
    const rect = ref.current?.getBoundingClientRect();
    const anchor: PreviewAnchor = rect
      ? { linkLeft: rect.left, linkRight: rect.right, linkTop: rect.top, linkBottom: rect.bottom }
      : { linkLeft: 0, linkRight: 0, linkTop: 0, linkBottom: 0 };
    floating.open({ kind: "media", src, alt, noteKind: kind }, initialPreviewBounds(anchor), false, false);
  }

  function enterFullscreen() {
    ref.current?.requestFullscreen?.().catch(() => {});
  }

  return (
    <div className="media-frame" ref={ref}>
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
    </div>
  );
}
