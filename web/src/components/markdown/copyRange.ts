export interface CopyLineRange {
  start: number;
  end: number;
  // Independent of the inclusive block lines used by Markdown/Confluence copy.
  source?: { start: SourcePosition; end: SourcePosition };
}

interface SourcePosition {
  line: number;
  column: number;
}

interface CopyLineElement extends HTMLElement {
  dataset: DOMStringMap & {
    copyLineStart?: string;
    copyLineEnd?: string;
  };
}

// resolveCopyLineRange deliberately uses the range's document-order boundaries instead of the
// selection's anchor/focus order: the browser normalizes a Range even when the user dragged backwards.
export function resolveCopyLineRange(scope: HTMLElement, selection: Selection | null, markdown?: string): CopyLineRange | null {
  if (!selection || selection.isCollapsed || selection.rangeCount !== 1) return null;
  const range = selection.getRangeAt(0);
  const start = lineElementForBoundary(scope, range.startContainer, range.startOffset, true);
  const end = lineElementForBoundary(scope, range.endContainer, range.endOffset, false);
  if (!start || !end) return null;
  const result: CopyLineRange = {
    start: Number(start.dataset.copyLineStart),
    end: Number(end.dataset.copyLineEnd),
  };
  if (markdown !== undefined) {
    const from = sourceBoundary(scope, range.startContainer, range.startOffset, true, markdown);
    const to = sourceBoundary(scope, range.endContainer, range.endOffset, false, markdown);
    if (from !== null && to !== null && from < to) {
      result.source = { start: sourcePosition(markdown, from), end: sourcePosition(markdown, to) };
    }
  }
  return result;
}

// Text wrappers survive BudouX's nested word-break spans. Counting their text up to the DOM
// boundary gives a UTF-16 offset; verify the entire wrapper before trusting that offset.
function sourceBoundary(scope: HTMLElement, node: Node, offset: number, isStart: boolean, markdown: string): number | null {
  let point = node;
  let pointOffset = offset;
  if (point instanceof Element && !point.closest("[data-copy-source-start]")) {
    point = point.childNodes[isStart ? offset : offset - 1];
    if (!point) return null;
    while (point.nodeType !== Node.TEXT_NODE && point.childNodes.length > 0) {
      point = isStart ? point.firstChild! : point.lastChild!;
    }
    pointOffset = isStart ? 0 : (point.textContent?.length ?? 0);
  }
  const element = point instanceof Element ? point : point.parentElement;
  const mapped = element?.closest<HTMLElement>("[data-copy-source-start]");
  if (!mapped || !scope.contains(mapped) || hasNonContentAncestor(scope, point)) return null;
  const start = Number(mapped.dataset.copySourceStart);
  const end = Number(mapped.dataset.copySourceEnd);
  if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start < 0 || end > markdown.length || end <= start ||
      mapped.textContent !== markdown.slice(start, end)) return null;
  const prefix = document.createRange();
  prefix.selectNodeContents(mapped);
  prefix.setEnd(point, pointOffset);
  const position = start + prefix.toString().length;
  // A programmatic DOM selection can split a surrogate pair; never advertise that as a character boundary.
  if (position > start && position < end && /[\uD800-\uDBFF]/.test(markdown[position - 1]) && /[\uDC00-\uDFFF]/.test(markdown[position])) return null;
  return position;
}

function sourcePosition(markdown: string, offset: number): SourcePosition {
  const prefix = markdown.slice(0, offset);
  return { line: prefix.split("\n").length, column: offset - prefix.lastIndexOf("\n") };
}

function lineElementForBoundary(
  scope: HTMLElement,
  node: Node,
  offset: number,
  isStart: boolean,
): CopyLineElement | null {
  if (!scope.contains(node) || hasNonContentAncestor(scope, node)) return null;
  const point = node === scope ? boundaryChild(scope, offset, isStart) : node;
  let current: Node | null = point;
  // Nearest marked ancestor, not the top-level block: list items carry their own span, so a selection
  // inside a list resolves to the items it touches instead of to the whole list.
  while (current && current !== scope) {
    if (current instanceof HTMLElement && isCopyLineElement(current)) return current;
    current = current.parentNode;
  }
  return null;
}

function boundaryChild(scope: HTMLElement, offset: number, isStart: boolean): Node | null {
  const index = isStart ? offset : offset - 1;
  return index >= 0 && index < scope.childNodes.length ? scope.childNodes[index] : null;
}

function isCopyLineElement(element: HTMLElement): element is CopyLineElement {
  const start = Number(element.dataset.copyLineStart);
  const end = Number(element.dataset.copyLineEnd);
  return Number.isInteger(start) && start > 0 && Number.isInteger(end) && end >= start;
}

// A diagram's generated SVG and the controls embedded in a source block are presentation, not note
// text. Refusing those endpoints prevents a selection of a chart label or "Copy code" from inheriting
// the line of the surrounding fence.
function hasNonContentAncestor(scope: HTMLElement, node: Node): boolean {
  let current: Node | null = node;
  while (current && current !== scope) {
    if (
      current instanceof Element &&
      current.matches("button, input, select, textarea, svg, canvas, [aria-hidden='true']")
    ) {
      return true;
    }
    current = current.parentNode;
  }
  return false;
}

// Plain-text reference, not a URL: preserve the path verbatim. Precise positions are 1-based,
// UTF-16 columns with an exclusive end; the original line-only fallback stays inclusive.
export function copyLineRangeText(path: string, range: CopyLineRange): string {
  if (range.source) {
    const { start, end } = range.source;
    return `${path}:${start.line}:${start.column}-${end.line}:${end.column}`;
  }
  return range.start === range.end ? `${path}:${range.start}` : `${path}:${range.start}-${range.end}`;
}

// copyLineRangeMarkdown slices the note's own source instead of describing it: rehypeCopyLine stamps
// source positions onto the rendered blocks (include splices keep 1-in/1-out so lines stay put), so
// the resolved range addresses bodyMarkdown's lines directly.
export function copyLineRangeMarkdown(markdown: string, range: CopyLineRange): string {
  const lines = markdown.split("\n");
  return lines.slice(range.start - 1, range.end).join("\n");
}
