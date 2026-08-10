import type { MermaidConfig } from "mermaid";
import { type PointerEvent, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useThemeVersion } from "../../hooks/useThemeVersion";
import { CodeBlock } from "./CodeBlock";
import { copyText } from "./clipboard";

interface MermaidDiagramProps {
  text: string;
}

// DiagramState is the render lifecycle shared by every async diagram engine (Mermaid, Graphviz):
// the engine turns source text into an SVG string, or fails with a message shown above the source.
export type DiagramState =
  | { status: "loading" }
  | { status: "ready"; svg: string }
  | { status: "error"; message: string };

let renderSequence = 0;

// MermaidDiagram renders fenced ```mermaid blocks in the browser. Mermaid owns parsing and SVG
// generation; securityLevel strict keeps diagram directives from loosening the renderer.
export function MermaidDiagram({ text }: MermaidDiagramProps) {
  const [state, setState] = useState<DiagramState>({ status: "loading" });
  const themeVersion = useThemeVersion();

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

  return <DiagramFrame state={state} source={text} sourceLang="mermaid" label="Mermaid diagram" />;
}

interface DiagramFrameProps {
  state: DiagramState;
  // The block's source text, for the copy button and the error fallback code block.
  source: string;
  sourceLang: string;
  // Accessible name of the rendered visualization, e.g. "Mermaid diagram".
  label: string;
  // Extra class on the root, so engine-specific CSS (e.g. Graphviz dark-mode inversion) can hook in.
  className?: string;
}

