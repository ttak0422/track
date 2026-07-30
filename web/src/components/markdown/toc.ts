// A note's heading outline, and the ids its rendered headings carry so an outline entry can jump to
// one. The scan is shared with the mindmap fence (which draws the same headings as a tree), so both
// read a note's structure the same way: ATX headings only — the engine's heading parsers are ATX-only
// too (internal/track/link) — and never inside a fenced code block.

export interface Heading {
  level: number;
  text: string;
}

export interface TocEntry extends Heading {
  // The DOM id of the rendered heading, and the URL hash that scrolls to it.
  id: string;
}

// scanHeadings returns the ATX headings of a Markdown body in document order, skipping fenced code
// blocks (a longer fence closes a shorter one of the same character, per CommonMark).
export function scanHeadings(markdown: string): Heading[] {
  const out: Heading[] = [];
  let fence: string | null = null;
  for (const line of markdown.split("\n")) {
    const fenceMark = /^\s{0,3}(`{3,}|~{3,})/.exec(line)?.[1];
    if (fence !== null) {
      if (fenceMark && fenceMark[0] === fence[0] && fenceMark.length >= fence.length) fence = null;
      continue;
    }
    if (fenceMark) {
      fence = fenceMark;
      continue;
    }
    const heading = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(line);
    if (heading) {
      out.push({ level: heading[1].length, text: heading[2] });
    }
  }
  return out;
}

// tocEntries turns a body into its outline: the heading text as a reader sees it (link and emphasis
// syntax stripped) plus the id its rendered heading carries.
export function tocEntries(markdown: string): TocEntry[] {
  const seen = new Map<string, number>();
  return scanHeadings(markdown).map(({ level, text }) => {
    const label = headingLabel(text);
    const base = slugify(label);
    // Two headings with the same text get -2, -3, … in document order, the convention GitHub uses.
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    return { level, text: label, id: n === 1 ? base : `${base}-${n}` };
  });
}

// headingLabel strips the syntax a heading's text may carry — wiki links, Markdown links, emphasis,
// inline code — leaving what the rendered heading reads as.
function headingLabel(text: string): string {
  return text
    .replace(/\[\[([^|\]]+)(?:\|([^\]]+))?\]\]/g, (_, target: string, alias?: string) => alias ?? target)
    .replace(/\[([^\]]+)\]\([^\s)]*\)/g, "$1")
    .replace(/[*_~`]/g, "")
    .trim();
}

// slugify is the id rule: lowercase, drop everything that is not a letter, number, space or hyphen,
// then spaces to hyphens. Letters and numbers are matched by Unicode property, so a Japanese heading
// keeps its text rather than collapsing to an empty id.
function slugify(label: string): string {
  const slug = label
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .trim()
    .replace(/\s+/g, "-");
  return slug === "" ? "section" : slug;
}

// headingElementID is the DOM id a rendered heading carries, mirroring blockElementID's namespacing
// so a heading anchor can never collide with a block anchor or with app markup.
export function headingElementID(id: string): string {
  return `h-${id}`;
}
