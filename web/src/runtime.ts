// Runtime mode for the frontend.
//
// The same React app powers two deployments: the live `track web` server (talks to /api/*) and the
// static-site export produced by `track export-site` (no server). STATIC_MODE is baked in at build
// time via VITE_TRACK_STATIC=1 for the static build. In static mode the app reads the pre-generated
// JSON bundle under ./data instead of the HTTP API and runs read-only — no editing, follow, live
// updates, or journal/heatmap writes.
export const STATIC_MODE = import.meta.env.VITE_TRACK_STATIC === "1";

// START_PAGE_ID is the root note's published id, injected into index.html at export time (see
// internal/track/site/bundle.go). The static "/" route renders this note. It is empty when unset (the
// live server) or when the placeholder is left unsubstituted (the Vite dev server / `make site-dev`,
// which serves web/index.html directly) — in which case "/" falls back to the empty state.
export const START_PAGE_ID = (() => {
  const raw = typeof window !== "undefined" ? window.__trackStartPage : "";
  return !raw || raw.startsWith("__TRACK_") ? "" : raw;
})();

// DATA_GENERATION is the fingerprint the exported data bundle was published under, injected into the page
// at export time (see internal/track/site/bundle.go). It makes every data URL specific to the deploy the
// page came from: GitHub Pages serves HTML for up to ten minutes after a deploy, and a shared path would
// let such a page read a newer bundle — or keep serving an older one from cache long after the site
// changed. Empty on the live server and on the Vite dev server (which serves index.html raw).
const DATA_GENERATION = (() => {
  const raw = typeof window !== "undefined" ? window.__trackData : "";
  return !raw || raw.startsWith("__TRACK_") ? "" : raw;
})();

// A published page belongs to one deploy: it carries that deploy's data generation and site key. When it
// cannot read what the site is currently serving — the bundle it names is gone, or a file will not open
// with its key — the page itself is what is out of date. reportStalePage says so; the client entry
// (main.tsx) decides what to do about it, and nothing happens outside the browser.
let stalePageHandler: () => void = () => {};

export function setStalePageHandler(handler: () => void): void {
  stalePageHandler = handler;
}

export function reportStalePage(): void {
  stalePageHandler();
}

// GitHub Pages caches HTML for up to 10 minutes (browser and CDN), so a page can outlive the deploy it
// came from. Two things break when it does, and both recover the same way: one revalidating reload picks
// up the new deploy. The sessionStorage stamp stops a reload loop when the fetched HTML is itself still
// stale. Both entry halves need it (main.tsx guards its own chunk load, app.tsx guards data reads), so
// it lives here beside them.
export function reloadOnce(): boolean {
  const last = Number(sessionStorage.getItem("track:stale-reload") ?? 0);
  if (Date.now() - last < 30_000) return false;
  sessionStorage.setItem("track:stale-reload", String(Date.now()));
  window.location.reload();
  return true;
}

// dataURL resolves a path inside the exported data bundle. The static site is path-routed, so it cannot
// rely on document.baseURI (which varies per route); anchor to the build-time base (BASE_URL, "/" or the
// configured subpath) instead, which is where the data bundle sits. During prerender (no import.meta in
// some contexts) BASE_URL is still inlined at build, and the leading path is matched by the prerender's
// fetch shim.
export function dataURL(path: string): string {
  return `${import.meta.env.BASE_URL}data/${DATA_GENERATION ? `${DATA_GENERATION}/` : ""}${path}`;
}
