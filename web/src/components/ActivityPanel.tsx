import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { openJournal } from "../api";
import { useActivityQuery } from "../queries";
import { monthColumnLabels, weekAlignedDates } from "./activityDates";

const cellWidth = 9;
const cellGap = 3;
// Sunday-first rows, captioned every other row like GitHub's graph: three names read as a scale, seven
// read as a list and crowd a 9px row pitch. The row numbers are the grid rows they sit on.
const weekdayLabels = [
  { row: 2, label: "Mon" },
  { row: 4, label: "Wed" },
  { row: 6, label: "Fri" },
];

interface ActivityPanelProps {
  variant?: "sidebar" | "home";
}

export function ActivityPanel({ variant = "sidebar" }: ActivityPanelProps) {
  const panelRef = useRef<HTMLElement | null>(null);
  const gridRef = useRef<HTMLDivElement | null>(null);
  const [weeks, setWeeks] = useState(4);
  const [hovered, setHovered] = useState<{ date: string; count: number } | null>(null);
  // The rendered window is week-aligned like GitHub's graph — see weekAlignedDates — and the
  // activity endpoint takes the same [since, until] range.
  const dates = weekAlignedDates(new Date(), weeks);
  const since = dates[0];
  const until = dates[dates.length - 1];
  const activity = useActivityQuery(since, until);
  const navigate = useNavigate();
  const className = `activity-panel activity-panel-${variant}`;
  const isHome = variant === "home";

  // Clicking a day opens (creating if needed) that day's journal and navigates to it, so the heatmap is
  // the entry point to a day's work log.
  async function openDay(date: string) {
    try {
      const { note_id } = await openJournal(date);
      navigate({ to: "/notes/$noteId", params: { noteId: String(note_id) } });
    } catch {
      // Surfacing a toast is out of scope; a failed open simply leaves the user on the current view.
    }
  }

  // Measured on the cell grid rather than the panel: the grid fills the track left over once the weekday
  // gutter has taken its share, so its own width already answers how many weeks fit — no arithmetic over
  // the panel's padding and no constant shadowing the gutter's rendered width. The count cannot feed back
  // into that width (the track is 1fr), so this settles in one pass.
  useEffect(() => {
    const grid = gridRef.current;
    if (!grid) return;
    const observedGrid = grid;

    function updateDays() {
      const width = observedGrid.clientWidth;
      setWeeks(Math.max(1, Math.floor((width + cellGap) / (cellWidth + cellGap))));
    }

    updateDays();
    const observer = new ResizeObserver(updateDays);
    observer.observe(observedGrid);
    return () => observer.disconnect();
    // The grid only exists once the query has answered, so the observer attaches on that transition.
  }, [activity.isPending, activity.isError]);

  if (activity.isPending) {
    return (
      <section className={className} aria-labelledby="activity-heading" ref={panelRef}>
        <h2 className={isHome ? "sr-only" : undefined} id="activity-heading">
          Activity
        </h2>
        <p className="muted">Loading activity...</p>
      </section>
    );
  }

  if (activity.isError) {
    return (
      <section className={className} aria-labelledby="activity-heading" ref={panelRef}>
        <h2 className={isHome ? "sr-only" : undefined} id="activity-heading">
          Activity
        </h2>
        <p className="error">{activity.error.message}</p>
      </section>
    );
  }

  const summary = activity.data.activity;
  const counts = new Map(summary.counts.map((day) => [day.date, day.count]));

  return (
    <section className={className} aria-labelledby="activity-heading" ref={panelRef}>
      {isHome ? (
        <h2 className="sr-only" id="activity-heading">
          Activity
        </h2>
      ) : (
        <div className="activity-header">
          <h2 id="activity-heading">Activity</h2>
          <p>{summary.total} updates</p>
        </div>
      )}
      {/* The captions are a scale for the cells, and every cell already names its own date, so to a
          screen reader they are decoration rather than something to read twice. */}
      <div className="activity-chart">
        <div className="activity-months" aria-hidden="true">
          {monthColumnLabels(dates).map(({ column, label }) => (
            <span key={column} style={{ gridColumn: column + 1 }}>
              {label}
            </span>
          ))}
        </div>
        <div className="activity-weekdays" aria-hidden="true">
          {weekdayLabels.map(({ row, label }) => (
            <span key={label} style={{ gridRow: row }}>
              {label}
            </span>
          ))}
        </div>
        <div className="activity-grid" ref={gridRef} aria-label={`Recent ${dates.length} day activity`}>
          {dates.map((date) => {
            const count = counts.get(date) ?? 0;
            // A day with activity has a journal (ensured when its notes were created/edited), so the cell
            // opens it. An empty day has nothing to open and offers no creation path; it stays hoverable
            // (to read its 0 count) but is not actionable.
            const active = count > 0;
            return (
              <button
                type="button"
                className="activity-cell"
                data-level={activityLevel(count)}
                data-empty={active ? undefined : ""}
                data-date={date}
                data-count={count}
                key={date}
                aria-label={active ? `${date}: ${count} ${contributionLabel(count)} — open journal` : `${date}: no activity`}
                tabIndex={active ? undefined : -1}
                onClick={active ? () => openDay(date) : undefined}
                onMouseEnter={() => setHovered({ date, count })}
                onMouseLeave={() => setHovered(null)}
                title={
                  isHome
                    ? `${date}: ${count} ${contributionLabel(count)}`
                    : `${date}: ${count} updates`
                }
              />
            );
          })}
        </div>
      </div>
      {isHome ? (
        <p className="activity-hover" aria-live="polite">
          {hovered ? `${hovered.date}: ${hovered.count} ${contributionLabel(hovered.count)}` : ""}
        </p>
      ) : null}
    </section>
  );
}

function activityLevel(count: number): number {
  if (count <= 0) return 0;
  if (count === 1) return 1;
  if (count <= 3) return 2;
  if (count <= 6) return 3;
  return 4;
}

function contributionLabel(count: number): string {
  return count === 1 ? "contribution" : "contributions";
}
