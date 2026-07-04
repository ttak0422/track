import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App, clientAppRouter } from "./App";
import { STATIC_MODE } from "./runtime";

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

// The static site prerenders content into #root for a fast first paint; the client then mounts with
// createRoot, which renders fresh over that markup (React discards it) rather than hydrating. This is
// deliberate: TanStack Router wraps route content in a client-only Suspense boundary that a standalone
// prerender cannot reproduce, so hydration would always mismatch. The dehydrated react-query cache
// (window.__TRACK_STATE__, read in App) is seeded before this render, so the re-render paints the same
// content immediately with no refetch flash. Loading the router before the first render keeps that render
// from briefly showing a pending state over the prerendered content. The live app has an empty #root and
// mounts the same way.
void clientAppRouter()
  .load()
  .finally(() => {
    createRoot(root).render(
      <StrictMode>
        <App />
      </StrictMode>,
    );
  });
