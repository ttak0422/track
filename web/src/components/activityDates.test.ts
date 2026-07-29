import { describe, expect, it } from "vitest";
import { dateKey, weekAlignedDates } from "./activityDates";

describe("weekAlignedDates", () => {
  it("starts on a Sunday and ends today, with whole weeks before the current one", () => {
    // A fixed mid-week day; the invariants below hold for any weekday.
    const today = new Date(2026, 6, 29);
    const dates = weekAlignedDates(today, 4);

    expect(dates[dates.length - 1]).toBe(dateKey(today));
    expect(dates).toHaveLength(3 * 7 + today.getDay() + 1);

    const [year, month, day] = dates[0].split("-").map(Number);
    expect(new Date(year, month - 1, day).getDay()).toBe(0);
  });

  it("renders exactly the trailing week when today is a Saturday", () => {
    // 2026-01-03 is a Saturday, so a single week is complete: Sunday through today.
    const today = new Date(2026, 0, 3);
    expect(today.getDay()).toBe(6);
    const dates = weekAlignedDates(today, 1);
    expect(dates).toEqual([
      "2025-12-28",
      "2025-12-29",
      "2025-12-30",
      "2025-12-31",
      "2026-01-01",
      "2026-01-02",
      "2026-01-03",
    ]);
  });

  it("keeps consecutive days across a month boundary", () => {
    const today = new Date(2026, 7, 2); // 2026-08-02, a Sunday
    expect(today.getDay()).toBe(0);
    const dates = weekAlignedDates(today, 2);
    expect(dates[0]).toBe("2026-07-26");
    expect(dates[dates.length - 1]).toBe("2026-08-02");
    expect(dates).toHaveLength(8);
  });
});

describe("dateKey", () => {
  it("zero-pads the month and day", () => {
    expect(dateKey(new Date(2026, 0, 5))).toBe("2026-01-05");
  });
});
