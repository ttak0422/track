import { type ReactNode, useContext, useRef } from "react";
import { initialPreviewBounds, type PreviewAnchor } from "../preview/bounds";
import { InFloatingWindowContext, useFloating } from "../preview/floatingStore";
import { NoteKindContext } from "./context";

// MediaFrame wraps a media embed (image, PDF) so hovering reveals a pin that floats it into the
// persistent floating layer, the same windows wiki previews use. Inside a floating window it renders the
// media bare, so a floated image does not offer to float itself again.
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

  return (
    <div className="media-frame" ref={ref}>
      {children}
      <button className="media-pin" type="button" onClick={floatMedia} aria-label="Float media" title="Float">
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
  );
}
