import { Link } from "@tanstack/react-router";
import { useContext, useEffect, useRef, useState } from "react";
import { useResolveQuery } from "../../queries";
import { PreviewDepthContext } from "../markdown/context";
import { type PreviewAnchor, type PreviewBounds, initialPreviewBounds } from "./bounds";
import { useFloating } from "./floatingStore";
import { NoteWindow } from "./NoteWindow";
import { nextPreviewStackOrder, previewOpenDelay } from "./stack";

interface WikiLinkProps {
  target: string;
  display: string;
}

export function WikiLink({ target, display }: WikiLinkProps) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<PreviewAnchor | null>(null);
  const [stackOrder, setStackOrder] = useState(nextPreviewStackOrder);
  const linkRef = useRef<HTMLAnchorElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);
  const openTimer = useRef<number | undefined>(undefined);
  const depth = useContext(PreviewDepthContext);
  const floating = useFloating();
  const resolved = useResolveQuery(target);
  const noteID = resolved.data?.found ? resolved.data.note.note_id : undefined;

  useEffect(() => {
    return () => {
      if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
      if (openTimer.current !== undefined) window.clearTimeout(openTimer.current);
    };
  }, []);

  // scheduleOpen defers opening on hover until the pointer has rested on the link, so a cursor passing
  // over a column of links does not flash a preview under each one.
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
    bringPreviewToFront();
    const rect = linkRef.current?.getBoundingClientRect();
    if (rect) {
      setAnchor({ linkLeft: rect.left, linkRight: rect.right, linkTop: rect.top, linkBottom: rect.bottom });
    }
    setOpen(true);
  }

  function holdPreview() {
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
  }

  function bringPreviewToFront() {
    setStackOrder(nextPreviewStackOrder());
  }

  function scheduleClose() {
    cancelOpen();
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = window.setTimeout(() => setOpen(false), 220);
  }

  // Pinning promotes the transient hover preview into the persistent floating layer at its current
  // position, then closes the inline copy.
  function promote(bounds: PreviewBounds, collapsed: boolean) {
    if (noteID === undefined) return;
    floating.open({ kind: "note", noteID }, bounds, collapsed, true);
    setOpen(false);
  }

  if (resolved.isPending) {
    return <span className="wiki-link pending">{display}</span>;
  }

  if (!noteID) {
    return <span className="wiki-link unresolved">{display}</span>;
  }

  return (
    <span
      className="wiki-link-wrap"
      onBlur={scheduleClose}
      onFocus={openPreview}
      onMouseEnter={scheduleOpen}
      onMouseLeave={scheduleClose}
    >
      <Link className="wiki-link" ref={linkRef} to="/notes/$noteId" params={{ noteId: String(noteID) }}>
        {display}
      </Link>
      {open && anchor ? (
        <NoteWindow
          noteID={noteID}
          initialBounds={initialPreviewBounds(anchor)}
          reanchor={anchor}
          pinned={false}
          depth={depth}
          stackOrder={stackOrder}
          onActivate={bringPreviewToFront}
          onHold={holdPreview}
          onClose={() => setOpen(false)}
          onPinToggle={promote}
        />
      ) : null}
    </span>
  );
}
