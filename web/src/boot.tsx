// The app boot, loaded after first paint by main.tsx. Keeping the split lets the prerendered markup
// be the whole story of the first paint: the reader sees the full note while the router, React, and
// the markdown stack are still downloading, instead of competing with them for bandwidth during it.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App, clientAppRouter, hydratePrerenderedState } from "./App";
import { reloadOnce, setStalePageHandler } from "./runtime";
import { applyDesignPreview, parseDesignPreview } from "./dev/preview";

const root = document.getElementById("root");

if (!root) {
  throw new Error("missing #root");
}

// Dev-only (dead code in both builds): ?theme=…&variant=… selects a design-candidate preview
// (ADR 0068) before first render, so design-shots screenshots address combinations as plain URLs.
// The candidate token blocks ship in their own dev-only import — on /gallery the playground also
// loads them, but any other dev URL needs them injected here for the variant to render.
if (import.meta.env.DEV) {
  applyDesignPreview(parseDesignPreview(window.location.search));
  void import("./dev/candidates.css");
}

setStalePageHandler(reloadOnce);

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
