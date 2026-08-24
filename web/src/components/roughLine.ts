// Rough source-line read-out for the reading surface. rehypeCopyLine stamps every rendered block
// with its span in the note's own source (data-copy-line-start/-end), so while a reader scrolls,
// the block at the top of the viewport says roughly where in the file they are. The number is a
// guide, not a coordinate: it rounds down to whole hundred-line bands ("~400") and stays quiet over
// the first band, where "around line 0" would say nothing the scrollbar doesn't.

export const ROUGH_LINE_UNIT = 100;

// One stamped block's position: its first source line, and its top edge in whatever coordinate
// space the caller measured both sides in (client rects, say). In document order.
export interface LineHintSpan {
  start: number;
  top: number;
}

// lineAtViewportTop returns the first source line of the block sitting at the viewport's top edge:
// the last stamped block whose top has reached or passed that edge. A gap between blocks (a rule,
// leading) reads as the block above it, so whitespace never blanks the marker; scrolled above all
// content falls back to the first block, so an anchored jump into padding still reports honestly.
export function lineAtViewportTop(spans: readonly LineHintSpan[], viewportTop: number): number | null {
  let current: number | null = null;
  for (const span of spans) {
    if (span.top > viewportTop) break;
    current = span.start;
  }
  if (current === null && spans.length > 0) return spans[0].start;
  return current;
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
