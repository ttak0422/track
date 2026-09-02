import { useNavigate } from "@tanstack/react-router";
import type { NoteID } from "../../types";
import { useNoteQuery, useRenderQuery } from "../../queries";
import { vaultOf } from "../../vaultId";
import { MarkdownView } from "../MarkdownView";
import { LoadingIndicator } from "../noteShared";
import { FloatingWindow, type FloatingWindowControls } from "./FloatingWindow";

interface NoteWindowProps extends FloatingWindowControls {
  noteID: NoteID;
}

// NoteWindow frames a note's body in a FloatingWindow, used both for the inline hover preview and for a
// pinned window in the floating layer. It re-fetches by id, so a pinned window survives its link.
export function NoteWindow({ noteID, ...controls }: NoteWindowProps) {
  const navigate = useNavigate();
  const note = useNoteQuery(noteID);
  // Sanitize the previewed body the same way as the main reader, so action links are flattened here too.
  const vault = vaultOf(noteID);
  const body = note.data?.note.body ?? "";
  const rendered = useRenderQuery(body, vault);
  const title = note.data?.note.title ?? "Preview";
  // Hold the loading state until the sanitized body is in hand: the note and its render land in two
  // steps, and drawing an empty MarkdownView in between makes FloatingWindow's auto-fit shrink to
  // nothing and then jump back to the real height. An empty note (or a failed render) has nothing to
  // wait for, so it falls through to the empty view instead of spinning forever.
  const awaitingRender = body.trim() !== "" && rendered.data === undefined && !rendered.isError;

  return (
    <FloatingWindow
      title={title}
      copyTitle={note.data?.note.title}
      {...controls}
      onJump={() => navigate({ to: "/notes/$noteId", params: { noteId: String(noteID) } })}
    >
      {note.isPending || awaitingRender ? <LoadingIndicator label="Loading note" /> : null}
      {note.isError ? <p className="error">{note.error.message}</p> : null}
      {note.data && !awaitingRender ? (
        // The source path goes in, so selecting prose in a window offers the same copy actions as the
        // reader does. No portal: the popover is position: fixed and the window sets no transform, so
        // it escapes the scrolling body instead of being clipped by it, and paints inside the window's
        // own stacking band rather than under the whole preview layer.
        <MarkdownView
          markdown={rendered.data?.markdown ?? ""}
          title={note.data.note.title}
          showTitle={false}
          kind={note.data.note.file_kind}
          vault={vault}
          includes={rendered.data?.includes}
          copyPath={note.data.note.copy_path}
        />
      ) : null}
    </FloatingWindow>
  );
}
