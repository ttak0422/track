import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  VIEW_TICK_SEC,
  adoptReadState,
  isNew,
  isRead,
  isStaleDay,
  lastActivityDay,
  markSeen,
  readThresholdFor,
  recordView,
} from "./reading";

function stored(): Record<string, { sec: number; read: boolean }> {
  return JSON.parse(localStorage.getItem("track.reading") ?? "{}");
}

// Milestones report to the live server fire-and-forget; every test gets an observable stand-in so
// nothing touches a network and the reporting contract can be asserted.
const fetchMock = vi.fn<(input: unknown, init?: { body?: string }) => Promise<{ ok: boolean }>>();

beforeEach(() => {
  localStorage.clear();
  fetchMock.mockReset();
  fetchMock.mockResolvedValue({ ok: true });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("readThresholdFor", () => {
  it("floors at the minimum so a glance at a tiny note cannot read it", () => {
    expect(readThresholdFor("")).toBeGreaterThanOrEqual(20);
    expect(readThresholdFor("短いメモ")).toBe(20);
  });

  it("halves the estimated reading time for a long report", () => {
    // 2000 chars at ~10 chars/sec is ~200s of reading; half is 100s.
    expect(readThresholdFor("あ".repeat(2000))).toBe(100);
  });
});

describe("NEW and read state", () => {
  it("starts every note as new", () => {
    expect(isNew("100")).toBe(true);
  });

  it("markSeen clears NEW without marking read", () => {
    markSeen("100");
    expect(isNew("100")).toBe(false);
    expect(isRead("100")).toBe(false);
  });

  it("recordView marks read past the threshold", () => {
    const threshold = readThresholdFor("あ".repeat(2000));
    recordView("100", threshold - 1, "あ".repeat(2000));
    expect(isRead("100")).toBe(false);
    recordView("100", 2, "あ".repeat(2000));
    expect(isRead("100")).toBe(true);
    // And NEW never comes back.
    expect(isNew("100")).toBe(false);
  });

  it("ignores non-positive seconds", () => {
    recordView("100", 0, "あ".repeat(2000));
    expect(isRead("100")).toBe(false);
  });

  it("survives a reload through localStorage", () => {
    markSeen("100");
    recordView("100", 100, "あ".repeat(2000));
    expect(stored()["100"].sec).toBe(100);
  });

  it("tolerates a corrupt store", () => {
    localStorage.setItem("track.reading", "{not json");
    expect(isNew("100")).toBe(true);
    markSeen("100");
    expect(isNew("100")).toBe(false);
  });

  it("exposes the tick the workspace flushes at", () => {
    expect(VIEW_TICK_SEC).toBeGreaterThan(0);
  });
});

describe("stale day", () => {
  it("takes the newest activity day", () => {
    expect(lastActivityDay(["2024-01-01", "2026-08-10", "2025-06-01"])).toBe("2026-08-10");
    expect(lastActivityDay([])).toBe("");
    expect(lastActivityDay(undefined)).toBe("");
  });

  it("flags days over a year old", () => {
    // Day strings are built from local date parts, matching how isStaleDay parses them; an
    // ISO-UTC slice would cross the local midnight and flip which side of the cutoff a sample
    // lands on depending on the timezone the tests run in.
    const localDay = (d: Date) =>
      `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
    const yearAgo = new Date();
    yearAgo.setFullYear(yearAgo.getFullYear() - 1);
    const inside = new Date(yearAgo.getTime() + 86400000);
    const outside = new Date(yearAgo.getTime() - 86400000);
    expect(isStaleDay(localDay(outside))).toBe(true);
    expect(isStaleDay(localDay(inside))).toBe(false);
    expect(isStaleDay("2026-13-45")).toBe(false);
  });
});

describe("shared read state", () => {
  it("reports seen to the server once, the first time a note is opened here", () => {
    markSeen("100");
    markSeen("100");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/note/read?id=100");
    expect(JSON.parse(init?.body ?? "{}")).toEqual({ event: "seen" });
  });

  it("reports read once, when the threshold is crossed", () => {
    const threshold = readThresholdFor("あ".repeat(2000));
    recordView("100", threshold - 1, "あ".repeat(2000));
    recordView("100", 2, "あ".repeat(2000));
    recordView("100", 50, "あ".repeat(2000));
    const events = fetchMock.mock.calls.map(([, init]) => JSON.parse(init?.body ?? "{}").event);
    expect(events).toEqual(["read"]);
  });

  it("keeps vault-qualified ids local instead of guessing a vault", () => {
    markSeen("work:100");
    expect(fetchMock).not.toHaveBeenCalled();
    expect(isNew("work:100")).toBe(false);
  });

  it("adopts another device's milestones from listing rows", () => {
    adoptReadState([
      { note_id: "100", seen_at: 1755000000 },
      { note_id: "200", seen_at: 1755000000, read_at: 1755000600 },
      { note_id: "300" },
      { id: "400", read_at: 1755000000 },
    ]);
    expect(isNew("100")).toBe(false);
    expect(isRead("100")).toBe(false);
    expect(isRead("200")).toBe(true);
    expect(isNew("300")).toBe(true);
    expect(isRead("400")).toBe(true);
    // Adoption is server truth flowing in, not new local activity going out.
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("adoption never un-reads what this browser read first", () => {
    recordView("200", 9999, "あ".repeat(2000));
    fetchMock.mockClear();
    adoptReadState([{ note_id: "200", seen_at: 123 }]);
    expect(isRead("200")).toBe(true);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("tolerates junk rows", () => {
    expect(() =>
      adoptReadState([
        { note_id: "" },
        null as unknown as { note_id?: unknown },
        { note_id: "100", seen_at: -5, read_at: undefined },
      ]),
    ).not.toThrow();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
