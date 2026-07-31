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
  // The server tags each hit with the search that found it; a body hit can carry no snippet (its
  // terms straddled lines), so the tag is the discriminator, not the snippet.
  const titleHits = results.filter((note) => note.match !== "body");
  const bodyHits = results.filter((note) => note.match === "body");
  // Vaults the server could not read. Saying so is the point: otherwise a search that reached only
  // half the vaults looks exactly like one that found nothing in the other half.
  const unavailable = hasQuery ? (search.data?.unavailable ?? []) : [];
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
        {/* Title matches first, then full-text — the ordering the engine composed, kept visible.
            The captions only appear once there is a second group to tell apart, so an ordinary
            title-only search looks exactly as it always has. */}
        {bodyHits.length > 0 && titleHits.length > 0 ? <h3 className="results-group">Titles</h3> : null}
        {titleHits.map((note) => (
          <SearchResultItem key={note.note_id} note={note} onNavigate={onNavigate} />
        ))}
        {bodyHits.length > 0 ? (
          <>
            {titleHits.length > 0 ? <h3 className="results-group">Full text</h3> : null}
            {bodyHits.map((note) => (
              <SearchResultItem key={note.note_id} note={note} onNavigate={onNavigate} />
            ))}
          </>
        ) : null}
        {unavailable.map((vault) => (
          <p key={vault.name} className="muted search-unavailable">
            ⚠ vault “{vault.name}” could not be searched{vault.error ? `: ${vault.error}` : ""}
          </p>
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
        {note.icon ? (
          <span className="note-icon" aria-hidden="true">
            {note.icon}
          </span>
        ) : null}
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
