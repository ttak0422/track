import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  VIEW_TICK_SEC,
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

beforeEach(() => localStorage.clear());
afterEach(() => localStorage.clear());

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
    const yearAgo = new Date();
    yearAgo.setFullYear(yearAgo.getFullYear() - 1);
    const inside = new Date(yearAgo.getTime() + 86400000);
    const outside = new Date(yearAgo.getTime() - 86400000);
    expect(isStaleDay(outside.toISOString().slice(0, 10))).toBe(true);
    expect(isStaleDay(inside.toISOString().slice(0, 10))).toBe(false);
    expect(isStaleDay("2026-13-45")).toBe(false);
  });
});
