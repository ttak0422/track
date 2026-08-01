import { describe, expect, it } from "vitest";
import fixture from "./staticSearch.cases.json";
import { bodyHits, lineMatch, matchesAnyGroup, splitOrGroups, titleHits } from "./staticSearch";
import type { SearchResult } from "./types";

// The published site's search has to agree with the live server's, so these pin the engine's rules
// (internal/track/store/search.go and internal/track/search/search.go) rather than this file's own.

function note(id: string, title: string, tags?: string[]): SearchResult {
  return { note_id: id, file_kind: "note", path: "", title, tags };
}

describe("splitOrGroups", () => {
  it("ANDs terms implicitly and splits groups on an uppercase OR", () => {
    expect(splitOrGroups("a b OR c")).toEqual([["a", "b"], ["c"]]);
  });

  it("drops an explicit AND, and leaves lowercase and/or as search terms", () => {
    expect(splitOrGroups("a AND b")).toEqual([["a", "b"]]);
    expect(splitOrGroups("a or b")).toEqual([["a", "or", "b"]]);
  });

  it("has no groups for an empty query, so a caller matches nothing rather than everything", () => {
    expect(splitOrGroups("   ")).toEqual([]);
  });
});

describe("matchesAnyGroup", () => {
  const groups = splitOrGroups("quick fox OR slow");

  it("needs every term of some one group, case-insensitively", () => {
    expect(matchesAnyGroup("The QUICK brown Fox", groups)).toBe(true);
    expect(matchesAnyGroup("the quick brown dog", groups)).toBe(false);
    expect(matchesAnyGroup("a slow dog", groups)).toBe(true);
  });

  it("matches inside a word, like the trigram index the live server uses", () => {
    expect(matchesAnyGroup("察する", splitOrGroups("察す"))).toBe(true);
  });
});

describe("lineMatch", () => {
  it("prefers the first line holding every term of a group", () => {
    const body = "quick here\nand fox here\nquick fox together";
    expect(lineMatch(body, splitOrGroups("quick fox"))).toEqual({ line: 3, snippet: "quick fox together" });
  });

  it("falls back to the first line holding any term", () => {
    const body = "nothing\nquick here\nfox here";
    expect(lineMatch(body, splitOrGroups("quick fox"))).toEqual({ line: 2, snippet: "quick here" });
  });

  it("returns the title-only sentinel when no line holds a term", () => {
    // Reachable in a real search: the body matched, but only because the terms straddle lines.
    expect(lineMatch("alpha\nbeta", splitOrGroups("gamma"))).toEqual({ line: 0, snippet: "" });
  });

  it("truncates the snippet at 120 bytes, like the engine", () => {
    const { snippet } = lineMatch(`  ${"あ".repeat(60)} hit`, splitOrGroups("あ"));
    // 120 bytes is 40 three-byte runes exactly, so this one lands on a boundary already.
    expect(snippet).toBe(`${"あ".repeat(40)}…`);
  });

  it("backs up to a rune boundary rather than cutting a character in half", () => {
    // One ASCII byte ahead of the Japanese puts byte 120 inside the 40th rune (bytes 118-120), so
    // the cut has to walk back to 118 and drop that rune whole.
    const { snippet } = lineMatch(`x${"あ".repeat(60)} hit`, splitOrGroups("あ"));
    expect(snippet).toBe(`x${"あ".repeat(39)}…`);
    expect(new TextEncoder().encode(snippet.slice(0, -1)).length).toBe(118);
  });
});

