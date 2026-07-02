import type { MermaidConfig } from "mermaid";
import { type PointerEvent, useEffect, useRef, useState } from "react";
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
  const panZoom = usePanZoom(text);

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

  const { transform, viewportRef, reset, handlers } = panZoom;
  return (
    <div className="mermaid-diagram">
      <div className="mermaid-viewport" ref={viewportRef} {...handlers}>
        <div
          className="mermaid-pan"
          style={{
            transform: `translate(${transform.x}px, ${transform.y}px) scale(${transform.scale})`,
            transformOrigin: "0 0",
          }}
          role="img"
          aria-label="Mermaid diagram"
          dangerouslySetInnerHTML={{ __html: state.svg }}
        />
      </div>
      <button
        className="mermaid-reset"
        type="button"
        onClick={reset}
        aria-label="Reset diagram view"
        title="Reset diagram view"
      >
        ↺
      </button>
    </div>
  );
}

interface Transform {
  x: number;
  y: number;
  scale: number;
}

const identityTransform: Transform = { x: 0, y: 0, scale: 1 };

// Pan (pointer drag) and zoom (wheel, toward the cursor) applied as a CSS transform on the diagram.
// resetKey resets the view when the rendered diagram changes.
function usePanZoom(resetKey: unknown) {
  const [transform, setTransform] = useState<Transform>(identityTransform);
  const viewportRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ px: number; py: number; x: number; y: number } | null>(null);

  useEffect(() => setTransform(identityTransform), [resetKey]);

  // Wheel zoom needs a non-passive listener so it can preventDefault the page scroll.
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    function onWheel(event: WheelEvent) {
      event.preventDefault();
      const rect = el!.getBoundingClientRect();
      const cx = event.clientX - rect.left;
      const cy = event.clientY - rect.top;
      setTransform((prev) => {
        const scale = clamp(prev.scale * Math.exp(-event.deltaY * 0.0015), 0.2, 8);
        const k = scale / prev.scale;
        // Keep the point under the cursor fixed while scaling.
        return { scale, x: cx - (cx - prev.x) * k, y: cy - (cy - prev.y) * k };
      });
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

  return {
    transform,
    viewportRef,
    reset: () => setTransform(identityTransform),
    handlers: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel: onPointerUp },
  };
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

function useThemeVersion(): number {
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
