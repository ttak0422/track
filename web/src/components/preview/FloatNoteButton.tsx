import { useRef } from "react";
import type { NoteID } from "../../types";
import { IconPictureInPicture, RailIcon } from "../icons";
import { elementAnchor, initialPreviewBounds } from "./bounds";
import { useFloating } from "./floatingStore";

interface FloatNoteButtonProps {
  noteID: NoteID;
  // The caller's own control class: the button belongs to whatever chrome it stands in (a tab, a
  // search result), and only its behaviour is shared.
  className: string;
  // What this button floats, spelled out — a column of them all reading "Float this note" tells a
  // screen reader nothing about which row it is on.
  label: string;
}

// FloatNoteButton pops a note into the floating layer instead of navigating to it. It is the explicit
// affordance the layer needs outside a wikilink: nothing here opens on hover, the reader asks. The
// window is pinned, so it survives the navigation that usually follows asking for it.
export function FloatNoteButton({ noteID, className, label }: FloatNoteButtonProps) {
  const ref = useRef<HTMLButtonElement>(null);
  const floating = useFloating();

  function float() {
    floating.open({ kind: "note", noteID }, initialPreviewBounds(elementAnchor(ref.current)), false, {
      pinned: true,
    });
  }

  return (
    <button ref={ref} type="button" className={className} aria-label={label} title={label} onClick={float}>
      <RailIcon Icon={IconPictureInPicture} size={14} />
    </button>
  );
}
