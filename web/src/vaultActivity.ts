import type { NoteID, SearchResult } from "./types";

// What this tab already knows about today's activity: every id it has toasted, plus the notes it saved
// itself — a save is not news to the tab that made it. Module state rather than a ref because
// useSaveNoteMutation writes to it from outside the watcher's tree. Kept free of React and of the
// query layer so the mutation can import it without a cycle.
let seen = new Set<NoteID>();
let seenDay = "";

export function markSelfWrite(noteID: NoteID) {
  seen.add(noteID);
}

// newlyActive returns the notes that joined `day`'s activity since the last call, and records them so
// each note is announced once per day. A note's activity days cover both halves of "something changed":
// the day it was created, and every day it was edited. A day change primes rather than reports —
// yesterday's notes are not news either — which the boolean tells the caller.
export function newlyActive(notes: SearchResult[], day: string): { notes: ActiveNote[]; priming: boolean } {
  const priming = day !== seenDay;
  if (priming) {
    seen = new Set();
    seenDay = day;
  }
  const active: ActiveNote[] = [];
  for (const note of notes) {
    if (!(note.days ?? []).includes(day)) continue;
    if (seen.has(note.note_id)) continue;
    seen.add(note.note_id);
    active.push({ note_id: note.note_id, title: note.title || note.note_id });
  }
  return { notes: active, priming };
}

// ActiveNote is one note that joined a day's activity, with the id a toast can navigate to.
export interface ActiveNote {
  note_id: NoteID;
  title: string;
}

export function activityMessage(titles: string[]): string {
  const head = titles[0];
  return titles.length === 1 ? `Updated: ${head}` : `Updated: ${head} (+${titles.length - 1} more)`;
}

// YYYY-MM-DD in local time, matching the days the engine stamps on a note.
export function today(): string {
  return new Date().toLocaleDateString("sv-SE");
}
