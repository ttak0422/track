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
//
// The bundle is locked (ADR 0069), so this script holds the site's key like the browser does: it reads
// the key out of the page export-site finalized, opens the data files it needs itself, and locks the
// dehydrated cache it inlines — an unlocked copy of that state in the HTML would hand out the very data
// the bundle keeps locked.
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

// export-site now bakes per-page OGP meta into every HTML file, including this root index.html reused
// here as the shared template. Strip that baked head so each prerendered route carries exactly one set
// of og:/twitter: tags — its own, injected below — rather than inheriting the root note's. The <title>
// is replaced (not appended) per route below, so it needs no stripping.
const template = readFileSync(join(siteDir, "index.html"), "utf8")
  .replace(/<meta property="og:[^"]*"[^>]*>/g, "")
  .replace(/<meta name="twitter:[^"]*"[^>]*>/g, "");
if (!template.includes('<div id="root"></div>')) {
  console.error('prerender: index.html has no empty <div id="root"></div> to fill');
  process.exit(1);
}

// A jsdom window lets the CSR-oriented components read window/localStorage during render unchanged, and
// carries the start-page id the "/" route needs plus the site key the lock module reads.
const dom = new JSDOM("<!doctype html><html><body></body></html>", {
  url: "http://localhost/",
  pretendToBeVisual: true,
});
const { window } = dom;
window.innerWidth = 1280;
window.innerHeight = 800;
// Both come out of the page rather than the bundle: the app reads them when its modules load, which is
// before this script can open a locked data file (opening one needs those modules).
window.__trackLock = template.match(/__trackLock\s*=\s*"([^"]*)"/)?.[1] ?? "";
window.__trackStartPage = template.match(/__trackStartPage\s*=\s*"([^"]*)"/)?.[1] ?? "";
globalThis.window = window;
globalThis.self = window;
globalThis.document = window.document;
globalThis.localStorage = window.localStorage;
Object.defineProperty(globalThis, "navigator", { value: window.navigator, configurable: true });
globalThis.requestAnimationFrame = (cb) => setTimeout(cb, 0);

// The app's own staticData does the unlocking, so the shim hands it the published bytes unchanged.
globalThis.fetch = async (url) => {
  const match = String(url).match(/data\/(.+)$/);
  if (!match) return new Response("", { status: 404 });
  try {
    return new Response(readFileSync(join(siteDir, "data", match[1])), { status: 200 });
  } catch {
    return new Response("", { status: 404 });
  }
};

const { renderPage, lock, unlock } = await import(pathToFileURL(serverEntry).href);

// readData opens one file of the bundle for this script's own use (the app's fetch path is above).
// Callers name it by what it holds; the published file is "<name>.bin".
async function readData(name) {
  const bytes = readFileSync(join(siteDir, "data", `${name}.bin`));
  return JSON.parse(await unlock(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)));
}

const site = await readData("site");
const notes = (await readData("notes")).notes;
// Dated tasks give the calendar days of their own: a day nothing was written on can still be a day
// something is planned for, and its page has to exist for the calendar's link to land.
const datedTasks = (await readData("tasks")).tasks ?? [];

// route → output file (relative to siteDir). Every note gets its own directory index so path routing
// resolves a real file on a fallback-less host.
// The calendar is opt-in per site (export-site --calendar, carried in site.json). When on, every day
// with note activity gets a real /day page file, so the calendar's day links survive a reload on a
// fallback-less host.
const dayDates = site.calendar
  ? [
      ...new Set([
        ...notes.flatMap((n) => n.days ?? []),
        ...datedTasks.flatMap((t) => [t.scheduled, t.due].filter(Boolean)),
      ]),
    ]
  : [];

// Every used tag gets a real /tags page file, plus each hierarchical ancestor ("a/b/c" also yields
// "a/b" and "a") so prefix tag pages resolve too. Mirrors internal/track/site tagRoutes.
const tags = [
  ...new Set(
    notes
      .flatMap((n) => n.tags ?? [])
      .flatMap((tag) => tag.split("/").map((_, i, parts) => parts.slice(0, i + 1).join("/"))),
  ),
];

