// Prerender the static help site: render each route to HTML with its data and write a real file per
// route, so the published site paints content before its JS runs (fast FCP/LCP) and the client hydrates
// the markup. Run after the client + SSR builds and after `track export-site` has produced _site (the
// finalized index.html template + the data bundle under _site/data).
//
//   node scripts/prerender.mjs <siteDir> <serverEntry>
//
// The app fetches its data through data/<file> URLs; a fetch shim resolves those against <siteDir>/data.
// Assets and data are referenced by the build-time base (absolute), so one template works at any route
// depth without a <base> tag.
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import { JSDOM } from "jsdom";

const siteDir = process.argv[2];
const serverEntry = process.argv[3];
if (!siteDir || !serverEntry) {
  console.error("usage: node scripts/prerender.mjs <siteDir> <serverEntry>");
  process.exit(1);
}

const site = JSON.parse(readFileSync(join(siteDir, "data/site.json"), "utf8"));
const notes = JSON.parse(readFileSync(join(siteDir, "data/notes.json"), "utf8")).notes;

// A jsdom window lets the CSR-oriented components read window/localStorage during render unchanged, and
// carries the start-page id the "/" route needs.
const dom = new JSDOM("<!doctype html><html><body></body></html>", {
  url: "http://localhost/",
  pretendToBeVisual: true,
});
const { window } = dom;
window.innerWidth = 1280;
window.innerHeight = 800;
window.__trackStartPage = site.root;
globalThis.window = window;
globalThis.self = window;
globalThis.document = window.document;
globalThis.localStorage = window.localStorage;
Object.defineProperty(globalThis, "navigator", { value: window.navigator, configurable: true });
globalThis.requestAnimationFrame = (cb) => setTimeout(cb, 0);

globalThis.fetch = async (url) => {
  const match = String(url).match(/data\/(.+)$/);
  if (!match) return new Response("", { status: 404 });
  try {
    return new Response(readFileSync(join(siteDir, "data", match[1]), "utf8"), { status: 200 });
  } catch {
    return new Response("", { status: 404 });
  }
};

const { renderPage } = await import(pathToFileURL(serverEntry).href);

const template = readFileSync(join(siteDir, "index.html"), "utf8");
if (!template.includes('<div id="root"></div>')) {
  console.error('prerender: index.html has no empty <div id="root"></div> to fill');
  process.exit(1);
}

// route → output file (relative to siteDir). Every note gets its own directory index so path routing
// resolves a real file on a fallback-less host.
// Every day with note activity gets a real /day page file, so the calendar's day links survive a reload
// on a fallback-less host.
const dayDates = [...new Set(notes.flatMap((n) => n.days ?? []))];

const targets = [
  { route: "/", out: "index.html" },
  { route: "/graph", out: "graph/index.html" },
  { route: "/calendar", out: "calendar/index.html" },
  { route: "/empty", out: "empty/index.html" },
  ...notes.map((n) => ({ route: `/notes/${n.note_id}`, out: `notes/${n.note_id}/index.html` })),
  ...dayDates.map((d) => ({ route: `/day/${d}`, out: `day/${d}/index.html` })),
];

for (const { route, out } of targets) {
  const { html, state } = await renderPage(route);
  const stateScript = `<script>window.__TRACK_STATE__=${serializeState(state)}</script>`;
  const page = template
    .replace('<div id="root"></div>', `<div id="root">${html}</div>`)
    .replace("</head>", `${stateScript}</head>`);
  const file = join(siteDir, out);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, page);
}

console.log(`prerendered ${targets.length} routes into ${siteDir}/`);

// serializeState inlines the dehydrated cache as a JS object literal, escaping "<" so a "</script>" in
// note content cannot close the inline script early.
function serializeState(json) {
  return json.replace(/</g, "\\u003c");
}
