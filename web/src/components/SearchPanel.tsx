import { Link, useNavigate } from "@tanstack/react-router";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useSearchQuery } from "../queries";
import { useSearchState } from "../searchState";
import type { SearchResult } from "../types";

export function SearchPanel() {
  const { query, setQuery } = useSearchState();
  const debouncedQuery = useDebouncedValue(query, 180);
  const search = useSearchQuery(debouncedQuery, 100);
  const navigate = useNavigate();
  const topResult = search.data?.results[0];

  function onKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter" && topResult) {
      event.preventDefault();
      void navigate({ to: "/notes/$noteId", params: { noteId: String(topResult.note_id) } });
    }
  }

  return (
    <section className="search-panel" aria-label="Search notes">
      <label className="searchbox">
        <span className="sr-only">Search notes</span>
        <input
          type="search"
          placeholder="Search notes"
          value={query}
          onChange={(event) => setQuery(event.currentTarget.value)}
          onKeyDown={onKeyDown}
        />
      </label>
      <div className="results" aria-live="polite">
        {search.isPending ? <p className="muted">Loading notes...</p> : null}
        {search.isError ? <p className="error">{search.error.message}</p> : null}
        {search.data?.results.map((note) => (
          <SearchResultItem key={note.note_id} note={note} />
        ))}
      </div>
    </section>
  );
}

interface SearchResultItemProps {
  note: SearchResult;
}

function SearchResultItem({ note }: SearchResultItemProps) {
  return (
    <Link className="result" to="/notes/$noteId" params={{ noteId: String(note.note_id) }}>
      <span className="result-title">
        {note.title}
      </span>
      {note.generated_by_ai ? <span className="badge">AI</span> : null}
      {note.snippet ? <p className="result-snippet">{note.snippet}</p> : null}
      {note.tags && note.tags.length > 0 ? (
        <div className="tag-list" aria-label={`${note.title} tags`}>
          {note.tags.map((tag) => (
            <span key={tag}>
              #{tag}
            </span>
          ))}
        </div>
      ) : null}
    </Link>
  );
}
