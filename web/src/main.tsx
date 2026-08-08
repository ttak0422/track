import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App, clientAppRouter, hydratePrerenderedState } from "./App";
import { STATIC_MODE } from "./runtime";
import { applyDesignPreview, parseDesignPreview } from "./dev/preview";

const root = document.getElementById("root");

if (!root) {
  throw new Error("missing #root");
}

// Each prerendered route is a directory index (/notes/<id>/index.html). A host — or Lighthouse, or a
// direct link — may serve it at the explicit .../index.html URL; the router only knows the directory
// route, so normalize the address to the directory before it initializes (otherwise the client would
// replace the correct prerendered content with a not-found). Must run before clientAppRouter() builds the
// browser history from location.
if (STATIC_MODE && window.location.pathname.endsWith("/index.html")) {
  const dir = window.location.pathname.slice(0, -"index.html".length);
  window.history.replaceState(window.history.state, "", dir + window.location.search + window.location.hash);
}

// Dev-only (dead code in both builds): ?theme=…&variant=… selects a design-candidate preview
// (ADR 0068) before first paint, so design-shots screenshots address combinations as plain URLs.
// The candidate token blocks ship in their own dev-only import — on /gallery the playground also
// loads them, but any other dev URL needs them injected here for the variant to render.
if (import.meta.env.DEV) {
  applyDesignPreview(parseDesignPreview(window.location.search));
  void import("./dev/candidates.css");
}

// Each deploy replaces every content-hashed chunk, and GitHub Pages caches HTML for up to 10 minutes
// (browser and CDN), so a stale page can lazy-load a chunk that no longer exists. Vite surfaces that
// failed dynamic import as vite:preloadError; one revalidating reload picks up the new deploy. The
// sessionStorage flag stops a reload loop when the fetched HTML is itself still stale.
window.addEventListener("vite:preloadError", (event) => {
  const last = Number(sessionStorage.getItem("track:chunk-reload") ?? 0);
  if (Date.now() - last < 30_000) return;
  sessionStorage.setItem("track:chunk-reload", String(Date.now()));
  event.preventDefault();
  window.location.reload();
});

// The static site prerenders content into #root for a fast first paint; the client then mounts with
// createRoot, which renders fresh over that markup (React discards it) rather than hydrating. This is
// deliberate: TanStack Router wraps route content in a client-only Suspense boundary that a standalone
// prerender cannot reproduce, so hydration would always mismatch. The dehydrated react-query cache
// (window.__TRACK_STATE__) is unlocked and seeded before this render, so the re-render paints the same
// content immediately with no refetch flash. Loading the router before the first render keeps that render
// from briefly showing a pending state over the prerendered content. The live app has an empty #root and
// mounts the same way.
void Promise.all([clientAppRouter().load(), hydratePrerenderedState()]).finally(() => {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
});
