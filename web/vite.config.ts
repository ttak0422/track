/// <reference types="vitest/config" />
import { type Connect, defineConfig, type PluginOption } from "vite";
import react from "@vitejs/plugin-react";

// Node builtins used only by dev-server middleware. Imported without @types/node (which would leak Node
// globals into the app's type surface); the Node runtime that runs Vite provides them.
declare const process: { env: Record<string, string | undefined> };
// @ts-expect-error node builtin — no @types/node installed on purpose
import { cpSync, existsSync, readFileSync, realpathSync, statSync } from "node:fs";
// @ts-expect-error node builtin — no @types/node installed on purpose
import { extname, isAbsolute, join, relative, resolve, sep } from "node:path";

const staticBuild = process.env.VITE_TRACK_STATIC === "1";
const siteBase = staticBuild ? (process.env.SITE_BASE || "/").replace(/\/*$/, "/") : "/";
const siteBasePath = siteBase === "/" ? "" : siteBase.slice(0, -1);

// Make passes SITE_OUT as an absolute path; direct `cd web && npm run dev` keeps the historical _site
// default at the repository root.
function exportedSiteDir(): string {
  return resolve("..", process.env.SITE_OUT || "_site");
}

function isPathWithin(root: string, candidate: string): boolean {
  const pathFromRoot = relative(root, candidate);
  return (
    pathFromRoot === "" ||
    (pathFromRoot !== ".." && !pathFromRoot.startsWith(`..${sep}`) && !isAbsolute(pathFromRoot))
  );
}

function notFound(res: Parameters<Connect.NextHandleFunction>[1]): void {
  res.statusCode = 404;
  res.setHeader("content-type", "text/plain; charset=utf-8");
  res.end("Not Found");
}

// serveExportedData lets `make site-dev` preview the help site with the Vite dev server (HMR): the
// static-mode app fetches its data from /data/*, which this middleware serves from the exported bundle
// (_site/data, produced by `make site-data`). Those files are locked (ADR 0069), so they go out as bytes
// and the app opens them itself. Dev-only; the production build reads no data at build time.
function serveExportedData(): PluginOption {
  return {
    name: "track-serve-exported-data",
    apply: "serve",
    configureServer(server) {
      const handler: Connect.NextHandleFunction = (req, res, next) => {
        const url = (req as { url?: string }).url ?? "";
        const pathname = url.split("?", 1)[0] ?? "";
        const dataPrefix = `${siteBasePath}/data/`;
        if (!pathname.startsWith(dataPrefix)) return next();
        let relativeFile: string;
        try {
          relativeFile = decodeURIComponent(pathname.slice(dataPrefix.length));
        } catch {
          return next();
        }
        const dataRoot = resolve(exportedSiteDir(), "data");
        const file = resolve(dataRoot, relativeFile);
        if (!isPathWithin(dataRoot, file) || !existsSync(file) || !statSync(file).isFile()) return next();
        res.setHeader("content-type", "application/octet-stream");
        res.end(readFileSync(file));
      };
      server.middlewares.use(handler);
    },
  };
}

const appContentTypes: Record<string, string> = {
  ".avif": "image/avif",
  ".css": "text/css; charset=utf-8",
  ".csv": "text/csv; charset=utf-8",
  ".gif": "image/gif",
  ".html": "text/html; charset=utf-8",
  ".ico": "image/x-icon",
  ".jpeg": "image/jpeg",
  ".jpg": "image/jpeg",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".md": "text/markdown; charset=utf-8",
  ".mp3": "audio/mpeg",
  ".mp4": "video/mp4",
  ".ogg": "audio/ogg",
  ".pdf": "application/pdf",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".txt": "text/plain; charset=utf-8",
  ".wasm": "application/wasm",
  ".wav": "audio/wav",
  ".webm": "video/webm",
  ".webp": "image/webp",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".xml": "application/xml; charset=utf-8",
  ".yaml": "text/yaml; charset=utf-8",
  ".yml": "text/yaml; charset=utf-8",
};

// In static mode /apps belongs to the exported allowlist, not the live workspace proxy. There is no
// directory listing or SPA fallback: only regular files under one exported app directory are returned.
function serveExportedApps(): PluginOption {
  return {
    name: "track-serve-exported-apps",
    apply: "serve",
    configureServer(server) {
      const handler: Connect.NextHandleFunction = (req, res, next) => {
        const url = (req as { url?: string }).url ?? "/";
        const method = (req as { method?: string }).method ?? "GET";
        const pathname = url.split("?", 1)[0] ?? "/";
        const appsPath = `${siteBasePath}/apps`;
        const appsPrefix = `${appsPath}/`;
        if (pathname === appsPath || pathname === appsPrefix) return notFound(res);
        if (!pathname.startsWith(appsPrefix)) return next();

        if (method !== "GET" && method !== "HEAD") {
          res.statusCode = 405;
          res.setHeader("allow", "GET, HEAD");
          return res.end("Method Not Allowed");
        }

        const rawSegments = pathname.slice(appsPrefix.length).split("/");
        const trailingSlash = rawSegments.at(-1) === "";
        if (trailingSlash) rawSegments.pop();
        if (rawSegments.length === 0 || rawSegments.some((segment) => segment === "")) return notFound(res);

        let segments: string[];
        try {
          segments = rawSegments.map((segment) => decodeURIComponent(segment));
        } catch {
          return notFound(res);
        }
        if (
          segments.some(
            (segment) =>
              segment === "." ||
              segment === ".." ||
              segment === "" ||
              segment.includes("/") ||
              segment.includes("\\") ||
              segment.includes("\0"),
          ) ||
          !/^[a-z0-9][a-z0-9-]{0,62}$/.test(segments[0])
        ) {
          return notFound(res);
        }

        if (segments.length === 1 && !trailingSlash) {
          const query = url.includes("?") ? url.slice(url.indexOf("?")) : "";
          res.statusCode = 308;
          res.setHeader("location", `${pathname}/${query}`);
          return res.end();
        }
        if (segments.length > 1 && trailingSlash) return notFound(res);

        try {
          const outputRoot = realpathSync(exportedSiteDir());
          const appsRoot = realpathSync(resolve(outputRoot, "apps"));
          const appRoot = realpathSync(resolve(appsRoot, segments[0]));
          if (!isPathWithin(outputRoot, appsRoot) || !isPathWithin(appsRoot, appRoot)) return notFound(res);

          const relativeFile = segments.length === 1 ? ["index.html"] : segments.slice(1);
          const file = resolve(appRoot, ...relativeFile);
          if (!isPathWithin(appRoot, file)) return notFound(res);
          const realFile = realpathSync(file);
          if (!isPathWithin(appRoot, realFile) || !statSync(realFile).isFile()) return notFound(res);

          const body = readFileSync(realFile);
          res.statusCode = 200;
          res.setHeader("content-type", appContentTypes[extname(realFile).toLowerCase()] ?? "application/octet-stream");
          res.setHeader("content-length", String(body.length));
          res.setHeader("cache-control", "no-cache");
          res.setHeader("x-content-type-options", "nosniff");
          res.end(method === "HEAD" ? undefined : body);
        } catch {
          notFound(res);
        }
      };
      server.middlewares.use(handler);
    },
  };
}

// The dev server serves index.html raw, so the Go-side placeholders (__TRACK_COLOR_OVERRIDES__ sits
// bare in <head>) would render as literal text on every dev page — and in every design-shots
// screenshot. Strip it in dev; the builds leave it for the server/export to substitute.
//
// The lock key and the data generation are the placeholders dev has to fill rather than drop: without
// them the app cannot find or open the bundle served above. Both come from the same export
// (_site/index.html), so a dev preview reads the site it is previewing.
function stripServerPlaceholders(): PluginOption {
  return {
    name: "track-strip-server-placeholders",
    apply: "serve",
    transformIndexHtml: (html: string) =>
      html
        .replace("__TRACK_COLOR_OVERRIDES__", "")
        .replace("__TRACK_LOCK_KEY__", exportedPageValue("__trackLock"))
        .replace("__TRACK_DATA_GEN__", exportedPageValue("__trackData")),
  };
}

function exportedPageValue(name: string): string {
  const page = join(exportedSiteDir(), "index.html");
  if (!existsSync(page)) return "";
  return new RegExp(`${name}\\s*=\\s*"([^"]*)"`).exec(readFileSync(page, "utf8"))?.[1] ?? "";
}

// The static-site export build (VITE_TRACK_STATIC=1) is path-routed and prerendered, so it needs a known
// absolute base (SITE_BASE, default "/") baked into the bundle: import.meta.env.BASE_URL then drives both
// the router basepath and asset URLs, keeping the prerender and the hydrating client in agreement. Set
// SITE_BASE=/repo/ when deploying under a GitHub Pages project subpath. The live server build serves from
// root.
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
  // LikeC4's browser parser/layout worker loads its bundled Graphviz WASM lazily.
  worker: { format: "es" },
  // Normalize to a trailing slash: GitHub's configure-pages emits base_path as "/repo" (no slash), and
  // BASE_URL consumers concatenate paths onto it ("/repo" + "data/…" would yield "/repodata/…").
  base: siteBase,
  plugins: [
    react(),
    stripServerPlaceholders(),
    ...(staticBuild ? [serveExportedData(), serveExportedApps()] : [bundlePdfjsAssets()]),
  ],
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
          // portableToHtml imports react-dom/server lazily for the Confluence copy, but the generic
          // react-dom rule below would bundle the server renderer into the initial react chunk
          // (renderToString/renderToStaticMarkup were confirmed in it). Keep the server entry in its
          // own chunk so ~56KB gzip stays off the boot path and loads only on a copy action.
          if (id.includes("/react-dom/") && id.includes("server")) return "react-server";
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
      // The track server guards Host and Origin on API requests, so a proxied request has to arrive
      // wearing the server's own address rather than the dev server's. Rewriting Origin is limited to
      // this trusted local proxy; direct browser requests from the static app origin remain refused.
      "/api": {
        target: "http://127.0.0.1:8765",
        changeOrigin: true,
        headers: { origin: "http://127.0.0.1:8765" },
      },
      // Static app launches are workspace routes too. Keep the request on the Go workspace so a missing
      // slash can redirect through Vite and the following absolute Location can reach the adjacent port.
      ...(!staticBuild
        ? {
            "/apps": {
              target: "http://127.0.0.1:8765",
              changeOrigin: true,
              headers: { origin: "http://127.0.0.1:8765" },
            },
          }
        : {}),
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
