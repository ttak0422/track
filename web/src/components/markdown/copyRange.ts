export interface CopyLineRange {
  start: number;
  end: number;
}

interface CopyLineElement extends HTMLElement {
  dataset: DOMStringMap & {
    copyLineStart?: string;
    copyLineEnd?: string;
  };
}

// resolveCopyLineRange deliberately uses the range's document-order boundaries instead of the
// selection's anchor/focus order: the browser normalizes a Range even when the user dragged backwards.
// The marked block is the smallest source unit the DOM carries. A long paragraph therefore resolves
// to its whole source span; marking inline nodes would improve that precision at the cost of noisy DOM.
export function resolveCopyLineRange(scope: HTMLElement, selection: Selection | null): CopyLineRange | null {
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return null;
  const range = selection.getRangeAt(0);
  const start = lineElementForBoundary(scope, range.startContainer, range.startOffset, true);
  const end = lineElementForBoundary(scope, range.endContainer, range.endOffset, false);
  if (!start || !end) return null;
  return {
    start: Number(start.dataset.copyLineStart),
    end: Number(end.dataset.copyLineEnd),
  };
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

// The compact path:start-end form is easy for an agent to parse; a one-line selection omits the
// redundant end so pasted instructions read as "look at path:12" rather than "path:12-12".
export function copyLineRangeText(path: string, range: CopyLineRange): string {
  return range.start === range.end ? `${path}:${range.start}` : `${path}:${range.start}-${range.end}`;
}
