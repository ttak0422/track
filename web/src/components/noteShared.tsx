import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { type RefObject, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useAgendaQuery, useLocalGraphQuery } from "../queries";
import { isNew } from "../reading";
import { dateKey } from "./activityDates";
import { applyListCaps } from "./asideSpace";
import { GraphCanvas } from "./GraphCanvasLazy";
import { headingElementID, tocEntries } from "./markdown/toc";
import { WikiLink } from "./preview/WikiLink";
import type { ExternalRef, FileKind, NoteID, NoteProp, NoteRef, UnavailableVault } from "../types";
import { split, vaultOf } from "../vaultId";
import { IconMaximize, IconRotate2, IconX, RailIcon } from "./icons";

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

// NoteStamps overlays a note's article with its author-assigned flags (ADR 0074): the classic red
// English-text stamp, uppercase and slightly rotated, transparent enough to keep reading and
// pointer-transparent so it never blocks selection or editing. Each flag stacks down the article's
// right edge (the per-stamp top offset comes from here, not CSS, so the stack scales with the flag
// list rather than a sibling rule). Callers anchor it inside the positioned article.
export function NoteStamps({ flags }: { flags?: string[] }) {
  if (!flags || flags.length === 0) return null;
  return (
    <div className="note-stamps" aria-hidden="true">
      {flags.map((flag, index) => (
        <span
          key={flag}
          className={`stamp stamp-${flag.toLowerCase()}`}
          style={{ top: `${24 + index * 76}px` }}
        >
          {flag}
        </span>
      ))}
    </div>
  );
}

// NoteFlagBadges renders a note's author-assigned flags as small label chips in list rows — the same
// inline treatment as the NEW/stale state badges, in the flags' danger red.
export function NoteFlagBadges({ flags }: { flags?: string[] }) {
  if (!flags || flags.length === 0) return null;
  return (
    <>
      {flags.map((flag) => (
        <span key={flag} className={`note-flag-badge note-flag-badge-${flag.toLowerCase()}`}>
          {flag}
        </span>
      ))}
    </>
  );
}

