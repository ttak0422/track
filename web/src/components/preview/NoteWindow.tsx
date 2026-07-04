import { useNavigate } from "@tanstack/react-router";
import type { NoteID } from "../../types";
import { useNoteQuery, useRenderQuery } from "../../queries";
import { PreviewDepthContext } from "../markdown/context";
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
  const rendered = useRenderQuery(note.data?.note.body ?? "");
  const title = note.data?.note.title ?? "Preview";

  return (
    <FloatingWindow
      title={title}
      {...controls}
      onJump={() => navigate({ to: "/notes/$noteId", params: { noteId: String(noteID) } })}
    >
      {note.isPending ? <LoadingIndicator label="Loading note" /> : null}
      {note.isError ? <p className="error">{note.error.message}</p> : null}
      {note.data ? (
        <PreviewDepthContext.Provider value={controls.depth + 1}>
          <MarkdownView markdown={rendered.data?.markdown ?? ""} kind={note.data.note.file_kind} />
        </PreviewDepthContext.Provider>
      ) : null}
    </FloatingWindow>
  );
}
