import { Link } from "@tanstack/react-router";
import { useContext, useEffect, useRef } from "react";
import { useResolveQuery } from "../../queries";
import { NoteVaultContext } from "../markdown/context";
import { blockElementID, splitWikiTarget } from "../markdown/plugins";
import { headingElementID } from "../markdown/toc";
import { initialPreviewBounds } from "./bounds";
import { useFloating } from "./floatingStore";
import { pointerCanHover, previewOpenDelay } from "./stack";

interface WikiLinkProps {
  target: string;
  display: string;
}

export function WikiLink({ target, display }: WikiLinkProps) {
  const linkRef = useRef<HTMLAnchorElement>(null);
  const openTimer = useRef<number | undefined>(undefined);
  // The window this link opened, so leaving the link closes the window it is answerable for and no
  // other. It is only a handle: the window belongs to the floating layer, and a preview opened from
  // inside another preview stays when that one is closed.
  const openedRef = useRef<string | null>(null);
  const floating = useFloating();
  // The target may carry a "#..." anchor (heading or ^block); the note resolves by its key, and a
  // block anchor becomes the URL hash so the reader scrolls to and highlights the marked block.
  const { key, blockID, headingID } = splitWikiTarget(target);
  const vault = useContext(NoteVaultContext);
  const resolved = useResolveQuery(key, vault);
  const noteID = resolved.data?.found ? resolved.data.note.note_id : undefined;

  // Only the intent timer is this component's to clean up. A window it opened is the layer's, and
  // closing the preview this link sits in must not take the preview it opened down with it.
  useEffect(() => () => cancelOpen(), []);

  // scheduleOpen defers opening on hover until the pointer has rested on the link, so a cursor passing
  // over a column of links does not flash a preview under each one.
  function scheduleOpen() {
    holdPreview();
    if (openTimer.current !== undefined) return;
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
    if (!pointerCanHover() || noteID === undefined) return;
    cancelOpen();
    const rect = linkRef.current?.getBoundingClientRect();
    if (!rect) return;
    const anchor = {
      linkLeft: rect.left,
      linkRight: rect.right,
      linkTop: rect.top,
      linkBottom: rect.bottom,
    };
    openedRef.current = floating.open(
      { kind: "note", noteID },
      initialPreviewBounds(anchor),
      false,
      { transient: true, anchor },
    );
  }

  function holdPreview() {
    if (openedRef.current) floating.hold(openedRef.current);
  }

  function scheduleClose() {
    cancelOpen();
    if (openedRef.current) floating.scheduleClose(openedRef.current);
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
