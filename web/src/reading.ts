// Read-state tracking for the web workspace: which notes have been opened, and how long was spent on
// each. Two layers with one cache:
//
// - Milestones (seen / read) are shared. The note sidecar carries them as vault metadata, so every
//   device converges through its own sync; the server reports them back on every listing
//   (seen_at/read_at) and this module adopts that truth into localStorage. The workspace POSTs a
//   milestone the first time it is reached here.
// - The accumulated viewing seconds stay per-browser — they are only the estimate that decides when
//   "read" fires, and reading also happens in Neovim, which reports nothing.
//
// NEW is "no device has opened the note yet" — a report the agent filed but nobody has looked at —
// and read is "viewed for at least half of the estimated reading time", floored at a minimum so a
// glance at a tiny note cannot mark it read.

import { STATIC_MODE } from "./runtime";

export interface ReadState {
  // Accumulated viewing seconds (this browser only).
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

// isNew reports whether no device has opened the note yet (server truth adopted, or never marked
// here — the two merge monotonically, so a note cannot flip back to NEW).
//
// Never on a published site: NEW is the author's own backlog marker — a note the vault holds that
// nobody has looked at — and a reader arriving at the site has looked at none of them, so every row
// would carry the badge. Nothing clears it there either (the static reader records no milestones),
// so it would be a permanent NEW on every note in every list.
export function isNew(id: string): boolean {
  if (STATIC_MODE) return false;
  return !(id in load());
}

// isRead reports whether the note reached its read threshold on any device that has reported.
export function isRead(id: string): boolean {
  return load()[id]?.read ?? false;
}

// markSeen records that the note was opened at all, so NEW goes away even before the read
// threshold is reached. Called when a note view mounts; the milestone is reported to the server
// the first time this browser reaches it.
export function markSeen(id: string) {
  const map = load();
  if (id in map) return;
  map[id] = { sec: 0, read: false };
  save(map);
  postReadEvent(id, "seen");
}

// recordView accumulates viewing time for the note and flips it to read once the threshold for the
// given text is reached. Coarse by design: called at VIEW_TICK_SEC intervals, never per tick. The
// crossing is reported to the server once, from the browser that saw it happen.
export function recordView(id: string, seconds: number, text: string) {
  if (!(seconds > 0)) return;
  const map = load();
  const entry = map[id] ?? { sec: 0, read: false };
  entry.sec += seconds;
  const becameRead = entry.sec >= readThresholdFor(text) && !entry.read;
  if (entry.sec >= readThresholdFor(text)) entry.read = true;
  map[id] = entry;
  save(map);
  if (becameRead) postReadEvent(id, "read");
}

// adoptReadState merges the server's shared milestones into the local cache. Every listing carries
// seen_at/read_at now; adopting them here means the badges the workspace already draws from
// isNew/isRead reflect what other devices recorded without any component learning about the wire.
// Monotonic by construction: adoption can only clear NEW and set read, never the reverse.
export function adoptReadState(rows: ReadonlyArray<{ note_id?: unknown; id?: unknown; seen_at?: number; read_at?: number }>) {
  if (rows.length === 0) return;
  const map = load();
  let changed = false;
  for (const row of rows) {
    if (!row || typeof row !== "object") continue;
    const key = row.note_id ?? row.id;
    if (key === undefined || key === null || key === "") continue;
    const id = String(key);
    const read = (row.read_at ?? 0) > 0;
    if ((row.seen_at ?? 0) <= 0 && !read) continue;
    if (map[id]?.read) continue; // read here already reads everywhere: nothing to adopt
    map[id] = { sec: map[id]?.sec ?? 0, read };
    changed = true;
  }
  if (changed) save(map);
}

// postReadEvent reports a locally reached milestone to the live server, which writes it into the
// note's sidecar for every other device to pick up. Fire-and-forget: reading must not break when a
// request fails, because the next listing re-offers the server's truth anyway. Vault-qualified ids
// ("name:123", federated results) stay local — the endpoint marks within one vault, and guessing
// which could stamp the wrong one.
function postReadEvent(id: string, event: "seen" | "read") {
  if (STATIC_MODE || !/^\d+$/.test(id)) return;
  fetch(`/api/note/read?id=${id}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ event }),
  }).catch(() => {});
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