// DiagramFrame is the presentation shell shared by the diagram engines: loading placeholder, error
// fallback (message + source), and the fitted pan/zoom viewport with fold/copy/zoom controls.
export function DiagramFrame({ state, source, sourceLang, label, className }: DiagramFrameProps) {
  const svg = state.status === "ready" ? state.svg : null;
  const panZoom = usePanZoom(svg);
  const [enlarged, setEnlarged] = useState(false);
  const lightboxPanZoom = usePanZoom(enlarged ? svg : null, { collapseTall: false });
  const dialogRef = useRef<HTMLDialogElement>(null);
  // A stable element per svg string: pan/zoom re-renders reuse it untouched, so react-dom never
  // rewrites the innerHTML — which would both discard sizeSvgToViewBox's sizing and re-parse a large
  // SVG on every drag frame.
  const svgHost = useMemo(
    () => (svg == null ? null : <div dangerouslySetInnerHTML={{ __html: svg }} />),
    [svg],
  );
  const rootClass = className ? `mermaid-diagram ${className}` : "mermaid-diagram";

  useEffect(() => {
    const dialog = dialogRef.current;
    if (enlarged && svg && dialog && !dialog.open) dialog.showModal();
  }, [enlarged, svg]);

  if (state.status === "error") {
    return (
      <div className={`${rootClass} mermaid-diagram-error`}>
        <p>{state.message}</p>
        <CodeBlock lang={sourceLang} text={source} />
      </div>
    );
  }

  if (state.status === "loading") {
    return <div className={`${rootClass} mermaid-diagram-loading`}>Rendering diagram...</div>;
  }

  const {
    transform,
    viewportRef,
    panRef,
    viewportHeight,
    overflow,
    reset,
    zoomBy,
    handlers,
    collapsed,
    showFoldControl,
    toggleCollapsed,
  } = panZoom;
  const showPopupControl = !collapsed && (showFoldControl || overflow.left || overflow.right);
  return (
    <div className={rootClass} data-collapsed={collapsed || undefined}>
      <div
        className="mermaid-viewport"
        ref={viewportRef}
        data-collapsed={collapsed || undefined}
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
          aria-label={label}
        >
          {svgHost}
        </div>
      </div>
      {collapsed && <div className="mermaid-continuation" aria-hidden="true" />}
      {/* The collapsed preview is inert, so a side fade would advertise a pan it cannot make;
          the fold chip already owns the "there is more" signal until expanded. */}
      {!collapsed && overflow.left && <div className="mermaid-continuation-left" aria-hidden="true" />}
      {!collapsed && overflow.right && <div className="mermaid-continuation-right" aria-hidden="true" />}
      {showFoldControl && (
        <button
          className="mermaid-control mermaid-fold"
          type="button"
          onClick={toggleCollapsed}
          aria-label={collapsed ? "Expand diagram" : "Collapse diagram"}
          title={collapsed ? "Expand diagram" : "Collapse diagram"}
        >
          {collapsed ? (
            <>
              <span aria-hidden="true">▾</span>
              <span>Show full diagram</span>
            </>
          ) : (
            "▴"
          )}
        </button>
      )}
      {!collapsed && (
        <div className="mermaid-controls">
          <CopySource text={source} />
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
          {showPopupControl ? (
            <button
              className="mermaid-control mermaid-open"
              type="button"
              onClick={() => setEnlarged(true)}
              aria-label="Open diagram in popup"
              title="Open diagram in popup"
            >
              ⛶
            </button>
          ) : null}
        </div>
      )}
      {enlarged && svg ? (
        <dialog
          ref={dialogRef}
          className="diagram-lightbox"
          onClose={() => setEnlarged(false)}
          onClick={(event) => {
            if (event.target === dialogRef.current) dialogRef.current.close();
          }}
        >
          <div className="mermaid-controls diagram-lightbox-controls">
            <CopySource text={source} />
            <button
              className="mermaid-control"
              type="button"
              onClick={() => lightboxPanZoom.zoomBy(zoomStep)}
              aria-label="Zoom in"
              title="Zoom in"
            >
              +
            </button>
            <button
              className="mermaid-control"
              type="button"
              onClick={() => lightboxPanZoom.zoomBy(1 / zoomStep)}
              aria-label="Zoom out"
              title="Zoom out"
            >
              −
            </button>
            <button
              className="mermaid-control"
              type="button"
              onClick={lightboxPanZoom.reset}
              aria-label="Reset diagram view"
              title="Reset diagram view"
            >
              ↺
            </button>
          </div>
          <button
            className="mermaid-control diagram-lightbox-close"
            type="button"
            onClick={() => dialogRef.current?.close()}
            aria-label="Close diagram popup"
            title="Close diagram popup"
          >
            ×
          </button>
          <div className={`diagram-lightbox-content ${className ?? ""}`}>
            <div
              className="mermaid-viewport"
              ref={lightboxPanZoom.viewportRef}
              style={
                lightboxPanZoom.viewportHeight != null ? { height: lightboxPanZoom.viewportHeight } : undefined
              }
              {...lightboxPanZoom.handlers}
            >
              <div
                ref={lightboxPanZoom.panRef}
                className="mermaid-pan"
                style={{
                  transform: `translate(${lightboxPanZoom.transform.x}px, ${lightboxPanZoom.transform.y}px) scale(${lightboxPanZoom.transform.scale})`,
                  transformOrigin: "0 0",
                }}
                role="img"
                aria-label={label}
              >
                {svgHost}
              </div>
            </div>
          </div>
        </dialog>
      ) : null}
    </div>
  );
}

