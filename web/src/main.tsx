import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App, clientAppRouter } from "./App";

const root = document.getElementById("root");

if (!root) {
  throw new Error("missing #root");
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
