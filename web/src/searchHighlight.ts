export interface SearchHighlightPart {
  text: string;
  highlighted: boolean;
}

// Search treats uppercase AND/OR as operators and every other whitespace-delimited field as a
// substring term. Keeping the same small grammar here means the visual highlight does not paint the
// operators themselves.
function searchTerms(query: string): string[] {
  return [...new Set(query.split(/\s+/).filter((term) => term !== "" && term !== "AND" && term !== "OR"))].sort(
    (a, b) => b.length - a.length,
  );
}

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Split display text into plain and matched pieces without injecting HTML. The match is literal and
// case-insensitive, matching the search engine's substring behavior while leaving the original casing
// and Unicode text untouched for display.
export function highlightSearchText(text: string, query: string): SearchHighlightPart[] {
  const terms = searchTerms(query);
  if (terms.length === 0 || text === "") return [{ text, highlighted: false }];

  const matcher = new RegExp(terms.map(escapeRegExp).join("|"), "giu");
  const parts: SearchHighlightPart[] = [];
  let cursor = 0;
  for (const match of text.matchAll(matcher)) {
    const index = match.index ?? 0;
    if (index > cursor) parts.push({ text: text.slice(cursor, index), highlighted: false });
    parts.push({ text: match[0], highlighted: true });
    cursor = index + match[0].length;
  }
  if (cursor < text.length) parts.push({ text: text.slice(cursor), highlighted: false });
  return parts.length > 0 ? parts : [{ text, highlighted: false }];
}
