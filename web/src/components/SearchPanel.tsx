import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { keys, step } from "../keys";
import { useSearchQuery } from "../queries";
import { useSearchState } from "../searchState";
import { highlightSearchText } from "../searchHighlight";
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
  const titleHits = results.filter((note) => note.match !== "body" && note.match !== "path");
  const bodyHits = results.filter((note) => note.match === "body");
  // A note the query named by its file rather than by anything it says. The published site never has
  // these — its bundle carries no source paths — so the group simply never appears there.
  const pathHits = results.filter((note) => note.match === "path");
  // Vaults the server could not read. Saying so is the point: otherwise a search that reached only
  // half the vaults looks exactly like one that found nothing in the other half.
  const unavailable = hasQuery ? (search.data?.unavailable ?? []) : [];
  // Keyboard order is what the reader sees: titles, then full text, then the file name.
  const ordered = [...titleHits, ...bodyHits, ...pathHits];
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);

  // A new result set invalidates the old cursor, so start from the top rather than land somewhere
  // arbitrary. The top hit is the one Enter has always taken.
  useEffect(() => {
    setActive(ordered.length > 0 ? 0 : -1);
    // Reset per result set, not per render: the ids are what actually changed.
  }, [ordered.map((note) => note.note_id).join(",")]);

  // Keep the cursor in view when it walks past the edge of the scrolling list.
  useEffect(() => {
    if (active < 0) return;
    listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active]);

  // Choosing a result ends the search, so the field goes back to empty and the host closes. The query
  // is shared state, so leaving it set kept the palette (and the home hero behind it) showing the last
  // search's hits — reopening search looked like it had never been used.
  function chooseResult() {
    setQuery("");
    onNavigate?.();
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (keys.next(event)) {
      event.preventDefault();
      setActive((index) => step(index, 1, ordered.length));
      return;
    }
    if (keys.prev(event)) {
      event.preventDefault();
      setActive((index) => step(index, -1, ordered.length));
      return;
    }
    if (keys.accept(event)) {
      const note = ordered[active];
      if (!note) return;
      event.preventDefault();
      void navigate({ to: "/notes/$noteId", params: { noteId: String(note.note_id) } });
      chooseResult();
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
          aria-activedescendant={active >= 0 && ordered[active] ? `search-result-${ordered[active].note_id}` : undefined}
        />
      </label>
      <div className="results" aria-live="polite" ref={listRef}>
        {hasQuery && search.isPending ? <p className="muted">Loading notes...</p> : null}
        {hasQuery && search.isError ? <p className="error">{search.error.message}</p> : null}
        {/* Title matches first, then full-text — the ordering the engine composed, kept visible.
            The captions only appear once there is a second group to tell apart, so an ordinary
            title-only search looks exactly as it always has. */}
        {bodyHits.length > 0 && titleHits.length > 0 ? <h3 className="results-group">Titles</h3> : null}
        {titleHits.map((note, index) => (
          <SearchResultItem
            key={note.note_id}
            note={note}
            index={index}
            active={index === active}
            query={trimmedQuery}
            onNavigate={chooseResult}
          />
        ))}
        {bodyHits.length > 0 ? (
          <>
            {titleHits.length > 0 ? <h3 className="results-group">Full text</h3> : null}
            {bodyHits.map((note, index) => (
              <SearchResultItem
                key={note.note_id}
                note={note}
                index={titleHits.length + index}
                active={titleHits.length + index === active}
                query={trimmedQuery}
                onNavigate={chooseResult}
              />
            ))}
          </>
        ) : null}
        {pathHits.length > 0 ? (
          <>
            {titleHits.length + bodyHits.length > 0 ? (
              <h3 className="results-group">File name</h3>
            ) : null}
            {pathHits.map((note, index) => (
              <SearchResultItem
                key={note.note_id}
                note={note}
                index={titleHits.length + bodyHits.length + index}
                active={titleHits.length + bodyHits.length + index === active}
                query={trimmedQuery}
                onNavigate={chooseResult}
              />
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
  index: number;
  active: boolean;
  query: string;
  onNavigate?: () => void;
}

function SearchResultItem({ note, index, active, query, onNavigate }: SearchResultItemProps) {
  return (
    <Link
      className={`result${active ? " is-active" : ""}`}
      id={`search-result-${note.note_id}`}
      data-index={index}
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
        <HighlightedSearchText text={note.title} query={query} />
      </span>
      {note.snippet ? (
        <p className="result-snippet">
          <HighlightedSearchText text={note.snippet} query={query} />
        </p>
      ) : null}
      {note.tags && note.tags.length > 0 ? (
        <div className="tag-list" aria-label={`${note.title} tags`}>
          {note.tags.map((tag) => (
            <span key={tag}>
              <HighlightedSearchText text={`#${tag}`} query={query} />
            </span>
          ))}
        </div>
      ) : null}
    </Link>
  );
}

function HighlightedSearchText({ text, query }: { text: string; query: string }) {
  return highlightSearchText(text, query).map((part, index) =>
    part.highlighted ? (
      <mark className="search-highlight" key={index}>
        {part.text}
      </mark>
    ) : (
      part.text
    ),
  );
}
