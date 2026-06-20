export type MarkdownBlock =
  | { type: "heading"; level: 1 | 2 | 3; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: string[] }
  | { type: "task"; checked: boolean; text: string }
  | { type: "embed"; src: string; alt: string }
  | { type: "code"; lang: string; text: string };

// A standalone Markdown image link (![alt](src)) on its own line becomes an embed block: YouTube
// links render as a player, PDFs as a viewer, and anything else as an image. Inline ![...](...) inside
// a paragraph is left as-is so block-level embeds never end up nested in a <p>.
const EMBED_LINE = /^!\[([^\]]*)\]\(([^)\s]+)\)$/;

export type InlinePart =
  | { type: "text"; text: string }
  | { type: "wiki"; target: string; display: string }
  | { type: "link"; href: string; children: InlinePart[] }
  | { type: "code"; text: string }
  | { type: "strong"; children: InlinePart[] }
  | { type: "em"; children: InlinePart[] }
  | { type: "del"; children: InlinePart[] };

export function parseMarkdown(markdown: string): MarkdownBlock[] {
  const blocks: MarkdownBlock[] = [];
  const paragraph: string[] = [];
  let list: string[] = [];
  let inCode = false;
  let codeLang = "";
  let codeLines: string[] = [];

  function flushParagraph() {
    if (paragraph.length === 0) return;
    blocks.push({ type: "paragraph", text: paragraph.join(" ") });
    paragraph.length = 0;
  }

  function flushList() {
    if (list.length === 0) return;
    blocks.push({ type: "list", items: list });
    list = [];
  }

  function flushCode() {
    blocks.push({ type: "code", lang: codeLang, text: codeLines.join("\n") });
    inCode = false;
    codeLang = "";
    codeLines = [];
  }

  for (const rawLine of markdown.split(/\r?\n/)) {
    const line = rawLine.trimEnd();
    const trimmed = line.trim();

    if (inCode) {
      if (/^```/.test(trimmed)) {
        flushCode();
      } else {
        codeLines.push(line);
      }
      continue;
    }

    const openFence = /^```(.*)$/.exec(trimmed);
    if (openFence) {
      flushParagraph();
      flushList();
      inCode = true;
      codeLang = openFence[1].trim();
      continue;
    }

    if (trimmed === "") {
      flushParagraph();
      flushList();
      continue;
    }

    const heading = /^(#{1,3})\s+(.+)$/.exec(trimmed);
    if (heading) {
      flushParagraph();
      flushList();
      blocks.push({ type: "heading", level: heading[1].length as 1 | 2 | 3, text: heading[2] });
      continue;
    }

    const embed = EMBED_LINE.exec(trimmed);
    if (embed) {
      flushParagraph();
      flushList();
      blocks.push({ type: "embed", src: embed[2], alt: embed[1].trim() });
      continue;
    }

    const task = /^[-*]\s+\[([ xX])\]\s+(.+)$/.exec(trimmed);
    if (task) {
      flushParagraph();
      flushList();
      blocks.push({ type: "task", checked: task[1] !== " ", text: task[2] });
      continue;
    }

    const bullet = /^[-*]\s+(.+)$/.exec(trimmed);
    if (bullet) {
      flushParagraph();
      list.push(bullet[1]);
      continue;
    }

    flushList();
    paragraph.push(trimmed);
  }

  flushParagraph();
  flushList();
  if (inCode) {
    flushCode();
  }
  return blocks;
}

export function parseInline(text: string): InlinePart[] {
  const parts: InlinePart[] = [];
  let buf = "";
  let i = 0;

  const flush = () => {
    if (buf) {
      parts.push({ type: "text", text: buf });
      buf = "";
    }
  };

  while (i < text.length) {
    const rest = text.slice(i);

    const wiki = /^\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/.exec(rest);
    if (wiki) {
      flush();
      parts.push({ type: "wiki", target: wiki[1].trim(), display: (wiki[2] ?? wiki[1]).trim() });
      i += wiki[0].length;
      continue;
    }

    // Standard markdown link [text](href). The label may carry nested markup; the href is literal.
    // Checked after the wiki rule so [[...]] never falls through to here.
    const mdLink = /^\[([^\]]+)\]\(([^)\s]+)\)/.exec(rest);
    if (mdLink) {
      flush();
      parts.push({ type: "link", href: mdLink[2], children: parseInline(mdLink[1]) });
      i += mdLink[0].length;
      continue;
    }

    // Inline code is literal: no nested markup inside the backticks.
    if (text[i] === "`") {
      const close = text.indexOf("`", i + 1);
      if (close > i + 1) {
        flush();
        parts.push({ type: "code", text: text.slice(i + 1, close) });
        i = close + 1;
        continue;
      }
    }

    const strong = matchDelimiter(text, i, "**") ?? matchDelimiter(text, i, "__");
    if (strong) {
      flush();
      parts.push({ type: "strong", children: parseInline(strong.inner) });
      i = strong.end;
      continue;
    }

    const del = matchDelimiter(text, i, "~~");
    if (del) {
      flush();
      parts.push({ type: "del", children: parseInline(del.inner) });
      i = del.end;
      continue;
    }

    const em = matchDelimiter(text, i, "*") ?? matchDelimiter(text, i, "_");
    if (em) {
      flush();
      parts.push({ type: "em", children: parseInline(em.inner) });
      i = em.end;
      continue;
    }

    buf += text[i];
    i += 1;
  }

  flush();
  return parts.length > 0 ? parts : [{ type: "text", text }];
}

// matchDelimiter matches a paired inline marker (e.g. ** ... **) starting at i,
// returning the inner text and the index just past the closing marker.
function matchDelimiter(
  text: string,
  i: number,
  marker: string,
): { inner: string; end: number } | null {
  if (!text.startsWith(marker, i)) {
    return null;
  }
  const contentStart = i + marker.length;
  const close = text.indexOf(marker, contentStart);
  if (close === -1 || close === contentStart) {
    return null;
  }
  return { inner: text.slice(contentStart, close), end: close + marker.length };
}

export function excerpt(markdown: string, maxLength = 180): string {
  const text = markdown
    .replace(/```[^\n]*\n?/g, "")
    .replace(/!\[([^\]]*)\]\([^)\s]+\)/g, "$1")
    .replace(/\[\[([^\]|]+)\|([^\]]+)\]\]/g, "$2")
    .replace(/\[\[([^\]]+)\]\]/g, "$1")
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, "$1")
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/^[-*]\s+\[[ xX]\]\s+/gm, "")
    .replace(/^[-*]\s+/gm, "")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/__([^_]+)__/g, "$1")
    .replace(/~~([^~]+)~~/g, "$1")
    .replace(/[*_]([^*_]+)[*_]/g, "$1")
    .replace(/\s+/g, " ")
    .trim();

  if (text.length <= maxLength) return text;
  return `${text.slice(0, maxLength - 1)}...`;
}
