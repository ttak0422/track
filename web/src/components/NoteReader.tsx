import { Link } from "@tanstack/react-router";
import { FormEvent, useEffect, useState } from "react";
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
  const noteQuery = useNoteQuery(noteID);
  const saveNote = useSaveNoteMutation(noteID);
  const { setQuery } = useSearchState();
  const [body, setBody] = useState("");
  const [copied, setCopied] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>(() => storedEditorMode());

  useEffect(() => {
    if (noteQuery.data) {
      setBody(noteQuery.data.note.body);
    }
  }, [noteQuery.data?.note.etag]);

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
  const dirty = body !== note.body;
  const tags = note.tags ?? [];

  async function copyPath() {
    if (!note) return;
    await navigator.clipboard.writeText(note.copy_path);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1200);
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!note || !dirty || saveNote.isPending) return;
    saveNote.mutate({ body, etag: note.etag });
  }

  return (
    <article className="note-reader">
      <header className="note-header">
        <div>
          <h2>{note.title}</h2>
          <p>{note.copy_path}</p>
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
          <button className="secondary-button" type="button" onClick={copyPath}>
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
        <div className="editor-actions">
          {saveNote.isError ? <p className="error">{saveNote.error.message}</p> : null}
          {saveNote.isSuccess && !dirty ? <p className="muted">Saved.</p> : null}
          <button className="primary-button" type="submit" disabled={!dirty || saveNote.isPending}>
            {saveNote.isPending ? "Saving..." : "Save"}
          </button>
        </div>
      </form>

      <section className="backlinks" aria-labelledby="backlinks-heading">
        <h3 id="backlinks-heading">Backlinks</h3>
        {data.backlinks.length === 0 ? <p className="muted">No backlinks.</p> : null}
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
