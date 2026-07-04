// Runtime mode for the frontend.
//
// The same React app powers two deployments: the live `track web` server (talks to /api/*) and the
// static-site export produced by `track export-site` (no server). STATIC_MODE is baked in at build
// time via VITE_TRACK_STATIC=1 for the static build. In static mode the app reads the pre-generated
// JSON bundle under ./data instead of the HTTP API and runs read-only — no editing, follow, live
// updates, or journal/heatmap writes.
export const STATIC_MODE = import.meta.env.VITE_TRACK_STATIC === "1";

// START_PAGE_ID is the root note's published id, injected into index.html at export time (see
// internal/track/site/bundle.go). It lets the static site redirect to the start page on launch without
// waiting for a site.json round-trip. Empty when unset (the live server, or an older bundle).
export const START_PAGE_ID =
  (typeof window !== "undefined" ? window.__trackStartPage : "") || "";

// dataURL resolves a path inside the exported data bundle. The static site is path-routed, so it cannot
// rely on document.baseURI (which varies per route); anchor to the build-time base (BASE_URL, "/" or the
// configured subpath) instead, which is where the data bundle sits. During prerender (no import.meta in
// some contexts) BASE_URL is still inlined at build, and the leading path is matched by the prerender's
// fetch shim.
export function dataURL(path: string): string {
  return `${import.meta.env.BASE_URL}data/${path}`;
}
