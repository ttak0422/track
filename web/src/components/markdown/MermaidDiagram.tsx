import type { MermaidConfig } from "mermaid";
import { type PointerEvent, useEffect, useLayoutEffect, useRef, useState } from "react";
import { CodeBlock } from "./CodeBlock";

interface MermaidDiagramProps {
  text: string;
}

type DiagramState =
  | { status: "loading" }
  | { status: "ready"; svg: string }
  | { status: "error"; message: string };

let renderSequence = 0;

// MermaidDiagram renders fenced ```mermaid blocks in the browser. Mermaid owns parsing and SVG
// generation; securityLevel strict keeps diagram directives from loosening the renderer.
export function MermaidDiagram({ text }: MermaidDiagramProps) {
  const [state, setState] = useState<DiagramState>({ status: "loading" });
  const themeVersion = useThemeVersion();
  const panZoom = usePanZoom(state.status === "ready" ? state.svg : null);

  useEffect(() => {
    let cancelled = false;
    const renderID = `track-mermaid-${++renderSequence}`;
    setState({ status: "loading" });

    async function renderDiagram() {
      try {
        const { default: mermaid } = await import("mermaid");
        mermaid.initialize(mermaidConfig());
        const { svg } = await mermaid.render(renderID, text);
        if (!cancelled) setState({ status: "ready", svg });
      } catch (error) {
        if (!cancelled) setState({ status: "error", message: errorMessage(error) });
      }
    }

    void renderDiagram();
    return () => {
      cancelled = true;
    };
  }, [text, themeVersion]);

  if (state.status === "error") {
    return (
      <div className="mermaid-diagram mermaid-diagram-error">
        <p>{state.message}</p>
        <CodeBlock lang="mermaid" text={text} />
      </div>
    );
  }

  if (state.status === "loading") {
    return <div className="mermaid-diagram mermaid-diagram-loading">Rendering diagram...</div>;
  }

  const { transform, viewportRef, panRef, viewportHeight, reset, zoomBy, handlers } = panZoom;
  return (
    <div className="mermaid-diagram">
      <div
        className="mermaid-viewport"
        ref={viewportRef}
        style={viewportHeight != null ? { height: viewportHeight } : undefined}
        {...handlers}
      >
        <div
          className="mermaid-pan"
          ref={panRef}
          style={{
            transform: `translate(${transform.x}px, ${transform.y}px) scale(${transform.scale})`,
            transformOrigin: "0 0",
          }}
          role="img"
          aria-label="Mermaid diagram"
          dangerouslySetInnerHTML={{ __html: state.svg }}
        />
      </div>
      <div className="mermaid-controls">
        <button
          className="mermaid-control"
          type="button"
          onClick={() => zoomBy(zoomStep)}
          aria-label="Zoom in"
          title="Zoom in"
        >
          +
        </button>
        <button
          className="mermaid-control"
          type="button"
          onClick={() => zoomBy(1 / zoomStep)}
          aria-label="Zoom out"
          title="Zoom out"
        >
          −
        </button>
        <button
          className="mermaid-control"
          type="button"
          onClick={reset}
          aria-label="Reset diagram view"
          title="Reset diagram view"
        >
          ↺
        </button>
      </div>
    </div>
  );
}

// Per-click zoom factor for the +/- controls.
const zoomStep = 1.3;

interface Transform {
  x: number;
  y: number;
  scale: number;
}

const identityTransform: Transform = { x: 0, y: 0, scale: 1 };

// Fraction of the viewport width the diagram fills on first paint.
const fitWidthRatio = 0.8;

