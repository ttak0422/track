import { useContext } from "react";
import { WikiLink } from "../preview/WikiLink";
import { CodeBlock } from "./CodeBlock";
import { NoteKindContext } from "./context";
import { assetHref } from "./urls";

// QueryView draws fenced ```track-view blocks: the laid-out result of a ```track-query fence whose
// :layout is board, gallery, or calendar. The engine resolves the fence at render time (live) or
// build time (static export) into a View JSON payload — grouping, date bucketing, and covers are all
// decided server-side — so this component only places already-grouped rows on screen. Titles render
// as wiki links, resolving exactly like a [[Title]] cell in the default table layout.

interface ViewRow {
  title: string;
  cells: string[];
  cover?: string;
  icon?: string;
}

interface ViewGroup {
  name?: string;
  rows: ViewRow[];
}

interface ViewPayload {
  layout: string;
  key?: string;
  columns: string[];
  groups: ViewGroup[];
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
// How many titles a calendar day cell lists before collapsing the rest into a "+N" count, matching
// the workspace calendar view.
const CELL_NOTES = 3;

export function QueryView({ text }: { text: string }) {
  const view = parseView(text);
  if (!view) {
    // Not a payload this build understands (hand-written fence, future shape): show the source.
    return <CodeBlock lang="json" text={text} />;
  }
  switch (view.layout) {
    case "board":
      return <Board view={view} />;
    case "gallery":
      return <Gallery view={view} />;
    case "calendar":
      return <Calendar view={view} />;
    default:
      return <CodeBlock lang="json" text={text} />;
  }
}

function parseView(text: string): ViewPayload | null {
  try {
    const parsed: unknown = JSON.parse(text);
    if (
      typeof parsed === "object" &&
      parsed !== null &&
      typeof (parsed as ViewPayload).layout === "string" &&
      Array.isArray((parsed as ViewPayload).columns) &&
      Array.isArray((parsed as ViewPayload).groups)
    ) {
      return parsed as ViewPayload;
    }
  } catch {
    // fall through
  }
  return null;
}

function Board({ view }: { view: ViewPayload }) {
  return (
    <div className="query-view query-board">
      {view.groups.map((group) => (
        <section className="query-lane" key={group.name ?? ""}>
          <header className="query-lane-head">
            <span className="query-lane-name">{group.name}</span>
            <span className="query-lane-count">{group.rows.length}</span>
          </header>
          {group.rows.map((row) => (
            <Card key={row.title} row={row} view={view} skip={view.key} />
          ))}
        </section>
      ))}
    </div>
  );
}

function Gallery({ view }: { view: ViewPayload }) {
  const kind = useContext(NoteKindContext);
  return (
    <div className="query-view query-gallery">
      {view.groups.flatMap((group) =>
        group.rows.map((row) => (
          <article className="query-card query-gallery-card" key={row.title}>
            {row.cover ? (
              <img
                className="query-card-cover"
                src={assetHref(row.cover, kind) ?? row.cover}
                alt=""
                loading="lazy"
              />
            ) : (
              // Most notes never set a cover image; the note's icon is the default card face, so a
              // cover-less gallery still tells its cards apart. With no icon either — the state a
              // fresh vault's notes are in — track's own neutral no-image face fills the slot.
              <div className="query-card-cover query-card-cover-empty" aria-hidden="true">
                {row.icon || <NoImageFace />}
              </div>
            )}
            <CardBody row={row} view={view} />
          </article>
        )),
      )}
    </div>
  );
}

// Calendar reuses the workspace calendar's grid rendering (its calendar-* styles): one month grid
// per month that has rows, days ascending, read-only. Group names are the engine's YYYY-MM-DD keys.
function Calendar({ view }: { view: ViewPayload }) {
  const byDay = new Map<string, ViewRow[]>();
  for (const group of view.groups) {
    if (group.name) byDay.set(group.name, group.rows);
  }
  const months = [...new Set([...byDay.keys()].map((day) => day.slice(0, 7)))].sort();
  return (
    <div className="query-view query-calendar">
      {months.map((month) => (
        <MonthGrid key={month} month={month} byDay={byDay} />
      ))}
    </div>
  );
}

function MonthGrid({ month, byDay }: { month: string; byDay: Map<string, ViewRow[]> }) {
  const year = Number(month.slice(0, 4));
  const monthNo = Number(month.slice(5, 7));
  const daysInMonth = new Date(year, monthNo, 0).getDate();
  // The 1st's weekday is the number of leading blank cells (weeks start on Sunday).
  const leadingBlanks = new Date(year, monthNo - 1, 1).getDay();
  return (
    <section className="query-month">
      <header className="calendar-head">
        <span className="calendar-title">{`${year} / ${pad2(monthNo)}`}</span>
      </header>
      <div className="calendar-grid">
        {WEEKDAYS.map((day) => (
          <span className="calendar-weekday" key={day}>
            {day}
          </span>
        ))}
        {Array.from({ length: leadingBlanks }, (_, i) => (
          <span className="calendar-day calendar-day-blank" key={`blank-${i}`} />
        ))}
        {Array.from({ length: daysInMonth }, (_, i) => {
          const date = `${year}-${pad2(monthNo)}-${pad2(i + 1)}`;
          const rows = byDay.get(date) ?? [];
          return (
            <span className={`calendar-day${rows.length > 0 ? " calendar-day-active" : ""}`} key={date}>
              <span className="calendar-day-number">{i + 1}</span>
              {rows.slice(0, CELL_NOTES).map((row) => (
                <span className="calendar-day-note" key={row.title}>
                  <WikiLink target={row.title} display={row.title} />
                </span>
              ))}
              {rows.length > CELL_NOTES ? <span className="calendar-day-more">+{rows.length - CELL_NOTES}</span> : null}
            </span>
          );
        })}
      </div>
    </section>
  );
}

// NoImageFace is the built-in placeholder pictogram for a card whose note sets neither a cover image
// nor an icon: a muted picture-frame glyph, so an unconfigured gallery reads as "no image yet", not
// as broken.
function NoImageFace() {
  return (
    <svg
      className="query-card-noimage"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <circle cx="9" cy="9" r="2" />
      <path d="m21 15-3.1-3.1a2 2 0 0 0-2.8 0L6 21" />
    </svg>
  );
}

function Card({ row, view, skip }: { row: ViewRow; view: ViewPayload; skip?: string }) {
  return (
    <article className="query-card">
      <CardBody row={row} view={view} skip={skip} />
    </article>
  );
}

// CardBody is the shared card content: the linked title, then one "key value" meta line per
// remaining column — the title column and the lane's own grouping column would only repeat what the
// card's position already says.
function CardBody({ row, view, skip }: { row: ViewRow; view: ViewPayload; skip?: string }) {
  const meta = view.columns
    .map((column, i) => ({ column, value: row.cells[i] ?? "" }))
    .filter(({ column, value }) => column !== "title" && column !== skip && value !== "");
  return (
    <div className="query-card-body">
      <span className="query-card-title">
        <WikiLink target={row.title} display={row.title} />
      </span>
      {meta.map(({ column, value }) => (
        <span className="query-card-meta" key={column}>
          <span className="query-card-meta-key">{column}</span> {value}
        </span>
      ))}
    </div>
  );
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}
