// Read-state tracking for the web workspace: which notes this browser has opened, and how long it
// has spent on each. Kept in localStorage so the state survives reloads. NEW is "never opened here" —
// a report the agent filed but nobody has looked at — and read is "viewed for at least half of the
// estimated reading time", floored at a minimum so a glance at a tiny note cannot mark it read.
// The state is per-browser on purpose: reading also happens in Neovim, and the workspace cannot know
// what a terminal read. The web side only ever claims "read here".

export interface ReadState {
  // Accumulated viewing seconds.
  sec: number;
  read: boolean;
}

const KEY = "track.reading";
// Nothing counts as read in under this, however short the note.
const MIN_READ_SEC = 20;
// Rough Japanese reading pace (~600 chars/min), the estimate that the read threshold halves.
const CHARS_PER_SECOND = 10;
// The coarse accumulation tick the workspace flushes at. Reading time is an estimate; a
// per-second timer would only burn battery for the same answer.
export const VIEW_TICK_SEC = 5;

function load(): Record<string, ReadState> {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, ReadState>;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function save(map: Record<string, ReadState>) {
  try {
    localStorage.setItem(KEY, JSON.stringify(map));
  } catch {
    // A full or unavailable storage must not break reading.
  }
}

// readThresholdFor returns the accumulated viewing seconds that makes a note "read": half of its
// estimated reading time (text length at a rough reading pace), floored at a minimum.
export function readThresholdFor(text: string): number {
  const estimate = Math.max(MIN_READ_SEC, Math.round(text.length / CHARS_PER_SECOND));
  return Math.max(MIN_READ_SEC, Math.round(estimate / 2));
}

// isNew reports whether this browser has never opened the note.
export function isNew(id: string): boolean {
  return !(id in load());
}

// isRead reports whether the note reached its read threshold here.
export function isRead(id: string): boolean {
  return load()[id]?.read ?? false;
}

// markSeen records that the note was opened at all, so NEW goes away even before the read
// threshold is reached. Called when a note view mounts.
export function markSeen(id: string) {
  const map = load();
  if (id in map) return;
  map[id] = { sec: 0, read: false };
  save(map);
}

// recordView accumulates viewing time for the note and flips it to read once the threshold for the
// given text is reached. Coarse by design: called at VIEW_TICK_SEC intervals, never per tick.
export function recordView(id: string, seconds: number, text: string) {
  if (!(seconds > 0)) return;
  const map = load();
  const entry = map[id] ?? { sec: 0, read: false };
  entry.sec += seconds;
  if (entry.sec >= readThresholdFor(text)) entry.read = true;
  map[id] = entry;
  save(map);
}

// lastActivityDay returns the newest of a note's activity days, or "" when it has none — the
// day-based clock the stale badge reads.
export function lastActivityDay(days?: string[]): string {
  if (!days || days.length === 0) return "";
  return [...days].sort().at(-1) ?? "";
}

// isStaleDay reports whether the day is more than a year before today — the "might be outdated"
// marker for notes that stopped being touched.
export function isStaleDay(day: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return false;
  const cutoff = new Date();
  cutoff.setFullYear(cutoff.getFullYear() - 1);
  return new Date(`${day}T00:00:00`) < cutoff;
}
