// Rough source-line read-out for the reading surface. rehypeCopyLine stamps every rendered block
// with its span in the note's own source (data-copy-line-start/-end), and the gutter names each
// hundred-line band ("~400") beside the passage where it begins — so the numbers travel with the
// prose as it scrolls, the way a printed page's marginal numbers do. They are a guide, not a
// coordinate, and stay quiet over the first band, where "around line 0" would say nothing the
// scrollbar doesn't.

export const ROUGH_LINE_UNIT = 100;

// One stamped block's position: its first source line, and its top edge in whatever coordinate
// space the caller measured it in — the gutter's own top, in the reading surface. In document order.
export interface LineHintSpan {
  start: number;
  top: number;
}

// One number in the gutter: where to draw it, and what it says.
export interface LineHintMark {
  top: number;
  label: string;
}

// bandMarks keeps the blocks that open a band the gutter has not named yet, so each number appears
// once, level with the passage it starts at. A note whose blocks run backwards (an included excerpt
// from elsewhere) names the band again when it comes back around — the number describes the block
// beside it, not a monotonic ruler.
export function bandMarks(spans: readonly LineHintSpan[], unit: number = ROUGH_LINE_UNIT): LineHintMark[] {
  const marks: LineHintMark[] = [];
  let last: string | null = null;
  for (const span of spans) {
    const label = roughLineLabel(span.start, unit);
    if (label === null || label === last) continue;
    last = label;
    marks.push({ top: span.top, label });
  }
  return marks;
}

// roughLineLabel rounds a source line down to its hundred-line band and formats it ("~400"). It
// returns null while there is no band to name yet — unknown positions, and lines inside the first
// band — rather than showing "~0" at the head of every note.
export function roughLineLabel(line: number | null, unit: number = ROUGH_LINE_UNIT): string | null {
  if (line === null || !Number.isInteger(line) || line < 1) return null;
  const band = Math.floor(line / unit) * unit;
  if (band === 0) return null;
  return `~${band}`;
}
