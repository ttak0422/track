// Loader for the vendored draw.io static viewer (public/drawio-viewer-static.min.js, jgraph/drawio
// v31.1.2, Apache-2.0 — see the .LICENSE.txt beside it). draw.io publishes no npm renderer: the npm
// mxgraph package is archived and ships only ~15 basic shapes, so real diagrams silently degrade to
// labeled rectangles. The static viewer is drawio's own renderer with every built-in shape inlined
// and the compressed <diagram> payload handled internally; the price is a hand-vendored script
// (ADR 0065), lazy-injected here so a note without a drawio diagram never downloads it.

// The subset of GraphViewer this integration relies on.
export interface DrawioGraphViewer {
  createViewerForElement: (element: Element) => void;
}

declare global {
  interface Window {
    GraphViewer?: DrawioGraphViewer;
    MathJax?: unknown;
    Editor?: { MathJaxRender?: (element: Element) => void };
    PROXY_URL?: string;
    STYLE_PATH?: string;
    SHAPES_PATH?: string;
    STENCIL_PATH?: string;
    DRAW_MATH_URL?: string;
  }
}

let viewerPromise: Promise<DrawioGraphViewer> | null = null;

// loadDrawioViewer injects the vendored viewer script once and resolves to the GraphViewer global.
// Browser-only: callers run it from effects, never during prerender.
export function loadDrawioViewer(): Promise<DrawioGraphViewer> {
  viewerPromise ??= new Promise((resolve, reject) => {
    if (window.GraphViewer) {
      resolve(window.GraphViewer);
      return;
    }
    // The script defaults these to https://viewer.diagrams.net/… with `window.X || "https://…"`,
    // so the overrides must be TRUTHY — point them at dead local paths so anything the static
    // build did not inline (image proxy, external stencils) 404s locally instead of phoning
    // diagrams.net; the exotic features degrade.
    const dead = `${import.meta.env.BASE_URL}drawio-absent`;
    window.PROXY_URL = dead;
    window.STYLE_PATH = dead;
    window.SHAPES_PATH = dead;
    window.STENCIL_PATH = dead;
    window.DRAW_MATH_URL = dead;
    // The viewer's Editor.initMath() injects MathJax from DRAW_MATH_URL at load, on every page,
    // math or not — its whole body is guarded by `typeof window.MathJax === "undefined"`, so a
    // predefined stub turns it into a no-op. Math labels render as their raw TeX text.
    window.MathJax ??= {};
    const script = document.createElement("script");
    script.src = `${import.meta.env.BASE_URL}drawio-viewer-static.min.js`;
    script.onload = () => {
      // One export-path call site invokes Editor.MathJaxRender unguarded on math-enabled
      // diagrams; with initMath skipped it never got defined, so give it a no-op.
      if (window.Editor && window.Editor.MathJaxRender == null) {
        window.Editor.MathJaxRender = () => {};
      }
      if (window.GraphViewer) {
        resolve(window.GraphViewer);
      } else {
        reject(new Error("draw.io viewer loaded without GraphViewer"));
      }
    };
    script.onerror = () => {
      viewerPromise = null; // a transient fetch failure should not poison every later diagram
      reject(new Error("failed to load the draw.io viewer"));
    };
    document.head.appendChild(script);
  });
  return viewerPromise;
}
