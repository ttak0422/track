// Runtime mode for the frontend.
//
// The same React app powers two deployments: the live `track web` server (talks to /api/*) and the
// static-site export produced by `track export-site` (no server). STATIC_MODE is baked in at build
// time via VITE_TRACK_STATIC=1 for the static build. In static mode the app reads the pre-generated
// JSON bundle under ./data instead of the HTTP API and runs read-only — no editing, follow, live
// updates, or journal/heatmap writes.
export const STATIC_MODE = import.meta.env.VITE_TRACK_STATIC === "1";

// dataURL resolves a path inside the exported data bundle relative to the current document, so it keeps
// working under any GitHub Pages subpath. Static mode uses hash routing, so document.baseURI stays at
// the site root (index.html) regardless of the in-app route.
export function dataURL(path: string): string {
  if (typeof document !== "undefined") {
    return new URL(`data/${path}`, document.baseURI).toString();
  }
  return `data/${path}`;
}
