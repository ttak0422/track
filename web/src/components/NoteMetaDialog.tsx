import { useEffect, useRef, useState } from "react";
import { useNoteMetaQuery, useSaveNoteMetaMutation, useUploadAssetMutation } from "../queries";
import type { NoteID } from "../types";

// NoteMetaDialog edits a note's editable sidecar metadata. The built-in fields get dedicated typed
// controls — title (a rename on change: backlinks rewritten by the engine), tags, description, a
// cover image picked from the browser and uploaded into the vault assets, and an icon — while props
// stays the one free-form control (a YAML "key: value" block). The engine composes and validates the whole edit
// (the same rules as `track meta --edit`), so the frontend never assembles YAML: a rejected edit
// surfaces the server's message inline and keeps the dialog open, changing nothing.
export function NoteMetaDialog({ noteID, onClose }: { noteID: NoteID; onClose: () => void }) {
  const meta = useNoteMetaQuery(noteID, { enabled: true });
  const save = useSaveNoteMetaMutation(noteID);
  const upload = useUploadAssetMutation();
  const [title, setTitle] = useState("");
  const [tags, setTags] = useState("");
  const [description, setDescription] = useState("");
  const [image, setImage] = useState("");
  const [icon, setIcon] = useState("");
  const [props, setProps] = useState("");
  const [loadedFor, setLoadedFor] = useState<NoteID | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  // A journal's title is derived from its date; the engine keeps that mechanical, so title editing is
  // disabled here rather than offering a rename that would fight the naming scheme.
  const isJournal = meta.data?.kind === "journal";

  // Seed the fields once from the fetched metadata; later edits belong to the user.
  useEffect(() => {
    if (meta.data && loadedFor !== noteID) {
      setTitle(meta.data.title);
      setTags(meta.data.tags.join(", "));
      setDescription(meta.data.description);
      setImage(meta.data.image);
      setIcon(meta.data.icon);
      setProps(meta.data.props);
      setLoadedFor(noteID);
    }
  }, [meta.data, loadedFor, noteID]);

  async function submit() {
    if (save.isPending) return;
    try {
      await save.mutateAsync({
        title: title.trim(),
        tags: tags
          .split(",")
          .map((tag) => tag.trim())
          .filter((tag) => tag.length > 0),
        description,
        image: image.trim(),
        icon: icon.trim(),
        props,
      });
      onClose();
    } catch {
      // The validation message surfaces via save.isError below; the dialog stays open.
    }
  }

  async function pickImage(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.currentTarget.files?.[0];
    // Reset so re-picking the same file fires onChange again.
    event.currentTarget.value = "";
    if (!file) return;
    try {
      const { ref } = await upload.mutateAsync(file);
      setImage(ref);
    } catch {
      // An upload failure surfaces via upload.isError; the existing ref is left untouched.
    }
  }

  const escapeCloses = (event: React.KeyboardEvent) => {
    if (event.key === "Escape") onClose();
  };
  const enterSubmits = (event: React.KeyboardEvent) => {
    if (event.key === "Enter") void submit();
    if (event.key === "Escape") onClose();
  };

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
            {isJournal
              ? "Title — a journal's title is set by its date and can't be changed here"
              : "Title — changing it renames the note and rewrites backlinks"}
          </span>
          <input
            className="modal-input"
            aria-label="Title"
            value={title}
            /* eslint-disable-next-line jsx-a11y/no-autofocus */
            autoFocus
            disabled={meta.isPending || isJournal}
            onChange={(event) => setTitle(event.currentTarget.value)}
            onKeyDown={enterSubmits}
          />
        </label>
        <label className="modal-field">
          <span className="muted">Tags — comma-separated</span>
          <input
            className="modal-input"
            aria-label="Tags"
            value={tags}
            placeholder="project, draft"
            disabled={meta.isPending}
            onChange={(event) => setTags(event.currentTarget.value)}
            onKeyDown={enterSubmits}
          />
        </label>
        <label className="modal-field">
          <span className="muted">Description (og:description)</span>
          <textarea
            className="modal-input"
            aria-label="Description"
            rows={3}
            value={description}
            disabled={meta.isPending}
            onChange={(event) => setDescription(event.currentTarget.value)}
            onKeyDown={escapeCloses}
          />
        </label>
        <div className="modal-field">
          <span className="muted">
            Cover image (og:image) — “Choose a file…” copies the picked image into the vault’s
            assets/ and fills in its reference; or type an existing assets/… path
          </span>
          <div className="modal-image-row">
            <input
              className="modal-input"
              aria-label="Cover image"
              value={image}
              placeholder="assets/cover.png"
              disabled={meta.isPending}
              onChange={(event) => setImage(event.currentTarget.value)}
              onKeyDown={enterSubmits}
            />
            <button
              type="button"
              disabled={meta.isPending || upload.isPending}
              onClick={() => fileInput.current?.click()}
            >
              {upload.isPending ? "Importing…" : "Choose a file…"}
            </button>
          </div>
          <input ref={fileInput} type="file" accept="image/*" hidden onChange={pickImage} />
        </div>
        <label className="modal-field">
          <span className="muted">
            Icon — an emoji shown beside the title; empty falls back to the tag/kind mapping
          </span>
          <input
            className="modal-input"
            aria-label="Icon"
            value={icon}
            placeholder="📚"
            disabled={meta.isPending}
            onChange={(event) => setIcon(event.currentTarget.value)}
            onKeyDown={enterSubmits}
          />
        </label>
        <label className="modal-field">
          <span className="muted">Properties — free-form YAML, one “key: value” per line</span>
          <textarea
            className="modal-input modal-input--code"
            aria-label="Properties"
            rows={6}
            value={props}
            placeholder={"status: reading\nrating: 8"}
            disabled={meta.isPending}
            onChange={(event) => setProps(event.currentTarget.value)}
            onKeyDown={escapeCloses}
          />
        </label>
        {meta.isError ? <p className="error">{meta.error.message}</p> : null}
        {upload.isError ? <p className="error">{upload.error.message}</p> : null}
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
