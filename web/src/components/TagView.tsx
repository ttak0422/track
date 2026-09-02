import { Link } from "@tanstack/react-router";
import { useNotesQuery } from "../queries";
import { isNew, isStaleDay, lastActivityDay } from "../reading";
import { NoteFlagBadges } from "./noteShared";

// TagView is the page a rendered #tag opens: every note carrying the tag or one of its descendants
// (tags are hierarchical, so /tags/a lists #a/b notes too), in the shared note-list order the notes
// listing already provides. Derived client-side from the one notes listing both deployments have
// (/api/notes live, notes.json static), so no dedicated endpoint or bundle file is needed.
export function TagView({ tag }: { tag: string }) {
  const notesQuery = useNotesQuery();
  const wanted = tag.replace(/\/+$/, "").toLowerCase();

  if (wanted === "") {
    return <p className="error">Invalid tag.</p>;
  }

  const matches = (notesQuery.data?.notes ?? []).filter((note) =>
    (note.tags ?? []).some((t) => {
      const lower = t.toLowerCase();
      return lower === wanted || lower.startsWith(`${wanted}/`);
    }),
  );

  return (
    <div className="day-view" aria-label={`Notes tagged #${tag}`}>
      <header className="day-head">
        <h1 className="day-title">#{wanted}</h1>
      </header>
      {notesQuery.isPending ? <p className="muted">Loading...</p> : null}
      {notesQuery.isError ? <p className="error">{notesQuery.error.message}</p> : null}
      {notesQuery.data ? (
        matches.length === 0 ? (
          <p className="muted">No notes carry this tag.</p>
        ) : (
          <div className="backlink-list day-list">
            {matches.map((note) => {
              const last = lastActivityDay(note.days);
              return (
                <Link
                  className="backlink"
                  key={note.note_id}
                  to="/notes/$noteId"
                  params={{ noteId: String(note.note_id) }}
                >
                  {note.title}
                  {isNew(note.note_id) ? (
                    <span className="note-state-badge note-state-new">NEW</span>
                  ) : null}
                  <NoteFlagBadges flags={note.flags} />
                  {last !== "" && isStaleDay(last) ? (
                    <span className="note-state-badge note-state-stale">古い</span>
                  ) : null}
                </Link>
              );
            })}
          </div>
        )
      ) : null}
    </div>
  );
}
