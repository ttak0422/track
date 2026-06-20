import { Link } from "@tanstack/react-router";
import { FormEvent, useEffect, useRef, useState } from "react";
import { MarkdownView } from "./MarkdownView";
import { useNoteQuery, useSaveNoteMutation } from "../queries";
import { useSearchState } from "../searchState";
import type { NoteID } from "../types";

interface NoteReaderProps {
  noteID: NoteID;
}

type EditorMode = "preview" | "edit" | "split";
const editorModeKey = "track.editor.mode";
const editorModes: EditorMode[] = ["preview", "edit", "split"];

export function NoteReader({ noteID }: NoteReaderProps) {
  // Poll so the note reflects edits made elsewhere; we still guard against
  // clobbering unsaved local edits below.
  const noteQuery = useNoteQuery(noteID, { live: true });
  const saveNote = useSaveNoteMutation(noteID);
  const { setQuery } = useSearchState();
  const [body, setBody] = useState("");
  const [copied, setCopied] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>(() => storedEditorMode());
  // The note/body/etag last adopted from disk. Edits are "dirty" relative to this, and
  // saves use this etag so a background reload cannot mask a conflicting change. noteID is
  // tracked so switching notes always reloads, even with unsaved edits to the previous note.
  const loadedRef = useRef({ noteID, body: "", etag: "" });

  useEffect(() => {
    const incoming = noteQuery.data?.note;
    if (!incoming) return;
    // Adopt the incoming note when switching to a different note (discarding any unsaved edits to
    // the previous one — otherwise the dirty guard below would block the switch entirely), or when
    // the current note changed on disk and the user has no unsaved edits.
    const switchedNote = noteID !== loadedRef.current.noteID;
    if (switchedNote || body === loadedRef.current.body) {
      loadedRef.current = { noteID, body: incoming.body, etag: incoming.etag };
      setBody(incoming.body);
    }
  }, [noteID, noteQuery.data?.note.etag]);

  useEffect(() => {
    localStorage.setItem(editorModeKey, editorMode);
  }, [editorMode]);

  if (noteQuery.isPending) {
    return <p className="muted">Loading note...</p>;
  }

  if (noteQuery.isError) {
    return <p className="error">{noteQuery.error.message}</p>;
  }

  const data = noteQuery.data;
  const note = data.note;
  const dirty = body !== loadedRef.current.body;
  const changedOnDisk = note.etag !== loadedRef.current.etag;
  const tags = note.tags ?? [];

  async function copyPath() {
    if (!note) return;
    await navigator.clipboard.writeText(note.copy_path);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1200);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!dirty || saveNote.isPending) return;
    try {
      const response = await saveNote.mutateAsync({ body, etag: loadedRef.current.etag });
      loadedRef.current = { noteID, body, etag: response.etag };
    } catch {
      // Conflict/errors surface via saveNote.isError below.
    }
  }

  return (
    <article className="note-reader">
      <header className="note-header">
        <div>
          <h2>{note.title}</h2>
        </div>
        <div className="note-header-actions">
          <div className="mode-switch" role="group" aria-label="Markdown display mode">
            {editorModes.map((mode) => (
              <button
                aria-pressed={editorMode === mode}
                key={mode}
                type="button"
                onClick={() => setEditorMode(mode)}
              >
                {modeLabel(mode)}
              </button>
            ))}
          </div>
          <button
            className={`copy-path${copied ? " copied" : ""}`}
            type="button"
            onClick={copyPath}
          >
            {copied ? "Copied" : "Copy path"}
          </button>
        </div>
      </header>

      {tags.length > 0 ? (
        <div className="tag-list note-tags" aria-label="Note tags">
          {tags.map((tag) => (
            <button key={tag} type="button" onClick={() => setQuery(`#${tag}`)}>
              #{tag}
            </button>
          ))}
        </div>
      ) : null}

      <form className="note-editor" onSubmit={submit}>
        <div className={`editor-grid editor-grid-${editorMode}`}>
          {editorMode !== "preview" ? (
            <textarea
              aria-label="Note body"
              value={body}
              onChange={(event) => setBody(event.currentTarget.value)}
            />
          ) : null}
          {editorMode !== "edit" ? (
            <section className="note-preview" aria-label="Rendered note preview">
              <MarkdownView markdown={body} />
            </section>
          ) : null}
        </div>
        {editorMode !== "preview" ? (
          <div className="editor-actions">
            {dirty && changedOnDisk ? (
              <p className="error">This note changed on disk while you were editing.</p>
            ) : null}
            {saveNote.isError ? <p className="error">{saveNote.error.message}</p> : null}
            {saveNote.isSuccess && !dirty ? <p className="muted">Saved.</p> : null}
            <button className="primary-button" type="submit" disabled={!dirty || saveNote.isPending}>
              {saveNote.isPending ? "Saving..." : "Save"}
            </button>
          </div>
        ) : null}
      </form>

      <section className="backlinks" aria-labelledby="backlinks-heading">
        <h3 id="backlinks-heading">Backlinks</h3>
        {data.backlinks.length === 0 ? (
          <p className="muted">No backlinks.</p>
        ) : (
          // Cap the height so a heavily linked note does not push the rest of the page away; the list
          // scrolls past that point.
          <div className="backlink-list">
            {data.backlinks.map((backlink) => (
              <Link
                className="backlink"
                key={backlink.note_id}
                to="/notes/$noteId"
                params={{ noteId: String(backlink.note_id) }}
              >
                {backlink.title}
              </Link>
            ))}
          </div>
        )}
      </section>
    </article>
  );
}

function storedEditorMode(): EditorMode {
  const value = localStorage.getItem(editorModeKey);
  return value === "edit" || value === "split" || value === "preview" ? value : "preview";
}

function modeLabel(mode: EditorMode): string {
  switch (mode) {
    case "edit":
      return "Edit";
    case "preview":
      return "Preview";
    case "split":
      return "Split";
  }
}
