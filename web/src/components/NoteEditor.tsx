import { useBlocker, useNavigate } from "@tanstack/react-router";
import { FormEvent, useEffect, useRef, useState } from "react";
import { MarkdownView } from "./MarkdownView";
import { LoadingIndicator, NoteAside, NoteTags, journalDateFromNote } from "./noteShared";
import { getFollowState } from "../api";
import { NoteMetaDialog } from "./NoteMetaDialog";
import { NoteActionsMenu } from "./NoteActionsMenu";
import { useDeleteNoteMutation, useNoteQuery, useRenderQuery, useSaveNoteMutation } from "../queries";
import { useSearchState } from "../searchState";
import { useTabs } from "./tabs/tabsStore";
import type { FollowState, NoteID } from "../types";

interface NoteEditorProps {
  noteID: NoteID;
}

type EditorMode = "preview" | "edit" | "split";
const editorModes: EditorMode[] = ["preview", "edit", "split"];
const unsavedChangesMessage = "保存していない変更は失われます。移動しますか？";

// NoteEditor is the live workspace's editable note view — read, edit (textarea/split), save, delete,
// follow. NoteReader renders it only when !__TRACK_STATIC__, so it and its editing dependencies are
// tree-shaken from the read-only static bundle (which uses NoteReaderStatic instead).
export function NoteEditor({ noteID }: NoteEditorProps) {
  // Poll so the note reflects edits made elsewhere; we still guard against
  // clobbering unsaved local edits below.
  const noteQuery = useNoteQuery(noteID, { live: true });
  const saveNote = useSaveNoteMutation(noteID);
  const deleteNote = useDeleteNoteMutation(noteID);
  const { setQuery } = useSearchState();
  const { setTitle: setTabTitle, setDirty: setTabDirty, close: closeTab } = useTabs();
  const navigate = useNavigate();
  // For a journal, surface the notes worked on that day. The day comes from the journal id (yyyyMMdd).
  const journalDate = journalDateFromNote(noteQuery.data?.note);
  // Seed the editor body from the note if it is already in cache at mount (prerender/hydration, or a
  // warm query cache) so the very first render shows content instead of an empty preview waiting on the
  // adopt effect below. When the note is not cached yet (a cold live load) this is "" and the effect
  // adopts it once it arrives — unchanged behavior.
  const cachedNote = noteQuery.data?.note;
  const [body, setBody] = useState(() => cachedNote?.body ?? "");
  const [editorMode, setEditorMode] = useState<EditorMode>("preview");
  const [followEnabled, setFollowEnabled] = useState(false);
  // Delete confirmation: the user must retype the title (GitHub-style) before the note can be removed.
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);
  // Page-metadata dialog (description / cover image for the published og: tags).
  const [metaOpen, setMetaOpen] = useState(false);
  const [deleteConfirmText, setDeleteConfirmText] = useState("");
  // Set just before the post-delete navigation so the unsaved-changes blocker does not fire on the way
  // out (the note no longer exists, so any pending edits are moot).
  const deletedRef = useRef(false);
  // The preview renders server-sanitized Markdown (action links flattened, wiki links kept) rather than
  // the raw body, so track-specific rules live only in the engine. The body is posted as you type.
  const renderQuery = useRenderQuery(body);
  // The note/body/etag last adopted from disk. Edits are "dirty" relative to this, and
  // saves use this etag so a background reload cannot mask a conflicting change. noteID is
  // tracked so switching notes always reloads, even with unsaved edits to the previous note.
  // Initialized to match the seeded body so a cache-warm first render is not falsely "dirty".
  const loadedRef = useRef({ noteID, body: cachedNote?.body ?? "", etag: cachedNote?.etag ?? "" });
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
      if (deletedRef.current || !dirty || current.pathname === next.pathname) return false;
      return !window.confirm(unsavedChangesMessage);
    },
    enableBeforeUnload: () => dirty && !deletedRef.current,
    disabled: !dirty,
  });

  // The tab bar owns the note's title and unsaved-edit indicator (the reader no longer renders its own
  // header), so keep its tab in sync. Only the active note can be dirty, so dirtiness is a single id.
  const noteTitle = noteQuery.data?.note.title;
  useEffect(() => {
    if (noteTitle) setTabTitle(noteID, noteTitle);
  }, [noteID, noteTitle, setTabTitle]);

  useEffect(() => {
    setTabDirty(dirty ? noteID : null);
  }, [dirty, noteID, setTabDirty]);

  // Leaving the reader entirely (home/graph) drops the dirty marker; a closed tab discards its edits.
  useEffect(() => () => setTabDirty(null), [setTabDirty]);

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
    return <LoadingIndicator label="Loading note" />;
  }

  if (noteQuery.isError) {
    // The note may be gone (deleted via the CLI/Neovim, or a stale tab restored from localStorage). We
    // cannot tell a 404 from a transient error here, so leave the tab in place but offer to close it —
    // otherwise a tab with no resolvable title is awkward to dismiss from the strip alone.
    return (
      <div className="note-error">
        <p className="error">{noteQuery.error.message}</p>
        <button type="button" onClick={() => closeTab(noteID)}>
          Close tab
        </button>
      </div>
    );
  }

  const data = noteQuery.data;
  const note = data.note;
  const changedOnDisk = note.etag !== loadedRef.current.etag;
  const tags = note.tags ?? [];

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

  const deleteConfirmed = deleteConfirmText.trim() === note.title.trim();

  async function confirmDelete() {
    if (!deleteConfirmed || deleteNote.isPending) return;
    try {
      await deleteNote.mutateAsync();
      // Skip the unsaved-changes guard, drop the dirty marker, then close this note's tab — which moves
      // to a neighbouring tab (or home when none remain).
      deletedRef.current = true;
      setTabDirty(null);
      closeTab(noteID);
    } catch {
      // Errors surface via deleteNote.isError in the dialog.
    }
  }

  return (
    <article className={`note-reader${editorMode === "split" ? " note-reader-split" : ""}`}>
      {/* Note controls float over the reader as a graph-style overlay, not in the header bar. */}
      <div className="note-float-controls">
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
          <NoteActionsMenu
            body={body}
            onMeta={() => setMetaOpen(true)}
            onDelete={() => {
              setDeleteConfirmText("");
              deleteNote.reset();
              setConfirmDeleteOpen(true);
            }}
          />
        </div>

      {metaOpen ? <NoteMetaDialog noteID={noteID} onClose={() => setMetaOpen(false)} /> : null}

      {confirmDeleteOpen ? (
        <div
          className="modal-backdrop"
          role="dialog"
          aria-modal="true"
          aria-labelledby="delete-note-title"
          onClick={() => setConfirmDeleteOpen(false)}
        >
          {/* Stop backdrop clicks inside the card from dismissing the dialog. */}
          <div className="modal-card" onClick={(event) => event.stopPropagation()}>
            <h3 id="delete-note-title">Delete note</h3>
            <p>
              This permanently deletes <strong>{note.title}</strong> and cannot be undone.
            </p>
            <label className="modal-field">
              <span className="muted">Type the note title to confirm:</span>
              <input
                className="modal-input"
                value={deleteConfirmText}
                /* eslint-disable-next-line jsx-a11y/no-autofocus */
                autoFocus
                onChange={(event) => setDeleteConfirmText(event.currentTarget.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && deleteConfirmed) void confirmDelete();
                  if (event.key === "Escape") setConfirmDeleteOpen(false);
                }}
              />
            </label>
            {deleteNote.isError ? <p className="error">{deleteNote.error.message}</p> : null}
            <div className="modal-actions">
              <button type="button" onClick={() => setConfirmDeleteOpen(false)}>
                Cancel
              </button>
              <button
                type="button"
                className="danger-button"
                disabled={!deleteConfirmed || deleteNote.isPending}
                onClick={confirmDelete}
              >
                {deleteNote.isPending ? "Deleting..." : "Delete note"}
              </button>
            </div>
          </div>
        </div>
      ) : null}
      <NoteTags tags={tags} onTag={setQuery} />

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
              {/* A non-empty body with no render yet is still loading — show a spinner rather than let
                  MarkdownView flash "Empty note." for a body that is not actually empty. */}
              {body.trim() !== "" && renderQuery.data?.markdown === undefined ? (
                <LoadingIndicator label="Loading note" />
              ) : (
                <MarkdownView
                  markdown={renderQuery.data?.markdown ?? ""}
                  kind={note.file_kind}
                  includes={renderQuery.data?.includes}
                />
              )}
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

      <NoteAside backlinks={data.backlinks} noteID={noteID} journalDate={journalDate} />
    </article>
  );
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
