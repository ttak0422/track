import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { openJournal } from "../api";
import { useActivityQuery } from "../queries";

const cellWidth = 9;
const cellGap = 3;

interface ActivityPanelProps {
  variant?: "sidebar" | "home";
}

export function ActivityPanel({ variant = "sidebar" }: ActivityPanelProps) {
  const panelRef = useRef<HTMLElement | null>(null);
  const [visibleDays, setVisibleDays] = useState(28);
  const [hovered, setHovered] = useState<{ date: string; count: number } | null>(null);
  // Show a window of visibleDays ending today; the activity endpoint takes a generic [since, until] range.
  const until = dateKey(new Date());
  const since = dateKey(daysAgo(visibleDays - 1));
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

  useEffect(() => {
    const panel = panelRef.current;
    if (!panel) return;
    const observedPanel = panel;

    function updateDays() {
      const style = window.getComputedStyle(observedPanel);
      const padding =
        Number.parseFloat(style.paddingLeft || "0") + Number.parseFloat(style.paddingRight || "0");
      const width = Math.max(1, observedPanel.clientWidth - padding);
      const columns = Math.max(1, Math.floor((width + cellGap) / (cellWidth + cellGap)));
      setVisibleDays(columns * 7);
    }

    updateDays();
    const observer = new ResizeObserver(updateDays);
    observer.observe(observedPanel);
    return () => observer.disconnect();
  }, []);

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
  const dates = recentDates(since, visibleDays);

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
      <div className="activity-grid" aria-label={`Recent ${visibleDays} day activity`}>
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
      {isHome ? (
        <p className="activity-hover" aria-live="polite">
          {hovered ? `${hovered.date}: ${hovered.count} ${contributionLabel(hovered.count)}` : ""}
        </p>
      ) : null}
    </section>
  );
}

function daysAgo(days: number): Date {
  const date = new Date();
  date.setDate(date.getDate() - days);
  return date;
}

function recentDates(startDate: string, days: number): string[] {
  const start = parseLocalDate(startDate);
  return Array.from({ length: days }, (_, offset) => {
    const date = new Date(start);
    date.setDate(start.getDate() + offset);
    return dateKey(date);
  });
}

function parseLocalDate(value: string): Date {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, month - 1, day);
}

function dateKey(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
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