// useScrollToHash scrolls the note view to the element the URL hash names — a block anchor,
// id="block-<id>" (see remarkBlockID), or a heading id="h-<slug>" — once the rendered body is in the
// DOM, and marks it with .block-target for the arrival highlight. The reader drives this itself because
// SPA navigation does not retrigger native fragment scrolling, and on a direct page load the content
// mounts after the fragment was already consumed. ready flips true when the markdown has rendered.
export function useScrollToHash(ready: boolean) {
  const hash = useLocation({ select: (location) => location.hash });
  useEffect(() => {
    if (!ready || !hash) return;
    const rawID = hash.replace(/^#/, "");
    const el = document.getElementById(rawID) ?? document.getElementById(headingElementID(rawID));
    if (!el) return;
    el.classList.add("block-target");
    el.scrollIntoView({ block: "center" });
    return () => el.classList.remove("block-target");
  }, [ready, hash]);
}

// useAsideSpace spends the room the docked rail has left over. Every capped list shows the same 320px
// however tall the screen is, so a tall display reads a column of stubs with the room below them going
// to waste; this hands that room to the lists still cut off (see asideSpace for the rule).
//
// The sizes are measured rather than computed: heading heights, the font scale, and the content width
// setting all move them, and a constant here would drift from the stylesheet. Nothing shrinks — below
// the cap the aside behaves exactly as it always has, and the rail's own scroll stays the safety net.
function useAsideSpace(railRef: RefObject<HTMLDivElement | null>) {
  // After layout on every render, since the aside's content arrives with the note's queries and a rail
  // already at its bound reports no resize when its content changes. The caps are written to the DOM
  // rather than to state, so measuring re-renders nothing and cannot chase itself.
  useLayoutEffect(() => {
    const rail = railRef.current;
    if (!rail) return;

    const measure = () => applyListCaps(rail);

    measure();
    // A shorter window changes what the rail may take without changing the rail itself.
    window.addEventListener("resize", measure);
    // ...and the content width setting changes the rail without touching the window.
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(rail);
    return () => {
      window.removeEventListener("resize", measure);
      observer?.disconnect();
    };
  });
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
  const railRef = useRef<HTMLDivElement>(null);
  useAsideSpace(railRef);
  const graph = graphQuery.data?.graph;
  // A single heading is not an outline — it is the note's own title restated — so the section only
  // appears once there is somewhere to navigate between.
  const toc = useMemo(() => tocEntries(markdown), [markdown]);
  // Indentation is measured from the outline's own shallowest heading, not from h1: a note whose
  // title is its metadata starts its body at "##", and that "##" is the top of this outline rather
  // than one level into a heading the note does not have.
  const topLevel = toc.length > 0 ? Math.min(...toc.map((entry) => entry.level)) : 1;

  // The lightbox <dialog> mounts only while enlarged; showModal() must run after that mount, so it
  // lives in an effect rather than the click handler.
  useEffect(() => {
    if (graphEnlarged) graphDialogRef.current?.showModal();
  }, [graphEnlarged]);

  return (
    <div className="note-aside" ref={railRef}>
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
                style={
                  entry.level > topLevel
                    ? { paddingLeft: `${(entry.level - topLevel) * 2}em` }
                    : undefined
                }
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
        {/* The count rides the heading rather than the note's meta strip: backlinks have a display of
            their own here, and saying "6" twice on one screen is one place too many to keep true. */}
        <h3 id="backlinks-heading">
          Backlinks
          {backlinks.length > 0 ? (
            <span className="backlinks-count">{String(backlinks.length).padStart(2, "0")}</span>
          ) : null}
        </h3>
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
                {isNew(backlink.note_id) ? (
                  <span className="note-state-badge note-state-new">NEW</span>
                ) : null}
                <NoteFlagBadges flags={backlink.flags} />
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
                      {isNew(item.note_id) ? (
                        <span className="note-state-badge note-state-new">NEW</span>
                      ) : null}
                      <NoteFlagBadges flags={item.flags} />
                    </Link>
                  ))}
                </div>
              );
            })()
          )}
        </section>
      ) : null}

      {/* The always-on local graph. A lone unlinked node says nothing the note itself doesn't, so
          the section only appears once the note connects somewhere. It is labelled like the lists
          above it: three sections down a column read as one stack, and the odd one out reads
          as something that fell off the end of the note (design.md, Sidebar). */}
      {graph && graph.nodes.length > 1 ? (
        <section className="backlinks note-aside-graph" aria-labelledby="local-graph-heading">
          <div className="aside-graph-heading">
            <h3 id="local-graph-heading">Graph</h3>
            {/* Both controls end the heading row, where the other sections carry their count — over the
                canvas they were glyphs floating on nothing. They remain siblings of the heading so
                the heading a screen reader announces stays "Graph". */}
            <div className="aside-graph-controls">
              <button
                className="graph-reset aside-graph-reset"
                type="button"
                aria-label="Reset graph view"
                title="Reset graph view"
                onClick={() => setGraphResetToken((token) => token + 1)}
              >
                <GraphResetIcon />
              </button>
              <button
                className="graph-reset aside-graph-expand"
                type="button"
                aria-label="Enlarge graph"
                title="Enlarge graph"
                onClick={() => setGraphEnlarged(true)}
              >
                {/* Expand-to-corners glyph (tabler maximize), the same one media embeds use to enlarge. */}
                <RailIcon Icon={IconMaximize} size={15} />
              </button>
            </div>
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
              {/* The way out. Esc and a click past the dialog still close it, but this one fills the
                  window: what is left to click past is a few pixels of backdrop, which a thumb
                  cannot aim at at all. Same corner as the diagram lightbox's (.lightbox-close). */}
              <button
                className="graph-reset lightbox-close"
                type="button"
                aria-label="Close enlarged graph"
                title="Close enlarged graph"
                onClick={() => graphDialogRef.current?.close()}
              >
                <CloseIcon />
              </button>
              <div className="graph-controls">
                <button
                  className="graph-reset"
                  type="button"
                  aria-label="Reset graph view"
                  title="Reset graph view"
                  onClick={() => setLightboxResetToken((token) => token + 1)}
                >
                  <GraphResetIcon />
                </button>
              </div>
            </dialog>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}

function CloseIcon() {
  return <RailIcon Icon={IconX} size={15} />;
}

function GraphResetIcon() {
  return <RailIcon Icon={IconRotate2} size={15} />;
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
