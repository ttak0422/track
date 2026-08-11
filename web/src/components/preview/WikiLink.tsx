import { Link } from "@tanstack/react-router";
import { useContext, useEffect, useRef, useState } from "react";
import { useResolveQuery } from "../../queries";
import { NoteVaultContext, PreviewDepthContext } from "../markdown/context";
import { blockElementID, splitWikiTarget } from "../markdown/plugins";
import { headingElementID } from "../markdown/toc";
import { type PreviewAnchor, type PreviewBounds, initialPreviewBounds } from "./bounds";
import { useFloating } from "./floatingStore";
import { NoteWindow } from "./NoteWindow";
import {
  activatePreview,
  bringPreviewToFront as raisePreviewToFront,
  createPreviewID,
  deactivatePreview,
  pointerCanHover,
  previewOpenDelay,
  releasePreview,
  usePreviewStackOrder,
} from "./stack";

interface WikiLinkProps {
  target: string;
  display: string;
}

export function WikiLink({ target, display }: WikiLinkProps) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<PreviewAnchor | null>(null);
  const [sticky, setSticky] = useState(false);
  const [previewID] = useState(createPreviewID);
  const stackOrder = usePreviewStackOrder(previewID);
  const linkRef = useRef<HTMLAnchorElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);
  const openTimer = useRef<number | undefined>(undefined);
  const depth = useContext(PreviewDepthContext);
  const floating = useFloating();
  // The target may carry a "#..." anchor (heading or ^block); the note resolves by its key, and a
  // block anchor becomes the URL hash so the reader scrolls to and highlights the marked block.
  const { key, blockID, headingID } = splitWikiTarget(target);
  const vault = useContext(NoteVaultContext);
  const resolved = useResolveQuery(key, vault);
  const noteID = resolved.data?.found ? resolved.data.note.note_id : undefined;

  useEffect(() => {
    return () => {
      releasePreview(previewID);
      if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
      if (openTimer.current !== undefined) window.clearTimeout(openTimer.current);
    };
  }, [previewID]);

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
    // Both ways in pass through here — the hover-intent timer and the focus a tap already gives the
    // link — so the pointer is asked once, here (see pointerCanHover).
    if (!pointerCanHover()) return;
    holdPreview();
    cancelOpen();
    activatePreview(previewID);
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
    raisePreviewToFront(previewID);
  }

  function scheduleClose() {
    cancelOpen();
    if (sticky) return;
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = window.setTimeout(() => {
      deactivatePreview(previewID);
      setOpen(false);
    }, 220);
  }

  // Pinning promotes the transient hover preview into the persistent floating layer at its current
  // position, then closes the inline copy.
  function promote(bounds: PreviewBounds, collapsed: boolean) {
    if (noteID === undefined) return;
    floating.open({ kind: "note", noteID }, bounds, collapsed, true);
    setSticky(false);
    deactivatePreview(previewID);
    setOpen(false);
  }

  function detachPreview() {
    holdPreview();
    setSticky(true);
  }

  function closePreview() {
    setSticky(false);
    deactivatePreview(previewID);
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
      <Link
        className="wiki-link"
        ref={linkRef}
        to="/notes/$noteId"
        params={{ noteId: String(noteID) }}
        hash={anchorHash(blockID, headingID)}
      >
        {display}
      </Link>
      {open && anchor ? (
        <NoteWindow
          noteID={noteID}
          initialBounds={initialPreviewBounds(anchor)}
          reanchor={sticky ? undefined : anchor}
          pinned={false}
          depth={depth}
          stackOrder={stackOrder}
          onActivate={bringPreviewToFront}
          onHold={holdPreview}
          onDetach={detachPreview}
          onClose={closePreview}
          onPinToggle={promote}
        />
      ) : null}
    </span>
  );
}

// anchorHash is the URL hash a [[Note#anchor]] link navigates with: a block marker, a heading, or
// nothing. Both ids are namespaced (see blockElementID / headingElementID), so they cannot collide.
function anchorHash(blockID: string, headingID: string): string | undefined {
  if (blockID) return blockElementID(blockID);
  if (headingID) return headingElementID(headingID);
  return undefined;
}
