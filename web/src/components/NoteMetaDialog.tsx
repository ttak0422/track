import { useEffect, useState } from "react";
import { useNoteMetaQuery, useSaveNoteMetaMutation } from "../queries";
import type { NoteID } from "../types";

// NoteMetaDialog edits a note's page metadata — the description and cover image the static export
// publishes as og:description / og:image. Validation lives in the engine (the same path as
// `track meta`); a rejected image surfaces the server's message inline and keeps the dialog open.
export function NoteMetaDialog({ noteID, onClose }: { noteID: NoteID; onClose: () => void }) {
  const meta = useNoteMetaQuery(noteID, { enabled: true });
  const save = useSaveNoteMetaMutation(noteID);
  const [description, setDescription] = useState("");
  const [image, setImage] = useState("");
  const [loadedFor, setLoadedFor] = useState<NoteID | null>(null);

  // Seed the fields once from the fetched metadata; later edits belong to the user.
  useEffect(() => {
    if (meta.data && loadedFor !== noteID) {
      setDescription(meta.data.description);
      setImage(meta.data.image);
      setLoadedFor(noteID);
    }
  }, [meta.data, loadedFor, noteID]);

  async function submit() {
    if (save.isPending) return;
    try {
      await save.mutateAsync({ description: description.trim(), image: image.trim() });
      onClose();
    } catch {
      // The validation message surfaces via save.isError below; the dialog stays open.
    }
  }

  return (
    <div
      className="modal-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="note-meta-title"
      onClick={onClose}
    >
      {/* Stop backdrop clicks inside the card from dismissing the dialog. */}
      <div className="modal-card" onClick={(event) => event.stopPropagation()}>
        <h3 id="note-meta-title">Page metadata</h3>
        <label className="modal-field">
          <span className="muted">Description (og:description)</span>
          <textarea
            className="modal-input"
            rows={3}
            value={description}
            /* eslint-disable-next-line jsx-a11y/no-autofocus */
            autoFocus
            disabled={meta.isPending}
            onChange={(event) => setDescription(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") onClose();
            }}
          />
        </label>
        <label className="modal-field">
          <span className="muted">Cover image (og:image) — a vault asset, e.g. assets/cover.png</span>
          <input
            className="modal-input"
            value={image}
            placeholder="assets/cover.png"
            disabled={meta.isPending}
            onChange={(event) => setImage(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") void submit();
              if (event.key === "Escape") onClose();
            }}
          />
        </label>
        {meta.isError ? <p className="error">{meta.error.message}</p> : null}
        {save.isError ? <p className="error">{save.error.message}</p> : null}
        <div className="modal-actions">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" disabled={meta.isPending || save.isPending} onClick={submit}>
            {save.isPending ? "Saving..." : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}
