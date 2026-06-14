import { useEffect, useRef, useState } from "react";
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
  const activity = useActivityQuery(visibleDays);
  const className = `activity-panel activity-panel-${variant}`;
  const isHome = variant === "home";

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
  const dates = recentDates(summary.start_date, summary.days);

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
      <div className="activity-grid" aria-label={`Recent ${summary.days} day activity`}>
        {dates.map((date) => {
          const count = counts.get(date) ?? 0;
          return (
            <time
              className="activity-cell"
              data-level={activityLevel(count)}
              data-date={date}
              data-count={count}
              dateTime={date}
              key={date}
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
