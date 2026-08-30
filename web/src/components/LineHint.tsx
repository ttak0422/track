import { useEffect, useRef, useState } from "react";
import { bandMarks, type LineHintMark, type LineHintSpan } from "./roughLine";

// LineHint is the reading surface's rough line marker: small faint numbers in the gutter left of the
// prose column, one per hundred-line band of the source file, each level with the passage that band
// begins at. They sit in the flow of the note rather than at a fixed height, so they travel with the
// prose as the reader scrolls — the marginal numbering of a printed page, not a read-out of where
// the viewport happens to be. Reading mode only — the callers mount it inside .note-preview and
// never alongside a textarea.
//
// It mounts as the first child of .note-preview and measures from there: the stamped blocks are its
// siblings (rehypeCopyLine's data-copy-line-start), and each mark is placed at its block's offset
// from this element's own top. Nothing here listens to scrolling; the marks move because the note
// they are pinned to moves.
export function LineHint() {
  const ref = useRef<HTMLDivElement>(null);
  const [marks, setMarks] = useState<LineHintMark[]>([]);

  useEffect(() => {
    const el = ref.current;
    const host = el?.parentElement;
    if (!el || !host) return;

    let scheduled = false;
    let frameId = 0;
    const measure = () => {
      scheduled = false;
      frameId = 0;
      // Both edges come from the same coordinate space, so the difference is the block's offset from
      // the gutter's top however far the surface has been scrolled.
      const base = el.getBoundingClientRect().top;
      // querySelectorAll walks document order, so spans arrive sorted by position.
      const spans: LineHintSpan[] = [];
      for (const block of host.querySelectorAll<HTMLElement>("[data-copy-line-start]")) {
        const start = Number(block.dataset.copyLineStart);
        if (!Number.isInteger(start)) continue;
        spans.push({ start, top: block.getBoundingClientRect().top - base });
      }
      const next = bandMarks(spans);
      setMarks((prev) => (sameMarks(prev, next) ? prev : next));
    };
    const schedule = () => {
      if (scheduled) return;
      scheduled = true;
      frameId = window.requestAnimationFrame(measure);
    };

    schedule();
    window.addEventListener("resize", schedule);
    // The body can swap in after mount (the render query resolves) with no scroll having happened;
    // watch the host rather than guessing at effects upstream.
    const observer = new MutationObserver(schedule);
    observer.observe(host, { childList: true, subtree: true });
    // A block that grows after layout — an image that loads, a diagram that settles — moves every
    // mark below it. ResizeObserver on the host catches the height change a mutation does not.
    const resize =
      typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    resize?.observe(host);
    return () => {
      if (scheduled) window.cancelAnimationFrame(frameId);
      window.removeEventListener("resize", schedule);
      observer.disconnect();
      resize?.disconnect();
    };
  }, []);

  return (
    <div ref={ref} className="line-hint" aria-hidden="true">
      {marks.map((mark) => (
        <span key={`${mark.label}@${mark.top}`} className="line-hint-mark" style={{ top: mark.top }}>
          {mark.label}
        </span>
      ))}
    </div>
  );
}

// Measuring runs on every mutation of a long note; re-rendering only when a number or its place
// actually moved keeps that cheap. Sub-pixel drift is not a move.
function sameMarks(prev: readonly LineHintMark[], next: readonly LineHintMark[]): boolean {
  return (
    prev.length === next.length &&
    prev.every(
      (mark, i) => mark.label === next[i].label && Math.abs(mark.top - next[i].top) < 0.5,
    )
  );
}
