import { useActivityQuery } from "../queries";

interface ActivityPanelProps {
  days?: number;
}

export function ActivityPanel({ days = 28 }: ActivityPanelProps) {
  const activity = useActivityQuery(days);

  if (activity.isPending) {
    return (
      <section className="activity-panel" aria-labelledby="activity-heading">
        <h2 id="activity-heading">Activity</h2>
        <p className="muted">Loading activity...</p>
      </section>
    );
  }

  if (activity.isError) {
    return (
      <section className="activity-panel" aria-labelledby="activity-heading">
        <h2 id="activity-heading">Activity</h2>
        <p className="error">{activity.error.message}</p>
      </section>
    );
  }

  const summary = activity.data.activity;
  const counts = new Map(summary.counts.map((day) => [day.date, day.count]));
  const dates = recentDates(summary.start_date, summary.days);

  return (
    <section className="activity-panel" aria-labelledby="activity-heading">
      <div className="activity-header">
        <h2 id="activity-heading">Activity</h2>
        <p>{summary.total} updates</p>
      </div>
      <div className="activity-grid" aria-label={`Recent ${summary.days} day activity`}>
        {dates.map((date) => {
          const count = counts.get(date) ?? 0;
          return (
            <time
              className="activity-cell"
              data-level={activityLevel(count)}
              dateTime={date}
              key={date}
              title={`${date}: ${count} updates`}
            />
          );
        })}
      </div>
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
