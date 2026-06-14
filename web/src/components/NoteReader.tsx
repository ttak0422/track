import { Link } from "@tanstack/react-router";
import { FormEvent, useEffect, useState } from "react";
import { MarkdownView } from "./MarkdownView";
import { useNoteQuery, useSaveNoteMutation } from "../queries";
import type { NoteID } from "../types";

interface NoteReaderProps {
  noteID: NoteID;
}

export function NoteReader({ noteID }: NoteReaderProps) {
  const noteQuery = useNoteQuery(noteID);
  const saveNote = useSaveNoteMutation(noteID);
  const [body, setBody] = useState("");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (noteQuery.data) {
      setBody(noteQuery.data.note.body);
    }
  }, [noteQuery.data?.note.etag]);

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
        <button className="secondary-button" type="button" onClick={copyPath}>
          {copied ? "Copied" : "Copy path"}
        </button>
      </header>

      {tags.length > 0 ? (
        <div className="tag-list note-tags" aria-label="Note tags">
          {tags.map((tag) => (
            <span key={tag}>#{tag}</span>
          ))}
        </div>
      ) : null}

      <form className="note-editor" onSubmit={submit}>
        <div className="editor-grid">
          <textarea
            aria-label="Note body"
            value={body}
            onChange={(event) => setBody(event.currentTarget.value)}
          />
          <section className="note-preview" aria-label="Rendered note preview">
            <MarkdownView markdown={body} />
          </section>
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
