import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useAgendaQuery, useLocalGraphQuery } from "../queries";
import { GraphCanvas } from "./GraphCanvasLazy";
import { WikiLink } from "./preview/WikiLink";
import type { ExternalRef, FileKind, NoteID, NoteProp, NoteRef, UnavailableVault } from "../types";
import { split, vaultOf } from "../vaultId";

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
  // A note from a named vault carries it in the id, so read the id half — a journal in vault "work"
  // is "work~20260725", and matching the whole string would drop its "on this day" section.
  const id = split(String(note.note_id)).id;
  if (!/^\d{8}$/.test(id)) return "";
  return `${id.slice(0, 4)}-${id.slice(4, 6)}-${id.slice(6, 8)}`;
}

// NoteBreadcrumbs renders the ancestor trail derived from the "up" relation property — root first,
// immediate parent last — as a quiet strip above the note. A note without an up-chain shows nothing.
export function NoteBreadcrumbs({ trail }: { trail: NoteRef[] }) {
  if (trail.length === 0) return null;
  return (
    <nav className="note-breadcrumbs" aria-label="Breadcrumbs">
      {trail.map((ref) => (
        <span className="note-crumb" key={ref.note_id}>
          <Link to="/notes/$noteId" params={{ noteId: String(ref.note_id) }}>
            {ref.title}
          </Link>
          <span className="note-crumb-sep" aria-hidden="true">
            ›
          </span>
        </span>
      ))}
    </nav>
  );
}

// useScrollToHash scrolls the note view to the element the URL hash names — a block anchor,
// id="block-<id>" (see remarkBlockID) — once the rendered body is in the DOM, and marks it with
// .block-target for the arrival highlight. The reader drives this itself because SPA navigation
// does not retrigger native fragment scrolling, and on a direct page load the content mounts after
// the fragment was already consumed. ready flips true when the markdown has rendered.
export function useScrollToHash(ready: boolean) {
  const hash = useLocation({ select: (location) => location.hash });
  useEffect(() => {
    if (!ready || !hash) return;
    const el = document.getElementById(hash.replace(/^#/, ""));
    if (!el) return;
    el.classList.add("block-target");
    el.scrollIntoView({ block: "center" });
    return () => el.classList.remove("block-target");
  }, [ready, hash]);
}

// NoteAside renders a note's backlinks, its hierarchy children (notes whose "up" property points
// here), the note's local link graph, and, for a journal, the other notes touched that day. The
// sections share the reader's width and wrap to a stack when narrow; on a wide viewport CSS docks
// the whole aside as a fixed right rail beside the note column (see the .note-aside rail rules).
export function NoteAside({
  backlinks,
  external = [],
  unavailable = [],
  childNotes = [],
  noteID,
  journalDate,
}: {
  backlinks: NoteRef[];
  // Inbound references from other vaults, listed apart from same-vault backlinks because they are
  // reached by title across a vault boundary rather than by an indexed id.
  external?: ExternalRef[];
  unavailable?: UnavailableVault[];
  childNotes?: NoteRef[];
  noteID: NoteID;
  journalDate: string;
}) {
  const agendaQuery = useAgendaQuery(journalDate, vaultOf(noteID), { enabled: journalDate !== "" });
  const graphQuery = useLocalGraphQuery(noteID);
  const [graphResetToken, setGraphResetToken] = useState(0);
  const navigate = useNavigate();
  const graph = graphQuery.data?.graph;

  return (
    <div className="note-aside">
      {childNotes.length > 0 ? (
        <section className="backlinks" aria-labelledby="children-heading">
          <h3 id="children-heading">Children</h3>
          <div className="backlink-list">
            {childNotes.map((child) => (
              <Link
                className="backlink"
                key={child.note_id}
                to="/notes/$noteId"
                params={{ noteId: String(child.note_id) }}
              >
                {child.title}
              </Link>
            ))}
          </div>
        </section>
      ) : null}

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

      {external.length > 0 || unavailable.length > 0 ? (
        <section className="backlinks" aria-labelledby="external-backlinks-heading">
          <h3 id="external-backlinks-heading">From other vaults</h3>
          <div className="backlink-list">
            {external.map((ref) => (
              <Link
                className="backlink"
                key={`${ref.vault}/${ref.note_id}`}
                to="/notes/$noteId"
                params={{ noteId: String(ref.note_id) }}
              >
                <span className="tab-vault">{ref.vault}</span>
                {ref.title}
              </Link>
            ))}
          </div>
          {unavailable.map((vault) => (
            <p key={vault.name} className="muted">
              ⚠ vault “{vault.name}” could not be checked{vault.error ? `: ${vault.error}` : ""}
            </p>
          ))}
        </section>
      ) : null}

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

      {/* The always-on local graph. A lone unlinked node says nothing the note itself doesn't, so
          the section only appears once the note connects somewhere. */}
      {graph && graph.nodes.length > 1 ? (
        <section className="backlinks note-aside-graph" aria-labelledby="graph-heading">
          <div className="aside-graph-head">
            <h3 id="graph-heading">Graph</h3>
            <button
              className="graph-reset"
              type="button"
              aria-label="Reset graph view"
              title="Reset graph view"
              onClick={() => setGraphResetToken((token) => token + 1)}
            >
              ↺
            </button>
          </div>
          <div className="aside-graph">
            <GraphCanvas
              graph={graph}
              resetToken={graphResetToken}
              onSelect={(selected) =>
                void navigate({ to: "/notes/$noteId", params: { noteId: String(selected) } })
              }
            />
          </div>
        </section>
      ) : null}
    </div>
  );
}

// NoteProperties renders a note's flattened properties (sidecar props and inline "key:: value"
// fields) as a read-only key/value strip above the body. Values group per key in first-seen order,
// so a list value reads as one row; link values navigate like any body wiki link.
// The "up" relation has its own display — the breadcrumb trail and children list — so its link
// values stay out of the strip; a string-typed up is not hierarchy and shows like any property.
export function NoteProperties({ props: noteProps }: { props: NoteProp[] }) {
  const shown = noteProps.filter((p) => !(p.key === "up" && p.type === "link"));
  if (shown.length === 0) return null;
  const keys: string[] = [];
  const byKey = new Map<string, NoteProp[]>();
  for (const prop of shown) {
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

// NoteTags renders a note's tags as links to their tag pages (/tags/<tag>), which list every note
// carrying the tag or one of its descendants (#a/b files under #a).
export function NoteTags({ tags }: { tags: string[] }) {
  if (tags.length === 0) return null;
  return (
    <div className="tag-list note-tags" aria-label="Note tags">
      {tags.map((tag) => (
        <Link key={tag} to="/tags/$" params={{ _splat: tag }}>
          #{tag}
        </Link>
      ))}
    </div>
  );
}
