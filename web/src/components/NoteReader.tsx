import { Link, useBlocker, useNavigate } from "@tanstack/react-router";
import { FormEvent, useEffect, useRef, useState } from "react";
import { MarkdownView } from "./MarkdownView";
import { getFollowState } from "../api";
import { useAgendaQuery, useNoteQuery, useRenderQuery, useSaveNoteMutation } from "../queries";
import { useSearchState } from "../searchState";
import type { FileKind, FollowState, NoteID } from "../types";

interface NoteReaderProps {
  noteID: NoteID;
}

type EditorMode = "preview" | "edit" | "split";
const editorModes: EditorMode[] = ["preview", "edit", "split"];
const unsavedChangesMessage = "保存していない変更は失われます。移動しますか？";

export function NoteReader({ noteID }: NoteReaderProps) {
  // Poll so the note reflects edits made elsewhere; we still guard against
  // clobbering unsaved local edits below.
  const noteQuery = useNoteQuery(noteID, { live: true });
  const saveNote = useSaveNoteMutation(noteID);
  const { setQuery } = useSearchState();
  const navigate = useNavigate();
  // For a journal, surface the notes worked on that day. The day comes from the journal id (yyyyMMdd).
  const journalDate = journalDateFromNote(noteQuery.data?.note);
  const agendaQuery = useAgendaQuery(journalDate, { enabled: journalDate !== "" });
  const [body, setBody] = useState("");
  const [copied, setCopied] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>("preview");
  const [followEnabled, setFollowEnabled] = useState(false);
  // The preview renders server-sanitized Markdown (action links flattened, wiki links kept) rather than
  // the raw body, so track-specific rules live only in the engine. The body is posted as you type.
  const renderQuery = useRenderQuery(body);
  // The note/body/etag last adopted from disk. Edits are "dirty" relative to this, and
  // saves use this etag so a background reload cannot mask a conflicting change. noteID is
  // tracked so switching notes always reloads, even with unsaved edits to the previous note.
  const loadedRef = useRef({ noteID, body: "", etag: "" });
  const noteIDRef = useRef(noteID);
  const editorModeRef = useRef(editorMode);
  const pendingFollowRef = useRef<FollowState | null>(null);
  const previewRef = useRef<HTMLElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    noteIDRef.current = noteID;
  }, [noteID]);

  useEffect(() => {
    editorModeRef.current = editorMode;
  }, [editorMode]);

  useEffect(() => {
    const incoming = noteQuery.data?.note;
    if (!incoming) return;
    // Adopt the incoming note when switching to a different note (discarding any unsaved edits to
    // the previous one — otherwise the dirty guard below would block the switch entirely), or when
    // the current note changed on disk and the user has no unsaved edits.
    const switchedNote = noteID !== loadedRef.current.noteID;
    const discardedUnsavedEdit = switchedNote && body !== loadedRef.current.body;
    if (switchedNote || body === loadedRef.current.body) {
      loadedRef.current = { noteID, body: incoming.body, etag: incoming.etag };
      setBody(incoming.body);
      if (discardedUnsavedEdit) {
        setEditorMode("preview");
      }
    }
  }, [noteID, noteQuery.data?.note.etag]);

  const dirty = body !== loadedRef.current.body;

  useBlocker({
    shouldBlockFn: ({ current, next }) => {
      if (!dirty || current.pathname === next.pathname) return false;
      return !window.confirm(unsavedChangesMessage);
    },
    enableBeforeUnload: () => dirty,
    disabled: !dirty,
  });

  useEffect(() => {
    if (!followEnabled || typeof EventSource === "undefined") return;
    let closed = false;

    getFollowState()
      .then((response) => {
        if (!closed && response.active && response.state) {
          applyFollowState(response.state);
        }
      })
      .catch(() => {
        // Follow is best-effort: the button can stay active while the Neovim side has not published yet.
      });

    const source = new EventSource("/api/events");
    source.addEventListener("follow", (event) => {
      try {
        applyFollowState(JSON.parse(event.data) as FollowState);
      } catch {
        // Ignore malformed events from an older or interrupted server.
      }
    });
    return () => {
      closed = true;
      source.close();
    };
  }, [followEnabled]);

  useEffect(() => {
    if (!followEnabled || noteQuery.isPending) return;
    const state = pendingFollowRef.current;
    if (state && state.note_id === noteID) {
      window.requestAnimationFrame(() => scrollToFollowState(state));
    }
  }, [body, editorMode, followEnabled, noteID, noteQuery.isPending, renderQuery.data?.markdown]);

  function applyFollowState(state: FollowState) {
    pendingFollowRef.current = state;
    if (state.note_id !== noteIDRef.current) {
      void navigate({ to: "/notes/$noteId", params: { noteId: String(state.note_id) } });
      return;
    }
    window.requestAnimationFrame(() => scrollToFollowState(state));
  }

  function scrollToFollowState(state: FollowState) {
    const target = followScrollTarget();
    if (!target) return;
    const maxScroll = target.scrollHeight - target.clientHeight;
    if (maxScroll <= 0) return;
    const sourceTop = Math.max(1, state.top_line || state.line || 1);
    const sourceLines = Math.max(sourceTop, state.line_count || 1);
    const ratio = sourceLines <= 1 ? 0 : (sourceTop - 1) / (sourceLines - 1);
    target.scrollTo({ top: maxScroll * ratio });
  }

  function followScrollTarget(): HTMLElement | null {
    if (editorModeRef.current === "edit") {
      return textareaRef.current;
    }
    const preview = previewRef.current;
    if (!preview) return null;
    if (preview.scrollHeight > preview.clientHeight) {
      return preview;
    }
    return preview.closest<HTMLElement>(".reader");
  }

  function revealTextareaCaret(textarea: HTMLTextAreaElement) {
    window.requestAnimationFrame(() => {
      const position = textarea.selectionStart ?? 0;
      const line = textarea.value.slice(0, position).split("\n").length;
      const styles = window.getComputedStyle(textarea);
      const lineHeight = Number.parseFloat(styles.lineHeight) || 22;
      const paddingTop = Number.parseFloat(styles.paddingTop) || 0;
      const caretTop = paddingTop + (line - 1) * lineHeight;
      const visibleTop = textarea.scrollTop;
      const visibleBottom = visibleTop + textarea.clientHeight - lineHeight;
      if (caretTop < visibleTop) {
        textarea.scrollTop = Math.max(0, caretTop - lineHeight);
      } else if (caretTop > visibleBottom) {
        textarea.scrollTop = caretTop - textarea.clientHeight + lineHeight * 2;
      }
    });
  }

  if (noteQuery.isPending) {
    return <p className="muted">Loading note...</p>;
  }

  if (noteQuery.isError) {
    return <p className="error">{noteQuery.error.message}</p>;
  }

  const data = noteQuery.data;
  const note = data.note;
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
        <div className="note-title-row">
          <h2>{note.title}</h2>
          {dirty ? (
            <span className="dirty-indicator" aria-label="Unsaved changes" title="Unsaved changes" />
          ) : null}
        </div>
        <div className="note-header-actions">
          <button
            className={`follow-toggle${followEnabled ? " active" : ""}`}
            type="button"
            aria-pressed={followEnabled}
            onClick={() => setFollowEnabled((value) => !value)}
          >
            Follow
          </button>
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
              ref={textareaRef}
              value={body}
              onChange={(event) => {
                setBody(event.currentTarget.value);
                revealTextareaCaret(event.currentTarget);
              }}
              onClick={(event) => revealTextareaCaret(event.currentTarget)}
              onKeyUp={(event) => revealTextareaCaret(event.currentTarget)}
              onSelect={(event) => revealTextareaCaret(event.currentTarget)}
            />
          ) : null}
          {editorMode !== "edit" ? (
            <section className="note-preview" ref={previewRef} aria-label="Rendered note preview">
              <MarkdownView markdown={renderQuery.data?.markdown ?? ""} kind={note.file_kind} />
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

      {/* Backlinks (references) sit on the left and a journal's "On this day" on the right, so the two
          share the reader's width instead of stacking. With only Backlinks (a non-journal note) the
          single column grows to full width, and both wrap to a stack when the reader is narrow. */}
      <div className="note-aside">
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

        {journalDate !== "" ? (
          <section className="backlinks" aria-labelledby="on-this-day-heading">
            <h3 id="on-this-day-heading">On this day</h3>
            {agendaQuery.isPending ? (
              <p className="muted">Loading...</p>
            ) : (
              (() => {
                // Exclude the journal itself so the section lists the other notes touched that day.
                const items = (agendaQuery.data?.notes ?? []).filter((item) => item.note_id !== noteID);
                if (items.length === 0) {
                  return <p className="muted">No notes were worked on this day.</p>;
                }
                return (
                  <div className="backlink-list">
                    {items.map((item) => (
                      <Link
                        className="backlink"
                        key={item.note_id}
                        to="/notes/$noteId"
                        params={{ noteId: String(item.note_id) }}
                      >
                        {item.title}
                      </Link>
                    ))}
                  </div>
                );
              })()
            )}
          </section>
        ) : null}
      </div>
    </article>
  );
}

// journalDateFromNote returns the YYYY-MM-DD a journal note is for, derived from its yyyyMMdd id, or ""
// when the note is not a journal. Journal ids are date-addressed (see ADR 0005), so no extra lookup is
// needed to know which day's activity to show.
function journalDateFromNote(note?: { file_kind: FileKind; note_id: NoteID }): string {
  if (!note || note.file_kind !== "journal") return "";
  const id = String(note.note_id);
  if (!/^\d{8}$/.test(id)) return "";
  return `${id.slice(0, 4)}-${id.slice(4, 6)}-${id.slice(6, 8)}`;
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
