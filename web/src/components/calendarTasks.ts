// Deadline math for the calendar's day-cell task rows, kept framework-free so it can be tested
// directly (the activityDates.ts precedent).

const DAY_MS = 24 * 60 * 60 * 1000;

// How many days ahead a deadline stops reading as urgent. The bar's fill runs from full (due today)
// down to nothing at this window's edge; past it the date alone carries the information.
export const URGENCY_WINDOW_DAYS = 14;

// daysUntil is the whole-day difference due − today between ISO YYYY-MM-DD keys. Both sides are read
// as UTC midnights, so the subtraction cannot straddle a DST shift: local-date arithmetic hands back
// a 23- or 25-hour day around one and rounds into the wrong count.
export function daysUntil(due: string, today: string): number {
  return Math.round((utcMidnight(due) - utcMidnight(today)) / DAY_MS);
}

// DueBar models one task row's deadline bar. "none" — the task carries no due date (scheduled only),
// and a period with no end cannot be drawn; "due" — upcoming, filled by urgency; "overdue" — past
// the deadline, the full track. The kinds name the state, not the token that paints it.
export type DueBarKind = "none" | "due" | "overdue";

export interface DueBar {
  kind: DueBarKind;
  fillPct: number;
}

// dueBar turns the countdown into the bar's geometry: overdue fills the whole track in danger,
// otherwise the fill is urgency — 1 at zero days left, linearly down to 0 at the window's edge and
// clamped beyond it.
export function dueBar(due: string | undefined, today: string): DueBar {
  if (!due) return { kind: "none", fillPct: 0 };
  const remaining = daysUntil(due, today);
  if (remaining < 0) return { kind: "overdue", fillPct: 100 };
  const urgency = Math.min(Math.max(1 - remaining / URGENCY_WINDOW_DAYS, 0), 1);
  return { kind: "due", fillPct: urgency * 100 };
}

function utcMidnight(iso: string): number {
  const [year, month, day] = iso.split("-").map(Number);
  return Date.UTC(year, month - 1, day);
}
