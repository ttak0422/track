import { Link } from "@tanstack/react-router";
import { useAgendaQuery } from "../queries";
import { WikiLink } from "./preview/WikiLink";
import type { FileKind, NoteID, NoteProp, NoteRef } from "../types";

// Shared read-only note UI, used by both the static reader (NoteReaderStatic) and the live editor
// (NoteEditor), so the two stay consistent and the editor-only code is the only thing that differs.

// LoadingIndicator is the spinner shown while a note (or its render) is still loading, in place of a
// "Loading..." text or MarkdownView's "Empty note." placeholder for a not-yet-rendered body.
export function LoadingIndicator({ label }: { label: string }) {
  return (
    <div className="note-loading" role="status" aria-label={label}>
      <span className="spinner" aria-hidden="true" />
    </div>
  );
}

// journalDateFromNote returns the YYYY-MM-DD a journal note is for, derived from its yyyyMMdd id, or ""
// when the note is not a journal. Journal ids are date-addressed (see ADR 0005), so no extra lookup is
// needed to know which day's activity to show.
export function journalDateFromNote(note?: { file_kind: FileKind; note_id: NoteID }): string {
  if (!note || note.file_kind !== "journal") return "";
  const id = String(note.note_id);
  if (!/^\d{8}$/.test(id)) return "";
  return `${id.slice(0, 4)}-${id.slice(4, 6)}-${id.slice(6, 8)}`;
}

// NoteAside renders a note's backlinks and, for a journal, the other notes touched that day. Backlinks
// (references) sit on the left and "On this day" on the right, so the two share the reader's width; a
// non-journal note's single Backlinks column grows to full width, and both wrap to a stack when narrow.
export function NoteAside({
  backlinks,
  noteID,
  journalDate,
}: {
  backlinks: NoteRef[];
  noteID: NoteID;
  journalDate: string;
}) {
  const agendaQuery = useAgendaQuery(journalDate, { enabled: journalDate !== "" });

  return (
    <div className="note-aside">
      <section className="backlinks" aria-labelledby="backlinks-heading">
        <h3 id="backlinks-heading">Backlinks</h3>
        {backlinks.length === 0 ? (
          <p className="muted">No backlinks.</p>
        ) : (
          // Cap the height so a heavily linked note does not push the rest of the page away; the list
          // scrolls past that point.
          <div className="backlink-list">
            {backlinks.map((backlink) => (
              <Link
                className="backlink"
                key={backlink.note_id}
                to="/notes/$noteId"
                params={{ noteId: String(backlink.note_id) }}
              >
                {backlink.title}
              </Link>
            ))}
          </div>
        )}
      </section>

      {journalDate !== "" ? (
        <section className="backlinks" aria-labelledby="on-this-day-heading">
          <h3 id="on-this-day-heading">On this day</h3>
          {agendaQuery.isPending ? (
            <p className="muted">Loading...</p>
          ) : (
            (() => {
              // Exclude the journal itself so the section lists the other notes touched that day.
              const items = (agendaQuery.data?.notes ?? []).filter((item) => item.note_id !== noteID);
              if (items.length === 0) {
                return <p className="muted">No notes were worked on this day.</p>;
              }
              return (
                <div className="backlink-list">
                  {items.map((item) => (
                    <Link
                      className="backlink"
                      key={item.note_id}
                      to="/notes/$noteId"
                      params={{ noteId: String(item.note_id) }}
                    >
                      {item.title}
                    </Link>
                  ))}
                </div>
              );
            })()
          )}
        </section>
      ) : null}
    </div>
  );
}

// NoteProperties renders a note's flattened properties (sidecar props and inline "key:: value"
// fields) as a read-only key/value strip above the body. Values group per key in first-seen order,
// so a list value reads as one row; link values navigate like any body wiki link.
export function NoteProperties({ props: noteProps }: { props: NoteProp[] }) {
  if (noteProps.length === 0) return null;
  const keys: string[] = [];
  const byKey = new Map<string, NoteProp[]>();
  for (const prop of noteProps) {
    const group = byKey.get(prop.key);
    if (group) {
      group.push(prop);
    } else {
      byKey.set(prop.key, [prop]);
      keys.push(prop.key);
    }
  }
  return (
    <dl className="note-props" aria-label="Note properties">
      {keys.map((key) => (
        <div className="note-prop" key={key}>
          <dt>{key}</dt>
          <dd>
            {(byKey.get(key) ?? []).map((prop, i) => (
              <span className={`note-prop-value note-prop-${prop.type}`} key={i}>
                {prop.type === "link" ? <WikiLink target={prop.value} display={prop.value} /> : prop.value}
              </span>
            ))}
          </dd>
        </div>
      ))}
    </dl>
  );
}

// NoteTags renders a note's tags as buttons that seed a #tag search.
export function NoteTags({ tags, onTag }: { tags: string[]; onTag: (tag: string) => void }) {
  if (tags.length === 0) return null;
  return (
    <div className="tag-list note-tags" aria-label="Note tags">
      {tags.map((tag) => (
        <button key={tag} type="button" onClick={() => onTag(`#${tag}`)}>
          #{tag}
        </button>
      ))}
    </div>
  );
}