describe("titleHits", () => {
  const notes = [
    note("n1", "Fox facts", ["animal/wild"]),
    note("n2", "Fox", ["animal"]),
    note("n3", "Arctic fox", ["animal"]),
    note("n4", "Bear", ["animal"]),
  ];

  it("ranks an exact title, then a prefix, then the rest, keeping input order on a tie", () => {
    expect(titleHits(notes, "Fox").map((n) => n.note_id)).toEqual(["n2", "n1", "n3"]);
  });

  it("tags every hit so the panel can group them", () => {
    expect(titleHits(notes, "Bear")[0].match).toBe("title");
  });

  it("filters by #tag hierarchically and ranks an exact tag above a descendant", () => {
    expect(titleHits(notes, "#animal").map((n) => n.note_id)).toEqual(["n2", "n3", "n4", "n1"]);
    expect(titleHits(notes, "#animal/wild").map((n) => n.note_id)).toEqual(["n1"]);
  });

  it("combines a #tag filter with the text query", () => {
    // n4 fails the text; n1 passes both but ranks last, since its tag matches only as a descendant.
    expect(titleHits(notes, "#animal fox").map((n) => n.note_id)).toEqual(["n2", "n3", "n1"]);
  });

  it("applies the OR/AND grammar to titles, like the live server's title query", () => {
    expect(titleHits(notes, "arctic OR bear").map((n) => n.note_id)).toEqual(["n3", "n4"]);
  });

  it("lists everything for an empty query, which is what the notes listing is", () => {
    expect(titleHits(notes, "")).toHaveLength(4);
  });

  it("searches titles for a bare # rather than listing the vault", () => {
    // The first keystroke of a "#tag" query yields no tag, and the store falls through to a plain
    // title search over the raw string. Dropping the "#" instead would flash every note.
    expect(titleHits(notes, "#")).toHaveLength(0);
    expect(titleHits([...notes, note("n5", "C# notes")], "#").map((n) => n.note_id)).toEqual(["n5"]);
  });
});

describe("bodyHits", () => {
  const notes = [note("n1", "One"), note("n2", "Two"), note("n3", "Three")];
  const docs = [
    { note_id: "n1", body: "# One\n\nthe quick brown fox\n" },
    { note_id: "n2", body: "# Two\n\nnothing here\n" },
    { note_id: "n3", body: "# Three\n\na quick dog\n" },
  ];

  it("returns matching notes in corpus order, each with its line and snippet", () => {
    const hits = bodyHits(notes, docs, "quick", 10, new Set());
    expect(hits.map((n) => n.note_id)).toEqual(["n1", "n3"]);
    expect(hits[0]).toMatchObject({ match: "body", line: 3, snippet: "the quick brown fox" });
  });

  it("keeps the corpus order rather than the notes listing's", () => {
    // The two orders differ on an mtime tie, and the corpus is the one the engine's scan uses.
    const reversed = [...docs].reverse();
    expect(bodyHits(notes, reversed, "quick", 10, new Set()).map((n) => n.note_id)).toEqual(["n3", "n1"]);
  });

  it("skips the notes the title search already named", () => {
    expect(bodyHits(notes, docs, "quick", 10, new Set(["n1"])).map((n) => n.note_id)).toEqual(["n3"]);
  });

  it("stops at the remaining budget", () => {
    expect(bodyHits(notes, docs, "quick", 1, new Set())).toHaveLength(1);
  });

  it("points at the first line holding a term when the group straddles lines", () => {
    const hit = bodyHits([note("n1", "One")], [{ note_id: "n1", body: "quick\nfox" }], "quick fox", 10, new Set())[0];
    expect(hit).toMatchObject({ match: "body", line: 1, snippet: "quick" });
  });

  it("matches nothing for an empty query rather than everything", () => {
    expect(bodyHits(notes, docs, "  ", 10, new Set())).toEqual([]);
  });

  it("ignores a corpus entry the notes listing does not name", () => {
    const extra = [...docs, { note_id: "gone", body: "quick" }];
    expect(bodyHits(notes, extra, "quick", 10, new Set()).map((n) => n.note_id)).toEqual(["n1", "n3"]);
  });
});

// The other half of internal/track/search/search_test.go's differential test. The cases above were
// transcribed from the Go source and can drift from it unnoticed; these are read from the same file
// the Go test reads, so a divergence between the engine's scan and this port goes red on one side or
// the other. The Unicode cases are the ones that caught this port folding like JavaScript rather than
// like strings.ToLower.
describe("the shared engine fixture", () => {
  it.each(fixture.cases)("$name", ({ query, body, match, line, snippet }) => {
    const groups = splitOrGroups(query);
    expect(matchesAnyGroup(body, groups)).toBe(match);
    expect(lineMatch(body, groups)).toEqual({ line, snippet });
  });
});
