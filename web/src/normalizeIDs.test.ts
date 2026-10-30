import { describe, expect, it } from "vitest";
import { normalizeIDs } from "./normalizeIDs";

describe("normalizeIDs", () => {
  it.each(["note_id", "source_id", "target_id", "center_id", "root"])("normalizes numeric and string %s values", (key) => {
    expect(normalizeIDs({ vault: "work", [key]: 7 })).toEqual({ vault: "work", [key]: "work~7" });
    expect(normalizeIDs({ [key]: "AbC123" })).toEqual({ [key]: "AbC123" });
    expect(normalizeIDs({ vault: "work", [key]: "20261030" })).toEqual({ vault: "work", [key]: "work~20261030" });
  });

  it("inherits the nearest vault through objects and arrays without leaking to siblings", () => {
    expect(normalizeIDs({
      vault: "work",
      graph: { root: 7, edges: [{ source_id: 7, target_id: "8" }] },
      hits: [{ vault: "home", note: { note_id: 7 } }, { note_id: 7 }],
    })).toEqual({
      vault: "work",
      graph: { root: "work~7", edges: [{ source_id: "work~7", target_id: "work~8" }] },
      hits: [{ vault: "home", note: { note_id: "home~7" } }, { note_id: "work~7" }],
    });
  });

  it("uses the optional fallback vault until a nested label replaces or clears it", () => {
    expect(normalizeIDs([
      { note_id: 1 },
      { vault: "home", note_id: 2 },
      { vault: "", note_id: 3, children: [{ note_id: 4 }] },
      { note_id: 5 },
    ], "work")).toEqual([
      { note_id: "work~1" },
      { vault: "home", note_id: "home~2" },
      { vault: "", note_id: "3", children: [{ note_id: "4" }] },
      { note_id: "work~5" },
    ]);
  });

  it.each([null, undefined, 42, false])("inherits across a non-string vault value %s and keeps that field", (vault) => {
    expect(normalizeIDs({ vault, child: { note_id: 7 } }, "work"))
      .toEqual({ vault, child: { note_id: "work~7" } });
  });

  it("scopes ids even when the vault field appears after them", () => {
    expect(normalizeIDs({ note_id: 7, nested: { root: 8 }, vault: "work" }))
      .toEqual({ note_id: "work~7", nested: { root: "work~8" }, vault: "work" });
  });

  it.each([null, undefined, true, false, 0, 7, "7", "work~7"])("preserves a top-level primitive %s", (value) => {
    expect(normalizeIDs(value, "work")).toBe(value);
  });

  it("preserves primitive array elements while normalizing objects inside arrays", () => {
    expect(normalizeIDs([7, "8", null, [9, { note_id: 10 }]], "work"))
      .toEqual([7, "8", null, [9, { note_id: "work~10" }]]);
  });

  it("recurses into non-scalar id fields without coercing their primitive elements", () => {
    expect(normalizeIDs({ vault: "work", note_id: [7, { note_id: 8 }], root: { target_id: 9 }, center_id: null, source_id: false }))
      .toEqual({ vault: "work", note_id: [7, { note_id: "work~8" }], root: { target_id: "work~9" }, center_id: null, source_id: false });
  });

  it("leaves unrelated and differently cased keys alone while traversing their child objects", () => {
    expect(normalizeIDs({ vault: "work", id: 7, event_id: 8, revision: 9, Note_id: 10, title: "7", metadata: { note_id: 11 } }))
      .toEqual({ vault: "work", id: 7, event_id: 8, revision: 9, Note_id: 10, title: "7", metadata: { note_id: "work~11" } });
  });

  it("preserves qualification of empty, zero, and already-qualified scalar ids", () => {
    expect(normalizeIDs({ vault: "work", note_id: "", root: 0, source_id: "home~7", target_id: "work~8" }))
      .toEqual({ vault: "work", note_id: "work~", root: "work~0", source_id: "work~home~7", target_id: "work~work~8" });
  });

  it("copies nested payload containers without mutating the source", () => {
    const note = Object.freeze({ note_id: 7 });
    const items = Object.freeze([note]);
    const input = Object.freeze({ vault: "work", items });
    const output = normalizeIDs(input);
    expect(output).toEqual({ vault: "work", items: [{ note_id: "work~7" }] });
    expect(input).toEqual({ vault: "work", items: [{ note_id: 7 }] });
    expect(output).not.toBe(input);
    expect(output.items).not.toBe(items);
    expect(output.items[0]).not.toBe(note);
  });
});
