import { splitOrGroups, fold } from "./staticSearch";

export interface SearchHighlightPart {
  text: string;
  highlighted: boolean;
}

// Search treats uppercase AND/OR as operators and every other whitespace-delimited field as a
// substring term. Keeping the same small grammar here means the visual highlight does not paint the
// operators themselves.
function searchTerms(query: string): string[] {
  return [...new Set(splitOrGroups(query).flat().map(fold))].sort((a, b) => b.length - a.length);
}

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Split display text into plain and matched pieces without injecting HTML. Matching uses the same fold
// as the search engine, then maps folded offsets back to the original text so the display casing and
// Unicode text stay untouched.
export function highlightSearchText(text: string, query: string): SearchHighlightPart[] {
  const terms = searchTerms(query);
  if (terms.length === 0 || text === "") return [{ text, highlighted: false }];

  const folded = foldedWithOffsets(text);
  const matcher = new RegExp(terms.map(escapeRegExp).join("|"), "gu");
  const parts: SearchHighlightPart[] = [];
  let cursor = 0;
  for (const match of folded.text.matchAll(matcher)) {
    const index = match.index ?? 0;
    const start = folded.starts[index];
    const end = folded.ends[index + match[0].length - 1];
    if (start === undefined || end === undefined || start < cursor) continue;
    if (start > cursor) parts.push({ text: text.slice(cursor, start), highlighted: false });
    parts.push({ text: text.slice(start, end), highlighted: true });
    cursor = end;
  }
  if (cursor < text.length) parts.push({ text: text.slice(cursor), highlighted: false });
  return parts.length > 0 ? parts : [{ text, highlighted: false }];
}

function foldedWithOffsets(text: string): { text: string; starts: number[]; ends: number[] } {
  let folded = "";
  const starts: number[] = [];
  const ends: number[] = [];
  for (let offset = 0; offset < text.length; ) {
    const codePoint = text.codePointAt(offset);
    if (codePoint === undefined) break;
    const original = String.fromCodePoint(codePoint);
    const next = offset + original.length;
    const mapped = fold(original);
    folded += mapped;
    for (let i = 0; i < mapped.length; i++) {
      starts.push(offset);
      ends.push(next);
    }
    offset = next;
  }
  return { text: folded, starts, ends };
}
