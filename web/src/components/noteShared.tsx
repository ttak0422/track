import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect, useMemo, useRef, useState } from "react";
import { useAgendaQuery, useLocalGraphQuery } from "../queries";
import { dateKey } from "./activityDates";
import { GraphCanvas } from "./GraphCanvasLazy";
import { headingElementID, tocEntries } from "./markdown/toc";
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
// sections share the reader's width and wrap to a stack when narrow; on a wide viewport CSS lays
// the whole aside beside the note column as a sticky right rail (see the .note-aside rail rules).
export function NoteAside({
  backlinks,
  external = [],
  unavailable = [],
  childNotes = [],
  noteID,
  journalDate,
  markdown = "",
  tags = [],
}: {
  backlinks: NoteRef[];
  // Inbound references from other vaults, listed apart from same-vault backlinks because they are
  // reached by title across a vault boundary rather than by an indexed id.
  external?: ExternalRef[];
  unavailable?: UnavailableVault[];
  childNotes?: NoteRef[];
  noteID: NoteID;
  journalDate: string;
  // The note's rendered body, for the Contents outline. Optional so a caller without one simply
  // gets no outline rather than a broken aside.
  markdown?: string;
  // The note's tags. They live here rather than above the body: they are metadata about the note,
  // like its backlinks and its outline, not part of what it says.
  tags?: string[];
}) {
  const agendaQuery = useAgendaQuery(journalDate, vaultOf(noteID), { enabled: journalDate !== "" });
  const graphQuery = useLocalGraphQuery(noteID);
  const [graphResetToken, setGraphResetToken] = useState(0);
  // The enlarged graph's own state: a separate reset so the aside and the lightbox don't disturb each
  // other's view, and the dialog element for the modal (mounted only while open, like MediaFrame's).
  const [graphEnlarged, setGraphEnlarged] = useState(false);
  const [lightboxResetToken, setLightboxResetToken] = useState(0);
  const graphDialogRef = useRef<HTMLDialogElement>(null);
  const navigate = useNavigate();
  const graph = graphQuery.data?.graph;
  // A single heading is not an outline — it is the note's own title restated — so the section only
  // appears once there is somewhere to navigate between.
  const toc = useMemo(() => tocEntries(markdown), [markdown]);

  // The lightbox <dialog> mounts only while enlarged; showModal() must run after that mount, so it
  // lives in an effect rather than the click handler.
  useEffect(() => {
    if (graphEnlarged) graphDialogRef.current?.showModal();
  }, [graphEnlarged]);

  return (
    <div className="note-aside">
      {tags.length > 0 ? (
        <section className="backlinks note-aside-tags" aria-labelledby="tags-heading">
          <h3 id="tags-heading">Tags</h3>
          <div className="tag-list">
            {tags.map((tag) => (
              <Link key={tag} to="/tags/$" params={{ _splat: tag }}>
                #{tag}
              </Link>
            ))}
          </div>
        </section>
      ) : null}

      {toc.length > 1 ? (
        <section className="backlinks note-toc" aria-labelledby="contents-heading">
          <h3 id="contents-heading">Contents</h3>
          <div className="backlink-list">
            {toc.map((entry) => (
              <a
                className="backlink note-toc-entry"
                key={entry.id}
                href={`#${headingElementID(entry.id)}`}
                style={entry.level > 1 ? { paddingLeft: `${(entry.level - 1) * 12}px` } : undefined}
              >
                {entry.text}
              </a>
            ))}
          </div>
        </section>
      ) : null}
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
          the section only appears once the note connects somewhere. It carries no caption: a graph is
          recognisably a graph, while the sections above are lists that a caption tells apart. */}
      {graph && graph.nodes.length > 1 ? (
        <section className="backlinks note-aside-graph" aria-label="Local graph">
          <div className="aside-graph">
            {/* Over the graph, like the floating panel's and the full view's controls — the aside was
                the one graph surface spending a row of chrome on a single button. The enlarge button
                mirrors the reset at the opposite corner. */}
            <button
              className="graph-reset aside-graph-reset"
              type="button"
              aria-label="Reset graph view"
              title="Reset graph view"
              onClick={() => setGraphResetToken((token) => token + 1)}
            >
              ↺
            </button>
            <button
              className="graph-reset aside-graph-expand"
              type="button"
              aria-label="Enlarge graph"
              title="Enlarge graph"
              onClick={() => setGraphEnlarged(true)}
            >
              {/* Expand-to-corners glyph, the same one media embeds use to enlarge. */}
              <svg
                viewBox="0 0 24 24"
                width="15"
                height="15"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3M3 16v3a2 2 0 0 0 2 2h3m8 0h3a2 2 0 0 0 2-2v-3" />
              </svg>
            </button>
            <GraphCanvas
              graph={graph}
              resetToken={graphResetToken}
              onSelect={(selected) =>
                void navigate({ to: "/notes/$noteId", params: { noteId: String(selected) } })
              }
            />
          </div>
          {graphEnlarged ? (
            /* The enlarged local graph: a modal <dialog> centered over the page, sized from the
               viewport (the canvas fills its container, so it needs real geometry to fill). Esc or
               a backdrop click closes it, like the media lightbox. Selecting a node navigates, which
               drops the dialog rather than leaving it open over a different note. */
            <dialog
              ref={graphDialogRef}
              className="graph-lightbox"
              aria-label="Enlarged local graph"
              onClose={() => setGraphEnlarged(false)}
              onClick={(event) => {
                // A backdrop click lands on the dialog element itself (content clicks land on children).
                if (event.target === graphDialogRef.current) graphDialogRef.current.close();
              }}
            >
              <GraphCanvas
                graph={graph}
                resetToken={lightboxResetToken}
                onSelect={(selected) => {
                  setGraphEnlarged(false);
                  void navigate({ to: "/notes/$noteId", params: { noteId: String(selected) } });
                }}
              />
              <div className="graph-controls">
                <button
                  className="graph-reset"
                  type="button"
                  aria-label="Reset graph view"
                  title="Reset graph view"
                  onClick={() => setLightboxResetToken((token) => token + 1)}
                >
                  ↺
                </button>
              </div>
            </dialog>
          ) : null}
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
// The note's own dates close the strip, after the user's props so user content leads: created is the
// sidecar string verbatim (its format is the vault's), updated the file mtime at the same day
// precision.
export function NoteProperties({
  props: noteProps,
  created,
  updated,
}: {
  props: NoteProp[];
  created?: string;
  updated?: number;
}) {
  const shown = noteProps.filter((p) => !(p.key === "up" && p.type === "link"));
  const dates: [string, string][] = [];
  if (created) dates.push(["created", created]);
  if (updated) dates.push(["updated", dateKey(new Date(updated * 1000))]);
  if (shown.length === 0 && dates.length === 0) return null;
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
      {dates.map(([key, value]) => (
        <div className="note-prop" key={key}>
          <dt>{key}</dt>
          <dd>
            <span className="note-prop-value note-prop-date">{value}</span>
          </dd>
        </div>
      ))}
    </dl>
  );
}

