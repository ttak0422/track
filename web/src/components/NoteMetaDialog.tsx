import { useEffect, useState } from "react";
import { useNoteMetaQuery, useSaveNoteMetaMutation } from "../queries";
import type { NoteID } from "../types";

// NoteMetaDialog edits a note's editable sidecar metadata — tags, description, cover image, and
// typed props — as one YAML document. Parsing and validation live in the engine (the same path as
// `track meta --edit`): a rejected document (bad image, prop breaking the configured schema)
// surfaces the server's message inline and keeps the dialog open; nothing is written on rejection.
export function NoteMetaDialog({ noteID, onClose }: { noteID: NoteID; onClose: () => void }) {
  const meta = useNoteMetaQuery(noteID, { enabled: true });
  const save = useSaveNoteMetaMutation(noteID);
  const [doc, setDoc] = useState("");
  const [loadedFor, setLoadedFor] = useState<NoteID | null>(null);

  // Seed the document once from the fetched metadata; later edits belong to the user.
  useEffect(() => {
    if (meta.data && loadedFor !== noteID) {
      setDoc(meta.data.doc);
      setLoadedFor(noteID);
    }
  }, [meta.data, loadedFor, noteID]);

  async function submit() {
    if (save.isPending) return;
    try {
      await save.mutateAsync({ doc });
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
        <h3 id="note-meta-title">Note metadata</h3>
        <label className="modal-field">
          <span className="muted">
            tags, description, cover image (assets/&lt;file&gt;), and props — YAML, validated on save.
            The title is renamed elsewhere.
          </span>
          <textarea
            className="modal-input modal-input--code"
            rows={12}
            value={doc}
            /* eslint-disable-next-line jsx-a11y/no-autofocus */
            autoFocus
            disabled={meta.isPending}
            onChange={(event) => setDoc(event.currentTarget.value)}
            onKeyDown={(event) => {
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
