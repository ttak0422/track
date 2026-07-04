import { type PointerEvent, type ReactNode, useEffect, useRef, useState } from "react";
import {
  clamp,
  constrainPreviewBounds,
  initialPreviewBounds,
  minPreviewHeight,
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
  // Called when the pointer leaves the window. A hover preview anchored to a separate element (the graph
  // canvas) uses this to schedule its close; WikiLink instead closes via its wrapping link span, so it
  // leaves this undefined.
  onLeave?: () => void;
  // Called once the user actually moves/resizes the preview. Hover previews use this to become sticky
  // until the page changes or the user closes them.
  onDetach?: () => void;
  // Reports the live bounds/collapsed after every drag/resize/collapse, so a hover preview can hand its
  // kept window off to the floating layer at its current geometry.
  onBoundsChange?: (bounds: PreviewBounds, collapsed: boolean) => void;
  onClose: () => void;
  // Toggle pin: an unpinned window promotes (carrying its current bounds/collapsed); a pinned one unpins.
  onPinToggle: (bounds: PreviewBounds, collapsed: boolean) => void;
  // Navigate to the previewed content as a full page. Only set for content that has its own page (a
  // note); media embeds leave it undefined and the jump button is omitted.
  onJump?: () => void;
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
  detached: boolean;
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
  onLeave,
  onDetach,
  onBoundsChange,
  onClose,
  onPinToggle,
  onJump,
  children,
}: FloatingWindowProps) {
  const [bounds, setBounds] = useState(initialBounds);
  const [collapsed, setCollapsed] = useState(initialCollapsed);
  const dragRef = useRef<DragState | null>(null);
  const asideRef = useRef<HTMLElement>(null);
  const chromeRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  // Once the user resizes the window, stop auto-fitting its height to the content.
  const manualResizeRef = useRef(false);

  useEffect(() => {
    onBoundsChange?.(bounds, collapsed);
    // Report geometry only, not on every onBoundsChange identity change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bounds, collapsed]);

  // Auto-fit the height to the content: a preview with little to show shrinks to the old minimum instead
  // of opening needlessly tall, while a long note still fills up to the viewport-based cap
  // (initialBounds.height). Width is left alone. Re-runs as async content (images, math) settles, and
  // stops once the user resizes the window by hand.
  useEffect(() => {
    if (collapsed || typeof ResizeObserver === "undefined") return;
    const aside = asideRef.current;
    const chrome = chromeRef.current;
    const body = bodyRef.current;
    const content = contentRef.current;
    if (!aside || !chrome || !body || !content) return;
    const fit = () => {
      if (manualResizeRef.current) return;
      // border-box height = chrome + body content (incl. its padding) + the aside's own vertical border.
      const border = aside.offsetHeight - aside.clientHeight;
      const desired = chrome.offsetHeight + body.scrollHeight + border;
      setBounds((current) => {
        const height = clamp(desired, minPreviewHeight, initialBounds.height);
        return height === current.height ? current : constrainPreviewBounds({ ...current, height });
      });
    };
    fit();
    const observer = new ResizeObserver(fit);
    observer.observe(content);
    return () => observer.disconnect();
  }, [collapsed, initialBounds.height]);

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
    return (event: PointerEvent<HTMLElement>) => {
      // A manual resize locks the height; auto-fit no longer overrides the user's chosen size.
      manualResizeRef.current = true;
      startDrag(event, corner);
    };
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
      detached: false,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function dragPreview(event: PointerEvent<HTMLElement>) {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    const dx = event.clientX - drag.startX;
    const dy = event.clientY - drag.startY;
    if (!drag.detached && Math.abs(dx) + Math.abs(dy) > 4) {
      drag.detached = true;
      onDetach?.();
    }
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
      ref={asideRef}
      className={`wiki-preview${pinned ? " pinned" : ""}${collapsed ? " collapsed" : ""}${onJump ? " with-jump" : ""}`}
      onFocusCapture={onActivate}
      onMouseEnter={onHold}
      onMouseLeave={onLeave}
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
        ref={chromeRef}
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
        {onJump ? (
          <button
            className="wiki-preview-jump"
            type="button"
            onClick={onJump}
            onPointerDown={(event) => event.stopPropagation()}
            aria-label="Open as page"
            title="Open as page"
          >
            <JumpIcon />
          </button>
        ) : null}
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
        <div className="wiki-preview-body" ref={bodyRef}>
          <div ref={contentRef}>
            <InFloatingWindowContext.Provider value={true}>{children}</InFloatingWindowContext.Provider>
          </div>
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

function JumpIcon() {
  return (
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
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
      <polyline points="15 3 21 3 21 9" />
      <line x1="10" y1="14" x2="21" y2="3" />
    </svg>
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
