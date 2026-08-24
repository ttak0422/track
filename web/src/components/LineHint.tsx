import { useEffect, useRef, useState } from "react";
import { lineAtViewportTop, roughLineLabel, type LineHintSpan } from "./roughLine";

// LineHint is the reading surface's rough line marker: a small faint "~400" in the gutter left of
// the prose column, following the scroll to say which hundred-line band of the source file is at
// the top of the viewport. Reading mode only — the callers mount it inside .note-preview and never
// alongside a textarea.
//
// It mounts as the first child of .note-preview and works from there: the stamped blocks are its
// siblings (rehypeCopyLine's data-copy-line-start), and the scroller is whichever ancestor actually
// scrolls (.reader in reading mode; a home note may scroll its preview internally instead). One
// rAF-throttled measurement per scroll frame; the label state only moves when the band changes.
export function LineHint() {
  const ref = useRef<HTMLDivElement>(null);
  const [label, setLabel] = useState<string | null>(null);

  useEffect(() => {
    const host = ref.current?.parentElement;
    if (!host) return;
    const scroller = scrollParent(host);
    if (!scroller) return;

    let scheduled = false;
    let frameId = 0;
    const update = () => {
      scheduled = false;
      frameId = 0;
      // querySelectorAll walks document order, so spans arrive sorted by position.
      const spans: LineHintSpan[] = [];
      for (const el of host.querySelectorAll<HTMLElement>("[data-copy-line-start]")) {
        const start = Number(el.dataset.copyLineStart);
        if (!Number.isInteger(start)) continue;
        spans.push({ start, top: el.getBoundingClientRect().top });
      }
      const viewportTop = scroller.getBoundingClientRect().top;
      const next = roughLineLabel(lineAtViewportTop(spans, viewportTop));
      setLabel((prev) => (prev === next ? prev : next));
    };
    const schedule = () => {
      if (scheduled) return;
      scheduled = true;
      frameId = window.requestAnimationFrame(update);
    };

    schedule();
    scroller.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    // The body can swap in after mount (the render query resolves) with no scroll having happened;
    // watch the host rather than guessing at effects upstream.
    const observer = new MutationObserver(schedule);
    observer.observe(host, { childList: true, subtree: true });
    return () => {
      if (scheduled) window.cancelAnimationFrame(frameId);
      scroller.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      observer.disconnect();
    };
  }, []);

  return (
    <div ref={ref} className="line-hint" aria-hidden="true">
      {label ? <span className="line-hint-mark">{label}</span> : null}
    </div>
  );
}

// scrollParent returns the nearest ancestor with a vertical scrolling box, or null when the marker
// has nothing to follow. Generic on purpose: the scrolling element differs between surfaces (the
// full-page reader vs an internally scrolling preview), and hardcoding one class would go stale.
function scrollParent(el: HTMLElement): HTMLElement | null {
  let node: HTMLElement | null = el.parentElement;
  while (node) {
    const overflowY = window.getComputedStyle(node).overflowY;
    if (overflowY === "auto" || overflowY === "scroll") return node;
    node = node.parentElement;
  }
  return null;
}
