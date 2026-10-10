import { describe, expect, it } from "vitest";
import { TabCycle } from "./tabCycle";

describe("MRU note-tab cycling", () => {
  it("reaches every note and wraps both ways despite promotion to the front", () => {
    const cycle = new TabCycle();
    expect(cycle.next(["a", "b", "c"], "a", 1)).toBe("b");
    expect(cycle.next(["b", "a", "c"], "b", 1)).toBe("c");
    expect(cycle.next(["c", "b", "a"], "c", 1)).toBe("a");
    expect(cycle.next(["a", "c", "b"], "a", -1)).toBe("c");
    expect(cycle.next(["c", "a", "b"], "c", -1)).toBe("b");
  });
  it("resets after a blocked or external navigation and open/close changes", () => {
    const cycle = new TabCycle();
    cycle.next(["a", "b", "c"], "a", 1);
    expect(cycle.next(["a", "b", "c"], "a", -1)).toBe("c"); // canceled switch
    expect(cycle.next(["d", "c", "a", "b"], "d", 1)).toBe("c"); // new note
    expect(cycle.next(["c", "b", "d"], "c", 1)).toBe("b"); // closed note
    cycle.reset();
    expect(cycle.next(["b", "d", "c"], "b", 1)).toBe("d");
  });
  it("observes intervening navigation or membership changes even after returning to the same tab", () => {
    const cycle = new TabCycle();
    cycle.next(["a", "b", "c"], "a", 1);
    cycle.observe(["b", "a", "c"], "b"); // own navigation preserves the snapshot
    cycle.observe(["c", "b", "a"], "c"); // another navigation invalidates it now
    cycle.observe(["b", "c", "a"], "b");
    expect(cycle.next(["b", "c", "a"], "b", 1)).toBe("c");
    cycle.observe(["c", "b", "a"], "c");
    cycle.observe(["c", "b"], "c");
    cycle.observe(["c", "b", "a"], "c");
    expect(cycle.next(["c", "b", "a"], "c", 1)).toBe("b");
  });
  it("does not invent tabs for empty, single-note, or off-note routes", () => {
    const cycle = new TabCycle();
    expect(cycle.next([], "a", 1)).toBeNull();
    expect(cycle.next(["a"], "a", 1)).toBeNull();
    expect(cycle.next(["a", "b"], "home", 1)).toBeNull();
  });
});
