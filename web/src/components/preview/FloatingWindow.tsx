import { type PointerEvent, type ReactNode, useEffect, useRef, useState } from "react";
import {
  constrainPreviewBounds,
  initialPreviewBounds,
  type PreviewAnchor,
  type PreviewBounds,
  type PreviewResizeCorner,
  resizePreviewBounds,
} from "./bounds";
import { InFloatingWindowContext } from "./floatingStore";
import { previewBaseZIndex } from "./stack";

// The window's frame/behavior props, shared by the content wrappers (NoteWindow, MediaWindow).
export interface FloatingWindowControls {
  initialBounds: PreviewBounds;
  // While unpinned, re-place the window when its source link's rect changes (a fresh hover).
  reanchor?: PreviewAnchor;
  pinned: boolean;
  initialCollapsed?: boolean;
  depth: number;
  stackOrder: number;
  onActivate: () => void;
  onHold?: () => void;
  onClose: () => void;
  // Toggle pin: an unpinned window promotes (carrying its current bounds/collapsed); a pinned one unpins.
  onPinToggle: (bounds: PreviewBounds, collapsed: boolean) => void;
}

interface FloatingWindowProps extends FloatingWindowControls {
  title: string;
  children: ReactNode;
}

interface DragState {
  pointerId: number;
  mode: "move" | PreviewResizeCorner;
  startX: number;
  startY: number;
  startBounds: PreviewBounds;
}

// FloatingWindow is the draggable/resizable/collapsible chrome shared by hover previews and the pinned
// windows in the floating layer. Content (a note body or a media embed) is passed as children.
export function FloatingWindow({
  title,
  initialBounds,
  reanchor,
  pinned,
  initialCollapsed = false,
  depth,
  stackOrder,
  onActivate,
  onHold,
  onClose,
  onPinToggle,
  children,
}: FloatingWindowProps) {
  const [bounds, setBounds] = useState(initialBounds);
  const [collapsed, setCollapsed] = useState(initialCollapsed);
  const dragRef = useRef<DragState | null>(null);

  useEffect(() => {
    if (!pinned && reanchor) {
      setBounds(initialPreviewBounds(reanchor));
    }
    // Re-place only when the anchor actually moves (a new hover), not on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reanchor?.linkLeft, reanchor?.linkRight, reanchor?.linkTop, reanchor?.linkBottom, pinned]);

  function startMove(event: PointerEvent<HTMLElement>) {
    startDrag(event, "move");
  }

  function startResize(corner: PreviewResizeCorner) {
    return (event: PointerEvent<HTMLElement>) => startDrag(event, corner);
  }

  function startDrag(event: PointerEvent<HTMLElement>, mode: DragState["mode"]) {
    event.preventDefault();
    event.stopPropagation();
    onActivate();
    dragRef.current = {
      pointerId: event.pointerId,
      mode,
      startX: event.clientX,
      startY: event.clientY,
      startBounds: bounds,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function dragPreview(event: PointerEvent<HTMLElement>) {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    const dx = event.clientX - drag.startX;
    const dy = event.clientY - drag.startY;
    setBounds(
      drag.mode === "move"
        ? constrainPreviewBounds({
            ...drag.startBounds,
            left: drag.startBounds.left + dx,
            top: drag.startBounds.top + dy,
          })
        : resizePreviewBounds(drag.mode, drag.startBounds, dx, dy),
    );
  }

  function endDrag(event: PointerEvent<HTMLElement>) {
    const drag = dragRef.current;
    dragRef.current = null;
    if (!drag || drag.pointerId !== event.pointerId) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
  }

  return (
    <aside
      className={`wiki-preview${pinned ? " pinned" : ""}${collapsed ? " collapsed" : ""}`}
      onFocusCapture={onActivate}
      onMouseEnter={onHold}
      onPointerDownCapture={onActivate}
      style={{
        left: bounds.left,
        top: bounds.top,
        width: bounds.width,
        height: collapsed ? "auto" : bounds.height,
        zIndex: previewBaseZIndex + depth + stackOrder,
      }}
    >
      <div
        className="wiki-preview-chrome"
        onPointerDown={startMove}
        onPointerMove={dragPreview}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        <button
          className="wiki-preview-toggle"
          type="button"
          onClick={() => setCollapsed((value) => !value)}
          onPointerDown={(event) => event.stopPropagation()}
          aria-expanded={!collapsed}
          aria-label={collapsed ? "Expand preview" : "Collapse preview"}
          title={collapsed ? "Expand" : "Collapse"}
        >
          <span className="wiki-preview-caret" aria-hidden="true" />
        </button>
        <span className="wiki-preview-title">{title}</span>
        <button
          className={`wiki-preview-pin${pinned ? " active" : ""}`}
          type="button"
          onClick={() => onPinToggle(bounds, collapsed)}
          onPointerDown={(event) => event.stopPropagation()}
          aria-pressed={pinned}
          aria-label={pinned ? "Unpin preview" : "Pin preview"}
          title={pinned ? "Unpin" : "Pin"}
        >
          <PinIcon filled={pinned} />
        </button>
        <button
          className="wiki-preview-close"
          type="button"
          onClick={onClose}
          onPointerDown={(event) => event.stopPropagation()}
          aria-label="Close preview"
        >
          ×
        </button>
      </div>
      {collapsed ? null : (
        <div className="wiki-preview-body">
          <InFloatingWindowContext.Provider value={true}>{children}</InFloatingWindowContext.Provider>
        </div>
      )}
      {collapsed
        ? null
        : (["nw", "ne", "sw", "se"] as const).map((corner) => (
            <button
              aria-label="Resize preview"
              className={`wiki-preview-resize wiki-preview-resize-${corner}`}
              key={corner}
              onPointerCancel={endDrag}
              onPointerDown={startResize(corner)}
              onPointerMove={dragPreview}
              onPointerUp={endDrag}
              title="Resize"
              type="button"
            />
          ))}
    </aside>
  );
}

function PinIcon({ filled }: { filled: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width="15"
      height="15"
      fill={filled ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <line x1="12" y1="17" x2="12" y2="22" />
      <path d="M9 4h6l-1 6 3 3H7l3-3-1-6z" />
    </svg>
  );
}
