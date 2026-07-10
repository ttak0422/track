import { useCallback, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { MarkdownView } from "./MarkdownView";
import { type Slide, splitSlides } from "./slides";
import type { NoteInclude } from "../types";

interface SlideDeckControlProps {
  markdown: string;
  kind?: string;
  includes?: NoteInclude[];
}

// SlideDeckControl presents a note as a slide deck: `---` thematic breaks split the rendered body
// into slides (see slides.ts). It renders a "Slides" toggle when the note has more than one slide,
// and — while the URL hash carries `#slide=N` — a full-viewport deck with keyboard navigation. The
// current slide lives in the hash, so a slide is deep-linkable and works the same on the live
// workspace and the published static site. Each slide renders through MarkdownView, so everything a
// note page shows (diagrams, charts, math, wiki links, includes) works inside a slide.
export function SlideDeckControl({ markdown, kind = "note", includes }: SlideDeckControlProps) {
  const slides = useMemo(() => splitSlides(markdown), [markdown]);
  const count = slides.length;
  // The 1-based slide requested by the hash; null means not presenting. Owned locally and mirrored
  // to the hash with pushState/replaceState (which fire no events); hashchange covers the external
  // changes — back/forward and manual URL edits.
  const [requested, setRequested] = useState<number | null>(slideFromHash);

  useEffect(() => {
    const onHashChange = () => setRequested(slideFromHash());
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  const open = requested !== null && count > 0;
  const index = open ? Math.min(requested, count) - 1 : 0;

  // goTo shows the 1-based slide n (clamped), rewriting the hash in place so flipping through a deck
  // does not pile up history entries.
  const goTo = useCallback(
    (n: number) => {
      const clamped = Math.min(Math.max(n, 1), count);
      window.history.replaceState(null, "", `#slide=${clamped}`);
      setRequested(clamped);
    },
    [count],
  );

  const exit = useCallback(() => {
    window.history.replaceState(null, "", window.location.pathname + window.location.search);
    setRequested(null);
  }, []);

  // Entering pushes one history entry, so the browser's Back button also leaves the deck.
  const enter = () => {
    window.history.pushState(null, "", "#slide=1");
    setRequested(1);
  };

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) {
        return;
      }
      switch (event.key) {
        case "ArrowRight":
        case "ArrowDown":
        case "PageDown":
          goTo(index + 2);
          break;
        case " ":
          goTo(event.shiftKey ? index : index + 2);
          break;
        case "ArrowLeft":
        case "ArrowUp":
        case "PageUp":
          goTo(index);
          break;
        case "Home":
          goTo(1);
          break;
        case "End":
          goTo(count);
          break;
        case "Escape":
          exit();
          break;
        default:
          return;
      }
      event.preventDefault();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open, index, count, goTo, exit]);

  // The page behind the deck must not scroll while presenting.
  useEffect(() => {
    if (!open) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, [open]);

  if (count < 2 && !open) return null;

  const slide = open ? slides[index] : null;

  return (
    <>
      {count > 1 ? (
        <button className="slide-toggle" type="button" onClick={enter}>
          Slides
        </button>
      ) : null}
      {/* Portaled to <body>: the deck must stack above the fixed sidebar rail and its popups, which
          the float-controls' own stacking context could not. Never rendered by the prerender (the
          hash is empty there), so the server renderer's no-portal limitation is moot. */}
      {slide
        ? createPortal(
            <div className="slide-deck" role="dialog" aria-modal="true" aria-label="Slide view">
              <button className="slide-deck-close" type="button" onClick={exit}>
                Close
              </button>
              <div className="slide-deck-stage">
                {/* Keyed by index so per-slide widgets (Mermaid, charts) remount cleanly. */}
                <div className="slide-deck-slide" key={index}>
                  <MarkdownView markdown={slide.text} kind={kind} includes={slideIncludes(slide, includes)} />
                </div>
              </div>
              <div className="slide-deck-nav">
                <button type="button" aria-label="Previous slide" disabled={index === 0} onClick={() => goTo(index)}>
                  ←
                </button>
                <span className="slide-deck-count" aria-live="polite">
                  {index + 1} / {count}
                </span>
                <button
                  type="button"
                  aria-label="Next slide"
                  disabled={index === count - 1}
                  onClick={() => goTo(index + 2)}
                >
                  →
                </button>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

// slideFromHash reads the 1-based slide number out of the URL hash, or null when not presenting.
function slideFromHash(): number | null {
  if (typeof window === "undefined") return null;
  const match = /^#slide=(\d+)$/.exec(window.location.hash);
  return match ? Math.max(1, Number(match[1])) : null;
}

// slideIncludes narrows a note's resolved ![[...]] includes to the ones inside a slide, rebasing
// their line numbers onto the slide's own text so MarkdownView splices them at the right lines.
function slideIncludes(slide: Slide, includes?: NoteInclude[]): NoteInclude[] | undefined {
  if (!includes || includes.length === 0) return undefined;
  const within = includes.filter(
    (inc) => inc.line >= slide.startLine && inc.line < slide.startLine + slide.lineCount,
  );
  if (within.length === 0) return undefined;
  return within.map((inc) => ({ ...inc, line: inc.line - slide.startLine }));
}
