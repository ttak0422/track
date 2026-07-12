import { MarkdownView } from "./MarkdownView";
import { LoadingIndicator, NoteAside, NoteProperties, NoteTags, journalDateFromNote } from "./noteShared";
import { useNoteQuery, useRenderQuery } from "../queries";
import { useSearchState } from "../searchState";
import { useTabs } from "./tabs/tabsStore";
import { useEffect } from "react";
import type { NoteID } from "../types";

// NoteReaderStatic is the published site's read-only note view: the title, tags, rendered body, and
// backlinks/on-this-day — no editor, save/delete, follow, or dirty tracking. NoteReader picks it over
// the live editor behind __TRACK_STATIC__, so the editor and its dependencies are tree-shaken from the
// static bundle.
export function NoteReaderStatic({ noteID }: { noteID: NoteID }) {
  const noteQuery = useNoteQuery(noteID);
  const { setQuery } = useSearchState();
  const { setTitle: setTabTitle } = useTabs();
  const note = noteQuery.data?.note;
  const rendered = useRenderQuery(note?.body ?? "");

  // Keep the tab strip's label in sync once the note resolves.
  const noteTitle = note?.title;
  useEffect(() => {
    if (noteTitle) setTabTitle(noteID, noteTitle);
  }, [noteID, noteTitle, setTabTitle]);

  if (noteQuery.isPending) {
    return <LoadingIndicator label="Loading note" />;
  }
  if (noteQuery.isError) {
    return <p className="error">{noteQuery.error.message}</p>;
  }

  const data = noteQuery.data;
  const body = data.note.body;
  const journalDate = journalDateFromNote(data.note);

  return (
    <article className="note-reader">
      <NoteTags tags={data.note.tags ?? []} onTag={setQuery} />
      <NoteProperties props={data.note.props ?? []} />

      <section className="note-preview" aria-label="Rendered note">
        {body.trim() !== "" && rendered.data?.markdown === undefined ? (
          <LoadingIndicator label="Loading note" />
        ) : (
          <MarkdownView
            markdown={rendered.data?.markdown ?? ""}
            kind={data.note.file_kind}
            includes={data.note.includes}
          />
        )}
      </section>

      <NoteAside backlinks={data.backlinks} noteID={noteID} journalDate={journalDate} />
    </article>
  );
}
