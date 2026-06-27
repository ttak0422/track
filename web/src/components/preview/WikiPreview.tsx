import { type PointerEvent, useEffect, useRef, useState } from "react";
import { useNoteQuery, useRenderQuery } from "../../queries";
import { PreviewDepthContext } from "../markdown/context";
import { MarkdownView } from "../MarkdownView";
import {
  constrainPreviewBounds,
  initialPreviewBounds,
  type PreviewAnchor,
  type PreviewBounds,
  type PreviewResizeCorner,
  resizePreviewBounds,
} from "./bounds";
import { previewBaseZIndex } from "./stack";

export interface WikiPreviewProps {
  noteID: number;
  anchor: PreviewAnchor;
  depth: number;
  pinned: boolean;
  stackOrder: number;
  onActivate: () => void;
  onClose: () => void;
  onHold: () => void;
  onPin: () => void;
}

interface PreviewDragState {
  pointerId: number;
  mode: "move" | PreviewResizeCorner;
  startX: number;
  startY: number;
  startBounds: PreviewBounds;
}

export function WikiPreview({
  noteID,
  anchor,
  depth,
  pinned,
  stackOrder,
  onActivate,
  onClose,
  onHold,
  onPin,
}: WikiPreviewProps) {
  const note = useNoteQuery(noteID);
  const [bounds, setBounds] = useState(() => initialPreviewBounds(anchor));
  const [collapsed, setCollapsed] = useState(false);
  const dragRef = useRef<PreviewDragState | null>(null);
  // Sanitize the previewed body the same way as the main reader, so action links are flattened here too.
  const rendered = useRenderQuery(note.data?.note.body ?? "");

  useEffect(() => {
    if (!pinned) {
      setBounds(initialPreviewBounds(anchor));
    }
  }, [anchor.linkLeft, anchor.linkRight, anchor.linkTop, anchor.linkBottom, pinned]);

  function startMove(event: PointerEvent<HTMLElement>) {
    startDrag(event, "move");
  }

  function startResize(corner: PreviewResizeCorner) {
    return (event: PointerEvent<HTMLElement>) => startDrag(event, corner);
  }

  function startDrag(event: PointerEvent<HTMLElement>, mode: PreviewDragState["mode"]) {
    event.preventDefault();
    event.stopPropagation();
    onActivate();
    onPin();
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

  const title = note.data?.note.title ?? "Preview";

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
        // Collapsed shows only the chrome bar, so let its height shrink to content.
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
          {note.isPending ? <p className="muted">Loading...</p> : null}
          {note.isError ? <p className="error">{note.error.message}</p> : null}
          {note.data ? (
            <PreviewDepthContext.Provider value={depth + 1}>
              <MarkdownView markdown={rendered.data?.markdown ?? ""} kind={note.data.note.file_kind} />
            </PreviewDepthContext.Provider>
          ) : null}
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
