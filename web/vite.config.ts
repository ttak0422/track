/// <reference types="vitest/config" />
import { type Connect, defineConfig, type PluginOption } from "vite";
import react from "@vitejs/plugin-react";

// Node builtins used only by the dev-server data middleware. Imported without @types/node (which would
// leak Node globals into the app's type surface); the Node runtime that runs Vite provides them.
declare const process: { env: Record<string, string | undefined> };
// @ts-expect-error node builtin — no @types/node installed on purpose
import { cpSync, existsSync, readFileSync } from "node:fs";
// @ts-expect-error node builtin — no @types/node installed on purpose
import { join } from "node:path";

// serveExportedData lets `make site-dev` preview the help site with the Vite dev server (HMR): the
// static-mode app fetches its JSON from /data/*, which this middleware serves from the exported bundle
// (_site/data, produced by `make site-data`). Dev-only; the production build reads no data at build time.
function serveExportedData(): PluginOption {
  return {
    name: "track-serve-exported-data",
    apply: "serve",
    configureServer(server) {
      const handler: Connect.NextHandleFunction = (req, res, next) => {
        const url = (req as { url?: string }).url ?? "";
        if (!url.startsWith("/data/")) return next();
        const file = join("..", "_site", url); // url already begins with /data/
        if (!existsSync(file)) return next();
        res.setHeader("content-type", "application/json");
        res.end(readFileSync(file, "utf8"));
      };
      server.middlewares.use(handler);
    },
  };
}

// The static-site export build (VITE_TRACK_STATIC=1) is path-routed and prerendered, so it needs a known
// absolute base (SITE_BASE, default "/") baked into the bundle: import.meta.env.BASE_URL then drives both
// the router basepath and asset URLs, keeping the prerender and the hydrating client in agreement. Set
// SITE_BASE=/repo/ when deploying under a GitHub Pages project subpath. The live server build serves from
// root.
const staticBuild = process.env.VITE_TRACK_STATIC === "1";

// bundlePdfjsAssets copies pdf.js' render-time asset directories — cmaps (CID-keyed fonts, i.e. most
// CJK PDFs) and standard_fonts (the standard 14 fonts PDFs may reference without embedding) — into
// the live build under pdfjs/, so `track web` renders such PDFs offline (ADR 0029: app surfaces
// bundle everything). The static-site build skips the ~2.5 MB and loads them from jsDelivr instead,
// pinned to the bundled pdfjs-dist version (see PdfDeck's pdfjsAssetBase).
function bundlePdfjsAssets(): PluginOption {
  let outDir = "dist";
  return {
    name: "track-bundle-pdfjs-assets",
    apply: "build",
    configResolved(config) {
      outDir = config.build.outDir;
    },
    closeBundle() {
      for (const dir of ["cmaps", "standard_fonts"]) {
        cpSync(join("node_modules", "pdfjs-dist", dir), join(outDir, "pdfjs", dir), {
          recursive: true,
        });
      }
    },
  };
}

export default defineConfig({
  // Normalize to a trailing slash: GitHub's configure-pages emits base_path as "/repo" (no slash), and
  // BASE_URL consumers concatenate paths onto it ("/repo" + "data/…" would yield "/repodata/…").
  base: staticBuild ? (process.env.SITE_BASE || "/").replace(/\/*$/, "/") : "/",
  plugins: [react(), ...(staticBuild ? [serveExportedData()] : [bundlePdfjsAssets()])],
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