// CopySource copies the diagram's raw mermaid source to the clipboard, briefly acknowledging with a
// check — same idiom as CodeBlock's copy button, sized to sit among the mermaid controls.
function CopySource({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(
    () => () => {
      if (resetTimer.current !== undefined) window.clearTimeout(resetTimer.current);
    },
    [],
  );

  async function copy() {
    if (!(await copyText(text))) return;
    setCopied(true);
    if (resetTimer.current !== undefined) window.clearTimeout(resetTimer.current);
    resetTimer.current = window.setTimeout(() => setCopied(false), 1500);
  }

  return (
    <button
      className="mermaid-control"
      type="button"
      onClick={copy}
      aria-label={copied ? "Copied" : "Copy source"}
      title={copied ? "Copied" : "Copy source"}
    >
      {copied ? "✓" : "⧉"}
    </button>
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

// Width target: fitting shrinks a diagram to at most this fraction of the viewport width — until
// the readability floor below binds, past which the diagram runs wider and clips.
const fitWidthRatio = 0.8;

// Readability floor: a wide diagram never fits below this fraction of the ideal scale (12px text
// against a 16px article). Past it the diagram overflows horizontally — clipped at the viewport
// edge and pannable, the horizontal analog of a tall diagram's collapsed preview — instead of
// shrinking the whole visualization to an unreadable thumbnail.
const minReadableRatio = 0.75;

// Font size mermaid renders at (pinned in mermaidConfig). The ideal display scale makes diagram
// text match the surrounding article text: articleFontPx / mermaidFontPx.
const mermaidFontPx = 16;

// A collapsed tall diagram keeps its normal readable fit but reveals only this much. A fade and
// labelled control make the continuation explicit instead of shrinking the whole visualization to illegibility.
const collapsedHeight = 320;

// A diagram whose fitted height exceeds this starts collapsed, so tall visualizations never dominate a
// page being skimmed; the fold button restores the full size.
const autoCollapseHeight = 480;

// Pan (pointer drag) and zoom (wheel/buttons) applied as a CSS transform on the diagram. On first paint
// the diagram is fitted to the ideal scale — diagram text matching the article's font size — shrunk
// only if that would overflow fitWidthRatio of the viewport width, and never below the readability
// floor: a wider diagram keeps legible text, is clipped at the viewport edge, and pans (drag or
// horizontal wheel), with `overflow` naming the clipped sides so the frame can fade them. The
// viewport height is sized to the scaled diagram; reset returns to the fit, and the fit follows
// container resizes until the user pans or zooms. A tall diagram starts collapsed: kept at the
// normal fit scale inside a clipped collapsedHeight preview, interactions off, with a labelled fold
// toggle to expand. `svg` is the rendered markup (null until ready), used to re-fit
// whenever the diagram changes.
function usePanZoom(svg: string | null, { collapseTall = true }: { collapseTall?: boolean } = {}) {
  const [transform, setTransform] = useState<Transform>(identityTransform);
  const [viewportHeight, setViewportHeight] = useState<number | null>(null);
  const [collapsed, setCollapsed] = useState(false);
  // A fold control is useful only when the diagram needed the initial collapsed preview. Small diagrams
  // start fully open and should not grow a permanent close affordance after every render.
  const [showFoldControl, setShowFoldControl] = useState(false);
  const viewportRef = useRef<HTMLDivElement>(null);
  const panRef = useRef<HTMLDivElement>(null);
  const fitRef = useRef<Transform>(identityTransform);
  const naturalRef = useRef({ w: 0, h: 0 });
  const idealScaleRef = useRef(1);
  // Set once the user pans or zooms: container resizes then stop re-fitting (the fit would stomp
  // their view) and only the reset target keeps tracking the width.
  const touchedRef = useRef(false);
  const collapsedRef = useRef(false);
  collapsedRef.current = collapsed;
  // Mirror for the non-passive wheel listener, whose closure would otherwise hold a stale transform.
  const transformRef = useRef(transform);
  transformRef.current = transform;
  // Viewport width as state, so the render-time overflow fades recompute on container resizes even
  // while a touched view has its fit frozen.
  const [viewportW, setViewportW] = useState(0);
  // Axis of the current wheel gesture, latched on its first event like a native scroller's: a
  // diagonal page scroll must not jiggle the diagram on its dx-dominant ticks.
  const gestureRef = useRef<{ axis: "x" | "y"; t: number } | null>(null);
  const dragRef = useRef<{ px: number; py: number; x: number; y: number } | null>(null);

  // applyView recomputes the canonical view (fit or collapsed thumbnail) for the current width.
  function applyView(col: boolean) {
    const viewport = viewportRef.current;
    const { w, h } = naturalRef.current;
    if (!viewport || w === 0) {
      // Unmeasurable (no layout engine): fall back to the last known fit so reset still resets.
      setTransform(fitRef.current);
      return;
    }
    const ideal = idealScaleRef.current;
    const view = col
      ? computeCollapsedFit(w, h, viewport.clientWidth, ideal)
      : computeFit(w, h, viewport.clientWidth, ideal);
    fitRef.current = computeFit(w, h, viewport.clientWidth, ideal).transform;
    setTransform(view.transform);
    setViewportHeight(view.height);
  }

  // Measure after the SVG is in the DOM but before paint, so the initial fit shows without a flash.
  // .mermaid-pan is width:fit-content, so its offset size is the diagram's natural (untransformed) size.
  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    const pan = panRef.current;
    if (!svg || !viewport || !pan) return;
    sizeSvgToViewBox(pan);
    const naturalW = pan.offsetWidth;
    const naturalH = pan.offsetHeight;
    if (naturalW === 0 || naturalH === 0) return;
    naturalRef.current = { w: naturalW, h: naturalH };
    setViewportW(viewport.clientWidth);
    idealScaleRef.current = measureIdealScale(viewport);
    touchedRef.current = false;
    const { height } = computeFit(naturalW, naturalH, viewport.clientWidth, idealScaleRef.current);
    const startCollapsed = collapseTall && height > autoCollapseHeight;
    setShowFoldControl(startCollapsed);
    setCollapsed(startCollapsed);
    applyView(startCollapsed);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [svg]);

  // Follow container width changes (a widened pane or window): the diagram re-fits — scale and
  // viewport height included — instead of keeping its old size in a larger box. Keyed on svg: the
  // viewport div only mounts once rendering succeeds, so a mount-time ([]) effect would observe
  // nothing.
  useEffect(() => {
    const el = viewportRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let lastW = el.clientWidth;
    const ro = new ResizeObserver(() => {
      const w = el.clientWidth;
      if (w === 0 || naturalRef.current.w === 0) return;
      // Keep the overflow fades honest even when a touched view skips the re-fit below.
      setViewportW(w);
      if (w === lastW) return;
      lastW = w;
      idealScaleRef.current = measureIdealScale(el);
      const { w: nw, h: nh } = naturalRef.current;
      fitRef.current = computeFit(nw, nh, w, idealScaleRef.current).transform;
      if (!touchedRef.current) {
        applyView(collapsedRef.current);
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [svg]);

  // Wheel zoom follows the charts' convention (db676ce): a plain wheel keeps scrolling the page,
  // Shift+wheel zooms, and a trackpad pinch (ctrl+wheel) zooms instead of scaling the whole page.
  // Non-passive so the zooming cases can preventDefault. A collapsed thumbnail is inert either way.
  // Keyed on svg for the same mount-timing reason as the resize observer above.
  // Layout, not passive: the listener has to exist by the time the diagram is on screen. A passive
  // effect leaves a window where the SVG is in the DOM and the wheel does nothing — small in a
  // browser, and wide enough in the tests that the zoom case failed about one run in ten.
  useLayoutEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    function onWheel(event: WheelEvent) {
      if (collapsedRef.current) return;
      if (!event.shiftKey && !event.ctrlKey) {
        // A horizontal trackpad swipe pans a clipped wide diagram, as it would scroll any other
        // overflow-x region; a vertical wheel still scrolls the page past the diagram.
        const g = gestureRef.current;
        const axis =
          g != null && event.timeStamp - g.t < 250
            ? g.axis
            : Math.abs(event.deltaX) > Math.abs(event.deltaY)
              ? "x"
              : "y";
        gestureRef.current = { axis, t: event.timeStamp };
        if (axis === "y") return;
        const viewW = el!.clientWidth;
        const scaledW = naturalRef.current.w * transformRef.current.scale;
        if (scaledW <= viewW + 1) return;
        // At an end of the pan the event is left unconsumed, so a swipe past the edge falls
        // through to the browser (back/forward) and a no-op tick doesn't mark the view touched.
        const x = clamp(transformRef.current.x - event.deltaX, viewW - scaledW, 0);
        if (x === transformRef.current.x) return;
        event.preventDefault();
        touchedRef.current = true;
        setTransform((prev) => ({ ...prev, x }));
        return;
      }
      event.preventDefault();
      touchedRef.current = true;
      // Browsers report Shift+wheel on the horizontal axis; take whichever axis carries the delta.
      const delta = event.deltaY !== 0 ? event.deltaY : event.deltaX;
      const rect = el!.getBoundingClientRect();
      const cx = event.clientX - rect.left;
      const cy = event.clientY - rect.top;
      setTransform((prev) => zoomAt(prev, cx, cy, Math.exp(-delta * 0.0015)));
    }
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [svg]);

  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    if (collapsed) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = { px: event.clientX, py: event.clientY, x: transform.x, y: transform.y };
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    const drag = dragRef.current;
    if (!drag) return;
    touchedRef.current = true;
    setTransform((prev) => ({
      ...prev,
      x: drag.x + (event.clientX - drag.px),
      y: drag.y + (event.clientY - drag.py),
    }));
  }

  function onPointerUp(event: PointerEvent<HTMLDivElement>) {
    if (dragRef.current === null) return;
    dragRef.current = null;
    event.currentTarget.releasePointerCapture(event.pointerId);
  }

  // zoomBy scales toward the viewport center, so the +/- buttons keep the middle of the diagram put.
  function zoomBy(factor: number) {
    const el = viewportRef.current;
    if (!el || collapsed) return;
    touchedRef.current = true;
    const rect = el.getBoundingClientRect();
    setTransform((prev) => zoomAt(prev, rect.width / 2, rect.height / 2, factor));
  }

  // Which sides hide clipped content under the current pan — recomputed per render (pan, zoom, and
  // viewport resizes all set state). Purely visual scroll-shadow-style hints.
  const scaledW = naturalRef.current.w * transform.scale;
  const overflow = {
    left: viewportW > 0 && transform.x < -1,
    right: viewportW > 0 && transform.x + scaledW > viewportW + 1,
  };

  return {
    transform,
    viewportRef,
    panRef,
    viewportHeight,
    overflow,
    reset: () => {
      touchedRef.current = false;
      setCollapsed(false);
      applyView(false);
    },
    zoomBy,
    handlers: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel: onPointerUp },
    collapsed,
    showFoldControl,
    toggleCollapsed: () => {
      touchedRef.current = false;
      setCollapsed(!collapsed);
      applyView(!collapsed);
    },
  };
}

// computeCollapsedFit keeps the normal, readable fit scale and clips only the viewport height. The
// continuation fade and fold button communicate that more content follows below the preview.
export function computeCollapsedFit(
  naturalW: number,
  naturalH: number,
  viewW: number,
  idealScale = 1,
): { transform: Transform; height: number } {
  const fit = computeFit(naturalW, naturalH, viewW, idealScale);
  return { transform: fit.transform, height: Math.min(fit.height, collapsedHeight) };
}

// computeFit shows a naturalW×naturalH diagram at idealScale (diagram text matches the article's
// font size), shrinking only if that overflows fitWidthRatio of viewW — but never below the
// readability floor: a wider diagram keeps legible text and is clipped at the viewport edge
// instead. Centers a fitting diagram, left-aligns a clipped one (reading order shows the start),
// and returns the viewport height that hugs the scaled diagram.
export function computeFit(
  naturalW: number,
  naturalH: number,
  viewW: number,
  idealScale = 1,
): { transform: Transform; height: number } {
  const scale = clamp(
    Math.min((viewW * fitWidthRatio) / naturalW, idealScale),
    minReadableRatio * idealScale,
    8,
  );
  const x = Math.max((viewW - naturalW * scale) / 2, 0);
  return { transform: { scale, x, y: 0 }, height: naturalH * scale };
}

// sizeSvgToViewBox pins the rendered SVG to its natural (viewBox) pixel size. Mermaid emits
// width="100%", which cannot resolve inside the width:fit-content pan, so the SVG would fall back to
// the 300×150 replaced-element default — squishing wide diagrams and making every measurement
// (fit scale, collapse detection) read the squished size instead of the diagram's real one.
function sizeSvgToViewBox(pan: HTMLElement) {
  const svgEl = pan.querySelector("svg");
  const vb = svgEl?.viewBox?.baseVal;
  if (!svgEl || !vb || vb.width <= 0 || vb.height <= 0) return;
  svgEl.style.width = `${vb.width}px`;
  svgEl.style.height = `${vb.height}px`;
  svgEl.style.maxWidth = "none";
}

// measureIdealScale reads the article font size at the diagram's position; scaling the 16px-rendered
// SVG by this makes diagram text the same size as the surrounding text. jsdom (and any unstyled
// context) reports no font size — fall back to 1.
function measureIdealScale(el: HTMLElement): number {
  const px = Number.parseFloat(getComputedStyle(el).fontSize);
  return px > 0 ? px / mermaidFontPx : 1;
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

export function mermaidConfig(): MermaidConfig {
  const css = getComputedStyle(document.documentElement);
  const color = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback;

  return {
    startOnLoad: false,
    securityLevel: "strict",
    theme: "base",
    themeVariables: {
      // The base theme derives every color we don't pin (edge labels, section fills, cluster
      // titles, …) for a light surface unless told otherwise; on the dark theme that left #333-ish
      // text on dark panels. darkMode flips those derivations, textColor pins the biggest offender.
      darkMode: isDarkColor(color("--bg", "#fbfaf8")),
      textColor: color("--text", "#1a1a18"),
      // Pinned (mermaid's default, but relied on by measureIdealScale) so display scale can map
      // diagram text onto the article's font size.
      fontSize: `${mermaidFontPx}px`,
      // The diagram has no decorative bed of its own; keep the page behind it visible and let nodes
      // carry the visual structure.
      background: "transparent",
      primaryColor: color("--panel", "#ffffff"),
      primaryTextColor: color("--text", "#1a1a18"),
      primaryBorderColor: color("--line-node", "#8e8c84"),
      secondaryColor: color("--panel", "#ffffff"),
      tertiaryColor: color("--panel-soft", "#f3f2ee"),
      lineColor: color("--muted", "#5e5d58"),
      noteBkgColor: color("--panel", "#ffffff"),
      noteTextColor: color("--text", "#1a1a18"),
      noteBorderColor: color("--line", "#e6e4de"),
    },
    fontFamily:
      css.getPropertyValue("--font-sans").trim() ||
      '"IBM Plex Sans JP", Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  };
}

// isDarkColor reads a theme token's perceived luminance, so dark detection follows whatever theme
// resolved the tokens instead of duplicating the data-theme / prefers-color-scheme cascade. Both
// notations are accepted: the tokens are written as hex, but they are registered custom properties
// (see styles.css), so getPropertyValue hands back the computed "rgb(r, g, b)" instead.
export function isDarkColor(color: string): boolean {
  const hex = /^#([0-9a-f]{6})$/i.exec(color);
  const rgb = /^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)/i.exec(color);
  let r: number;
  let g: number;
  let b: number;
  if (hex) {
    const v = Number.parseInt(hex[1], 16);
    [r, g, b] = [(v >> 16) & 0xff, (v >> 8) & 0xff, v & 0xff];
  } else if (rgb) {
    [r, g, b] = [Number(rgb[1]), Number(rgb[2]), Number(rgb[3])];
  } else {
    return false;
  }
  const luminance = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
  return luminance < 0.5;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== "") {
    return `Mermaid render failed: ${error.message}`;
  }
  return "Mermaid render failed.";
}
