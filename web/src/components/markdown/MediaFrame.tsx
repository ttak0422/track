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
    floating.pin({ kind: "media", src, alt, noteKind: kind }, initialPreviewBounds(anchor), false);
  }

  return (
    <div className="media-frame" ref={ref}>
      {children}
      <button className="media-pin" type="button" onClick={floatMedia} aria-label="Float media" title="Float">
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
          <line x1="12" y1="17" x2="12" y2="22" />
          <path d="M9 4h6l-1 6 3 3H7l3-3-1-6z" />
        </svg>
      </button>
    </div>
  );
}
