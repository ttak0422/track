interface WikiLinkMatch {
  start: number;
  end: number;
  target: string;
  alias?: string;
}

// The engine permits balanced square brackets inside [[target|display]], including a title such as
// "[draft]" whose final bracket touches the link delimiter. Track depth before recognizing the outer
// "]]"; a regex that stops at the first closing bracket truncates these titles. Offsets address the
// original string, so rendering and portable copy can preserve the surrounding text unchanged.
export function wikiLinks(value: string): WikiLinkMatch[] {
  const links: WikiLinkMatch[] = [];
  let cursor = 0;
  while (cursor < value.length) {
    const start = value.indexOf("[[", cursor);
    if (start < 0) break;
    cursor = value.length; // An unterminated candidate consumes the remainder.
    let depth = 0;
    for (let end = start + 2; end < value.length; end++) {
      const character = value[end];
      if (character === "\n" || character === "\r") {
        cursor = end + 1;
        break;
      }
      if (character === "[") {
        depth++;
      } else if (character === "]") {
        if (depth > 0) {
          depth--;
        } else {
          cursor = end + 1;
          if (value[end + 1] === "]") {
            const inner = value.slice(start + 2, end);
            const pipe = inner.indexOf("|");
            const target = (pipe < 0 ? inner : inner.slice(0, pipe)).trim();
            const alias = pipe < 0 ? undefined : inner.slice(pipe + 1).trim();
            if (target) links.push({ start, end: end + 2, target, alias });
            cursor = end + 2;
          }
          // A lone unmatched "]" ends a malformed candidate. Resume after it, rather than scanning
          // its nested openings again, so even a long malformed bracket run takes linear time.
          break;
        }
      }
    }
  }
  return links;
}