// Pan (pointer drag) and zoom (wheel/buttons) applied as a CSS transform on the diagram. On first paint
// the diagram is fitted to fitWidthRatio of the viewport width, and the viewport height is sized to the
// scaled diagram; reset returns to that fit. `svg` is the rendered markup (null until ready), used to
// re-fit whenever the diagram changes.
function usePanZoom(svg: string | null) {
  const [transform, setTransform] = useState<Transform>(identityTransform);
  const [viewportHeight, setViewportHeight] = useState<number | null>(null);
  const viewportRef = useRef<HTMLDivElement>(null);
  const panRef = useRef<HTMLDivElement>(null);
  const fitRef = useRef<Transform>(identityTransform);
  const dragRef = useRef<{ px: number; py: number; x: number; y: number } | null>(null);

  // Measure after the SVG is in the DOM but before paint, so the initial fit shows without a flash.
  // .mermaid-pan is width:fit-content, so its offset size is the diagram's natural (untransformed) size.
  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    const pan = panRef.current;
    if (!svg || !viewport || !pan) return;
    const naturalW = pan.offsetWidth;
    const naturalH = pan.offsetHeight;
    if (naturalW === 0 || naturalH === 0) return;
    const { transform: fit, height } = computeFit(naturalW, naturalH, viewport.clientWidth);
    fitRef.current = fit;
    setTransform(fit);
    setViewportHeight(height);
  }, [svg]);

  // Wheel zoom needs a non-passive listener so it can preventDefault the page scroll.
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    function onWheel(event: WheelEvent) {
      event.preventDefault();
      const rect = el!.getBoundingClientRect();
      const cx = event.clientX - rect.left;
      const cy = event.clientY - rect.top;
      setTransform((prev) => zoomAt(prev, cx, cy, Math.exp(-event.deltaY * 0.0015)));
    }
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = { px: event.clientX, py: event.clientY, x: transform.x, y: transform.y };
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    const drag = dragRef.current;
    if (!drag) return;
    setTransform((prev) => ({
      ...prev,
      x: drag.x + (event.clientX - drag.px),
      y: drag.y + (event.clientY - drag.py),
    }));
  }

  function onPointerUp(event: PointerEvent<HTMLDivElement>) {
    dragRef.current = null;
    event.currentTarget.releasePointerCapture(event.pointerId);
  }

  // zoomBy scales toward the viewport center, so the +/- buttons keep the middle of the diagram put.
  function zoomBy(factor: number) {
    const el = viewportRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    setTransform((prev) => zoomAt(prev, rect.width / 2, rect.height / 2, factor));
  }

  return {
    transform,
    viewportRef,
    panRef,
    viewportHeight,
    reset: () => setTransform(fitRef.current),
    zoomBy,
    handlers: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel: onPointerUp },
  };
}

// computeFit scales a naturalW×naturalH diagram to fill fitWidthRatio of viewW, centers it
// horizontally, and returns the viewport height that hugs the scaled diagram.
export function computeFit(
  naturalW: number,
  naturalH: number,
  viewW: number,
): { transform: Transform; height: number } {
  const scale = clamp((viewW * fitWidthRatio) / naturalW, 0.2, 8);
  return { transform: { scale, x: (viewW - naturalW * scale) / 2, y: 0 }, height: naturalH * scale };
}

// zoomAt multiplies the scale by factor while keeping the point (cx, cy) fixed in the viewport.
function zoomAt(prev: Transform, cx: number, cy: number, factor: number): Transform {
  const scale = clamp(prev.scale * factor, 0.2, 8);
  const k = scale / prev.scale;
  return { scale, x: cx - (cx - prev.x) * k, y: cy - (cy - prev.y) * k };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function mermaidConfig(): MermaidConfig {
  const css = getComputedStyle(document.documentElement);
  const color = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback;

  return {
    startOnLoad: false,
    securityLevel: "strict",
    theme: "base",
    themeVariables: {
      background: color("--panel", "#ffffff"),
      primaryColor: color("--panel-soft", "#f6f6f3"),
      primaryTextColor: color("--text", "#20231f"),
      primaryBorderColor: color("--line", "#d9d6cd"),
      secondaryColor: color("--panel", "#ffffff"),
      tertiaryColor: color("--bg", "#faf9f5"),
      lineColor: color("--muted", "#666a60"),
      noteBkgColor: color("--panel-soft", "#f6f6f3"),
      noteTextColor: color("--text", "#20231f"),
      noteBorderColor: color("--line", "#d9d6cd"),
    },
    fontFamily: 'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  };
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== "") {
    return `Mermaid render failed: ${error.message}`;
  }
  return "Mermaid render failed.";
}

// useThemeVersion bumps whenever the app theme changes (the data-theme attribute or the OS
// preference), so theme-dependent renders (Mermaid, ECharts) can redraw with the new colors.
export function useThemeVersion(): number {
  const [version, setVersion] = useState(0);

  useEffect(() => {
    const bump = () => setVersion((value) => value + 1);
    const observer = new MutationObserver(bump);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    media?.addEventListener("change", bump);

    return () => {
      observer.disconnect();
      media?.removeEventListener("change", bump);
    };
  }, []);

  return version;
}
