import { Link } from "@tanstack/react-router";
import { useNewNotesQuery } from "../queries";

// NewNotes is the vault's creation history. It deliberately differs from RecentNotes: New answers
// "what was added to the vault?", while Recent remembers what this browser opened.
export function NewNotes() {
  const query = useNewNotesQuery(10);
  const notes = query.data?.notes ?? [];
  if (notes.length === 0) {
    return null;
  }
  return (
    <section className="backlinks home-note-list home-new" aria-labelledby="new-heading">
      <h3 id="new-heading">New</h3>
      <div className="backlink-list">
        {notes.map((note) => (
          <Link className="backlink" key={note.note_id} to="/notes/$noteId" params={{ noteId: String(note.note_id) }}>
            {note.title || note.note_id}
          </Link>
        ))}
      </div>
    </section>
  );
}
