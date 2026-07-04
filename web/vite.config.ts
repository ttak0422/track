/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Minimal process typing so the config compiles without pulling in @types/node; the Node runtime that
// runs Vite provides it.
declare const process: { env: Record<string, string | undefined> };

// The static-site export build (VITE_TRACK_STATIC=1) is path-routed and prerendered, so it needs a known
// absolute base (SITE_BASE, default "/") baked into the bundle: import.meta.env.BASE_URL then drives both
// the router basepath and asset URLs, keeping the prerender and the hydrating client in agreement. Set
// SITE_BASE=/repo/ when deploying under a GitHub Pages project subpath. The live server build serves from
// root.
const staticBuild = process.env.VITE_TRACK_STATIC === "1";

export default defineConfig({
  base: staticBuild ? process.env.SITE_BASE || "/" : "/",
  plugins: [react()],
  // A literal boolean the bundler folds at build time, so code gated on `!__TRACK_STATIC__` (e.g. the
  // BudouX word-break model) is dead-code-eliminated from the static build rather than merely unused.
  define: {
    __TRACK_STATIC__: JSON.stringify(staticBuild),
  },
  build: {
    manifest: true,
    rollupOptions: {
      output: {
        // Split the big, stable vendor groups out of the app chunk so they download in parallel and stay
        // cached across app-code deploys. The prerendered HTML paints content without JS, so a smaller,
        // parallel-loading initial chunk helps interactivity (TBT/TTI) without hurting LCP. Heavy optional
        // libs (mermaid, pdf.js, KaTeX, cytoscape, d3-force) are already dynamically imported.
        manualChunks(id: string) {
          if (!id.includes("node_modules")) return;
          if (id.includes("/react-dom/") || /\/react\//.test(id) || id.includes("/scheduler/")) return "react";
          if (id.includes("/@tanstack/")) return "tanstack";
          if (id.includes("/budoux/")) return "budoux";
          if (
            /\/(react-markdown|remark-|rehype-|micromark|mdast|hast|unist|unified|vfile|property-information|character-entities|decode-named|space-separated|comma-separated|trim-lines|zwitch|bail|is-plain-obj|ccount|escape-string-regexp|markdown-table|longest-streak|mdurl|devlop|estree|html-|web-namespaces|parse-entities|stringify-entities)/.test(
              id,
            )
          ) {
            return "markdown";
          }
        },
      },
    },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8765",
    },
  },
  test: {
    // jsdom gives the pure helpers a window (viewport size, URL) without a real browser, and the
    // components a DOM to render into.
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test-setup.ts"],
  },
});
