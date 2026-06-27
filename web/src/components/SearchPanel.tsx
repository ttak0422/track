import { Link, useNavigate } from "@tanstack/react-router";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useSearchQuery } from "../queries";
import { useSearchState } from "../searchState";
import type { SearchResult } from "../types";

interface SearchPanelProps {
  // Called when a result is chosen (click or Enter), so a host like the sidebar popup can close itself.
  onNavigate?: () => void;
  autoFocus?: boolean;
}

export function SearchPanel({ onNavigate, autoFocus }: SearchPanelProps = {}) {
  const { query, setQuery } = useSearchState();
  const debouncedQuery = useDebouncedValue(query, 180);
  // With no query the home should stay empty rather than listing every note, so the search only runs
  // once something is typed.
  const trimmedQuery = debouncedQuery.trim();
  const hasQuery = trimmedQuery !== "";
  const search = useSearchQuery(trimmedQuery, 100, { enabled: hasQuery });
  const navigate = useNavigate();
  const results = hasQuery ? (search.data?.results ?? []) : [];
  const topResult = results[0];

  function onKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter" && topResult) {
      event.preventDefault();
      void navigate({ to: "/notes/$noteId", params: { noteId: String(topResult.note_id) } });
      onNavigate?.();
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
          autoFocus={autoFocus}
        />
      </label>
      <div className="results" aria-live="polite">
        {hasQuery && search.isPending ? <p className="muted">Loading notes...</p> : null}
        {hasQuery && search.isError ? <p className="error">{search.error.message}</p> : null}
        {results.map((note) => (
          <SearchResultItem key={note.note_id} note={note} onNavigate={onNavigate} />
        ))}
      </div>
    </section>
  );
}

interface SearchResultItemProps {
  note: SearchResult;
  onNavigate?: () => void;
}

function SearchResultItem({ note, onNavigate }: SearchResultItemProps) {
  return (
    <Link
      className="result"
      to="/notes/$noteId"
      params={{ noteId: String(note.note_id) }}
      onClick={() => onNavigate?.()}
    >
      <span className="result-title">
        {note.title}
      </span>
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
