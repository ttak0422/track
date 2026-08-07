import { beforeEach, describe, expect, it } from "vitest";
import type { SearchResult } from "./types";
import { activityMessage, markSelfWrite, newlyActive } from "./vaultActivity";

function note(id: string, title: string, days: string[]): SearchResult {
  return { note_id: id, file_kind: "note", path: `/vault/note/${id}.md`, title, days };
}

const DAY = "2026-08-06";

describe("newlyActive", () => {
  beforeEach(() => {
    // Each test starts on its own day so the module's per-day memory begins empty.
    newlyActive([], `reset-${Math.random()}`);
  });

  it("primes on the first look and reports only what arrives after it", () => {
    const first = newlyActive([note("1", "Old", [DAY])], DAY);
    expect(first.priming).toBe(true);
    expect(first.notes).toEqual([{ note_id: "1", title: "Old" }]);

    const second = newlyActive([note("1", "Old", [DAY]), note("2", "New", [DAY])], DAY);
    expect(second.priming).toBe(false);
    expect(second.notes).toEqual([{ note_id: "2", title: "New" }]);
  });

  it("announces a note once per day", () => {
    newlyActive([], DAY);
    expect(newlyActive([note("1", "Edited", [DAY])], DAY).notes).toEqual([{ note_id: "1", title: "Edited" }]);
    expect(newlyActive([note("1", "Edited", [DAY])], DAY).notes).toEqual([]);
  });

  it("ignores notes whose activity is on another day", () => {
    newlyActive([], DAY);
    expect(newlyActive([note("1", "Yesterday", ["2026-08-05"])], DAY).notes).toEqual([]);
  });

  it("skips a note this tab saved itself", () => {
    newlyActive([], DAY);
    markSelfWrite("1");
    expect(newlyActive([note("1", "Mine", [DAY]), note("2", "Theirs", [DAY])], DAY).notes).toEqual([
      { note_id: "2", title: "Theirs" },
    ]);
  });
});

describe("activityMessage", () => {
  it("names one note, and counts the rest", () => {
    expect(activityMessage(["A"])).toBe("Updated: A");
    expect(activityMessage(["A", "B", "C"])).toBe("Updated: A (+2 more)");
  });
});
