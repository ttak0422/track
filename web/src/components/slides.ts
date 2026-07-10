// splitSlides cuts rendered note markdown into slides at `---` thematic breaks, so any note doubles
// as a presentable deck (see SlideDeck.tsx). The split mirrors how the page itself renders: a `---`
// inside a fenced code block is code, and a `---` directly under paragraph text is a setext H2
// underline — neither starts a new slide. Only a hyphen break (`---`, `- - -`, `----`) separates
// slides; `***`/`___` stay ordinary horizontal rules.

export interface Slide {
  text: string;
  // 0-based line of the slide's first line in the input, so callers can rebase the line numbers of
  // per-note data (the resolved ![[...]] includes) onto the slide's own text.
  startLine: number;
  lineCount: number;
}

// A hyphen thematic break: up to 3 leading spaces, then 3+ hyphens optionally spaced (`- - -`).
const breakPattern = /^ {0,3}(?:-[ \t]*){3,}$/;
const fencePattern = /^ {0,3}(`{3,}|~{3,})/;
const atxHeadingPattern = /^ {0,3}#{1,6}([ \t]|$)/;

export function splitSlides(markdown: string): Slide[] {
  const lines = markdown.split("\n");
  const separators: number[] = [];
  let fence: { char: string; length: number } | null = null;
  // Whether a `---` on the current line would be a thematic break rather than a setext underline:
  // true at document start, after a blank line, after an ATX heading, or after another separator.
  let breakable = true;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (fence) {
      const close = fencePattern.exec(line);
      if (close && close[1][0] === fence.char && close[1].length >= fence.length && line.trim() === close[1]) {
        fence = null;
      }
      breakable = false;
      continue;
    }
    const open = fencePattern.exec(line);
    if (open) {
      fence = { char: open[1][0], length: open[1].length };
      breakable = false;
      continue;
    }
    if (breakable && breakPattern.test(line)) {
      separators.push(i);
      continue;
    }
    breakable = line.trim() === "" || atxHeadingPattern.test(line);
  }

  const slides: Slide[] = [];
  let start = 0;
  for (const boundary of [...separators, lines.length]) {
    const chunk = lines.slice(start, boundary);
    // Blank chunks (leading/trailing/consecutive separators) make no slide.
    if (chunk.some((l) => l.trim() !== "")) {
      slides.push({ text: chunk.join("\n"), startLine: start, lineCount: boundary - start });
    }
    start = boundary + 1;
  }
  return slides;
}
