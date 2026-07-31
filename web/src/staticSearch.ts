// The published site's search, run in the browser because there is no server to ask.
//
// It is not a second search: it is the engine's own scan path (internal/track/search,
// bodySearchScan) expressed in TypeScript. The live server prefers its FTS5 index, but that index
// is a *trigram* index — matching it means matching substrings, case-insensitively — and whenever a
// query cannot be served from it the engine falls back to exactly this: scan every body, keep the
// ones satisfying an OR group, take the first matching line as the snippet, order by recency. So
// the published site runs the fallback the engine already ships, over the bodies the bundle already
// ships, and the two agree on which notes match and on what the snippet says.
//
// The one thing it cannot reproduce is bm25 relevance, which lives in the index. Neither can the
// engine's own fallback, which is why the fallback orders by recency — and why this does too,
// rather than inventing a ranking the live server would disagree with.

import type { SearchResult } from "./types";

// bodies maps a published slug to that note's source Markdown, the shape of data/search.json.
export interface SearchCorpus {
  bodies: Record<string, string>;
}

// splitOrGroups mirrors store.splitOrGroups: an uppercase OR ends a group of implicitly-ANDed terms,
// an uppercase AND is that default spelled out (and dropped), and everything else is a term. A
// lowercase "and"/"or" stays an ordinary search term.
export function splitOrGroups(text: string): string[][] {
  const groups: string[][] = [];
  let cur: string[] = [];
  for (const field of text.split(/\s+/)) {
    if (field === "") continue;
    if (field === "OR") {
      if (cur.length > 0) {
        groups.push(cur);
        cur = [];
      }
    } else if (field !== "AND") {
      cur.push(field);
    }
  }
  if (cur.length > 0) groups.push(cur);
  return groups;
}

// matchesAnyGroup mirrors search.bodyMatchesAnyGroup: text matches when some one group has all of
// its terms present as case-insensitive substrings — the trigram index's semantics, which is why the
// scan and the index agree on what matched.
export function matchesAnyGroup(text: string, groups: string[][]): boolean {
  const lower = text.toLowerCase();
  return groups.some((terms) => terms.every((term) => lower.includes(term.toLowerCase())));
}

// lineMatch mirrors search.bodyLineMatchGroups: the first line carrying every term of some satisfied
// group (the tightest match), else the first line carrying any term. Line 0 is the title-only
// sentinel, reached when a group's terms straddle line breaks — which is why a body hit is tagged by
// `match` and not by whether it has a snippet.
export function lineMatch(body: string, groups: string[][]): { line: number; snippet: string } {
  let fallback = { line: 0, snippet: "" };
  const lines = body.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const lower = lines[i].toLowerCase();
    for (const terms of groups) {
      let all = terms.length > 0;
      let some = false;
      for (const term of terms) {
        if (lower.includes(term.toLowerCase())) some = true;
        else all = false;
      }
      if (all) return { line: i + 1, snippet: snippetOf(lines[i]) };
      if (some && fallback.line === 0) fallback = { line: i + 1, snippet: snippetOf(lines[i]) };
    }
  }
  return fallback;
}

const SNIPPET_BYTES = 120;

// snippetOf mirrors search.truncateSearchSnippet, which measures in bytes and backs up to a rune
// boundary. Measuring in UTF-16 units instead would cut a Japanese line at a different place than
// the live server does, so encode and cut where it cuts.
function snippetOf(line: string): string {
  const text = line.trim();
  const bytes = new TextEncoder().encode(text);
  if (bytes.length <= SNIPPET_BYTES) return text;
  let end = SNIPPET_BYTES;
  // 0b10xxxxxx is a UTF-8 continuation byte; walk back off it to the start of its rune.
  while (end > 0 && (bytes[end] & 0xc0) === 0x80) end--;
  return `${new TextDecoder().decode(bytes.subarray(0, end))}…`;
}

interface TaggedQuery {
  text: string;
  tags: string[];
}

// splitTagQuery mirrors store.parseTaggedQuery: "#tag" fields filter by tag and leave the rest as the
// text query. Duplicates collapse, and a bare "#" is not a tag.
function splitTagQuery(query: string): TaggedQuery {
  const tags: string[] = [];
  const text: string[] = [];
  for (const field of query.split(/\s+/)) {
    if (field === "") continue;
    if (field.startsWith("#")) {
      const tag = field.slice(1).trim();
      if (tag !== "" && !tags.includes(tag)) tags.push(tag);
      continue;
    }
    text.push(field);
  }
  return { text: text.join(" "), tags };
}

// hasTag mirrors the store's hierarchical tag filter: #a matches a note tagged "a" or any descendant
// like "a/b", never "ab".
function hasTag(note: SearchResult, tag: string): boolean {
  const want = tag.toLowerCase();
  return (note.tags ?? []).some((t) => {
    const lower = t.toLowerCase();
    return lower === want || lower.startsWith(`${want}/`);
  });
}

function hasExactTag(note: SearchResult, tag: string): boolean {
  const want = tag.toLowerCase();
  return (note.tags ?? []).some((t) => t.toLowerCase() === want);
}

// titleHits mirrors the store's title query: #tags filter hierarchically and rank an exact tag before
// a descendant one, the remaining text matches titles under the same OR/AND grammar the body path
// uses, and an exact title outranks a prefix outranks the rest. Ties keep the input order, which is
// the recently-updated-first order the store breaks ties by.
export function titleHits(notes: SearchResult[], query: string): SearchResult[] {
  const { text, tags } = splitTagQuery(query);
  const groups = splitOrGroups(text);
  const lowerText = text.toLowerCase();
  const rankOf = (note: SearchResult): number[] => {
    const rank = tags.map((tag) => (hasExactTag(note, tag) ? 0 : 1));
    if (text !== "") {
      const title = note.title.toLowerCase();
      rank.push(title === lowerText ? 0 : 1, title.startsWith(lowerText) ? 0 : 1);
    }
    return rank;
  };
  return notes
    .filter(
      (note) =>
        tags.every((tag) => hasTag(note, tag)) &&
        (groups.length === 0 || matchesAnyGroup(note.title, groups)),
    )
    .map((note, order) => ({ note, order, rank: rankOf(note) }))
    .sort((a, b) => {
      for (let i = 0; i < a.rank.length; i++) {
        if (a.rank[i] !== b.rank[i]) return a.rank[i] - b.rank[i];
      }
      return a.order - b.order;
    })
    .map(({ note }) => ({ ...note, match: "title" as const }));
}

// bodyHits scans the corpus for notes whose body satisfies the query, skipping the ones the titles
// already named — the same budget the engine's composition spends, title hits first. Notes arrive in
// the bundle's recently-updated-first order and are kept in it, which is the scan path's ordering.
export function bodyHits(
  notes: SearchResult[],
  bodies: Record<string, string>,
  query: string,
  limit: number,
  skip: Set<string>,
): SearchResult[] {
  const groups = splitOrGroups(query);
  if (limit <= 0 || groups.length === 0) return [];
  const out: SearchResult[] = [];
  for (const note of notes) {
    const id = String(note.note_id);
    if (skip.has(id)) continue;
    const body = bodies[id];
    if (body === undefined || !matchesAnyGroup(body, groups)) continue;
    const { line, snippet } = lineMatch(body, groups);
    // Line 0 is the sentinel, and the live server omits both fields there rather than sending a
    // line number that points at nothing.
    out.push({ ...note, match: "body", ...(line > 0 ? { line, snippet } : {}) });
    if (out.length >= limit) break;
  }
  return out;
}
