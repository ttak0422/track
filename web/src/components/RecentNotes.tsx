import { Link } from "@tanstack/react-router";
import { useTabs } from "./tabs/tabsStore";

// RecentNotes lists the notes this browser opened most recently, for the search hero: the landing
// screen otherwise offers only search, so getting back to what you were just reading meant retyping
// its title. The list is the browser's own history of opened notes (see the tabs store), not the
// vault's recently-updated notes — those are what a home note's ```dashboard recent widget shows,
// and the two answer different questions.
export function RecentNotes() {
  const { recent } = useTabs();
  if (recent.length === 0) {
    return null;
  }
  return (
    <section className="backlinks home-recent" aria-labelledby="recent-heading">
      <h3 id="recent-heading">Recent</h3>
      <div className="backlink-list">
        {recent.map((note) => (
          <Link className="backlink" key={note.id} to="/notes/$noteId" params={{ noteId: String(note.id) }}>
            {note.title || note.id}
          </Link>
        ))}
      </div>
    </section>
  );
}
