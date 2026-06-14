import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useSearchQuery } from "../queries";
import type { SearchResult } from "../types";

export function SearchPanel() {
  const [query, setQuery] = useState("");
  const debouncedQuery = useDebouncedValue(query, 180);
  const search = useSearchQuery(debouncedQuery, 100);

  return (
    <section className="search-panel" aria-label="Search notes">
      <label className="searchbox">
        <span className="sr-only">Search notes</span>
        <input
          type="search"
          placeholder="Search notes"
          value={query}
          onChange={(event) => setQuery(event.currentTarget.value)}
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
    <article className="result">
      <Link className="result-title" to="/notes/$noteId" params={{ noteId: String(note.note_id) }}>
        {note.title}
      </Link>
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
    </article>
  );
}