// /tasks is deliberately absent: the bundle carries the dated listing alone, so the page has nothing
// to read and neither it nor its rail button is shown on a published site.
const targets = [
  { route: "/", out: "index.html" },
  { route: "/graph", out: "graph/index.html" },
  ...(site.calendar ? [{ route: "/calendar", out: "calendar/index.html" }] : []),
  { route: "/empty", out: "empty/index.html" },
  ...notes.map((n) => ({ route: `/notes/${n.note_id}`, out: `notes/${n.note_id}/index.html` })),
  ...dayDates.map((d) => ({ route: `/day/${d}`, out: `day/${d}/index.html` })),
  ...tags.map((t) => ({ route: `/tags/${t}`, out: `tags/${t}/index.html` })),
];

const notesById = new Map(notes.map((n) => [n.note_id, n]));
const baseUrl = site.base_url ?? "";

for (const { route, out } of targets) {
  const { html, state } = await renderPage(route);
  // Locked, so the inlined cache is base64 — nothing in it can close the inline script early either.
  const stateScript = `<script>window.__TRACK_STATE__="${await lock(state)}"</script>`;
  const head = (await ogpTags(route)) + stateScript;
  const page = template
    // Use replacer functions: Markdown content may contain `$&`, `$\``, or `$$`, which have special
    // meanings in String.replace replacement strings and would corrupt the generated HTML/state.
    .replace('<div id="root"></div>', () => `<div id="root">${html}</div>`)
    .replace("</head>", () => `${head}</head>`)
    .replace(/<title>[^<]*<\/title>/, () => `<title>${escapeHTML(pageTitle(route))}</title>`);
  const file = join(siteDir, out);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, page);
}

console.log(`prerendered ${targets.length} routes into ${siteDir}/`);

// --- OGP head tags -------------------------------------------------------------------------------
// Every prerendered page gets og:title/og:description (relative-safe); og:url and og:image need an
// absolute origin, so they are emitted only when the export ran with --base-url (site.base_url).
// A note's description comes from its sidecar metadata (`track meta --description`), falling back to
// an excerpt of the body; its image from `track meta --image` (already published under assets/),
// falling back to the site-wide default shipped with the frontend build (ogp-default.png).
// (notesById / baseUrl are declared above the render loop, which calls into these helpers.)

function noteFor(route) {
  const m = route.match(/^\/notes\/(.+)$/);
  return m ? notesById.get(m[1]) : undefined;
}

function pageTitle(route) {
  const note = noteFor(route);
  if (note && note.title && note.title !== site.title) return `${note.title} · ${site.title}`;
  return site.title || "track";
}

// bodyExcerpt flattens the first meaningful lines of a note body into one og:description-sized line:
// code fences and headings drop, links/images/emphasis reduce to their text.
async function bodyExcerpt(noteId) {
  let body;
  try {
    body = (await readData(join("note", noteId))).note?.body ?? "";
  } catch {
    return "";
  }
  const text = body
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/^#+\s.*$/gm, " ")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[\[([^\]]+)\]\]/g, "$1")
    .replace(/[`*_>#|-]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  return text.length > 160 ? `${text.slice(0, 157)}…` : text;
}

async function ogpTags(route) {
  const note = noteFor(route);
  const title = note?.title || site.title || "track";
  const description = (note?.description || (note ? await bodyExcerpt(note.note_id) : "")).replace(/\s+/g, " ");
  const tags = [
    tag("og:site_name", site.title || "track"),
    tag("og:title", title),
    tag("og:type", note ? "article" : "website"),
  ];
  if (description) tags.push(tag("og:description", description));
  if (baseUrl) {
    tags.push(tag("og:url", baseUrl + (route === "/" ? "/" : `${route}/`)));
    const image = note?.image ? `${baseUrl}/${note.image}` : `${baseUrl}/ogp-default.png`;
    tags.push(tag("og:image", image));
    tags.push(`<meta name="twitter:card" content="summary_large_image">`);
  }
  return tags.join("");
}

function tag(property, content) {
  return `<meta property="${property}" content="${escapeHTML(content)}">`;
}

function escapeHTML(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}
