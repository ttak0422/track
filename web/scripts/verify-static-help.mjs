// Integration proof after `make site`, without a browser or any vault writes:
// node web/scripts/verify-static-help.mjs <SITE_OUT> <SITE_BASE> <SITE_ORIGIN>
// Run for both / and /guide/help/, with SITE_ORIGIN including the deployment path.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { once } from "node:events";
import { readFileSync, readdirSync } from "node:fs";
import { createServer } from "node:http";
import { join, resolve, sep } from "node:path";
import { JSDOM } from "jsdom";

const [siteDir, base, canonical] = process.argv.slice(2);
assert(siteDir && base?.startsWith("/") && base.endsWith("/") && canonical,
  "usage: node web/scripts/verify-static-help.mjs <SITE_OUT> <SITE_BASE> <SITE_ORIGIN>");
const root = resolve(siteDir);
const source = readFileSync(new URL("../../docs/help/apps/counter/index.html", import.meta.url));
assert.deepEqual(readFileSync(join(root, "apps/counter/index.html")), source);

// Mount output at its deployment base, as a static host does. No SPA fallback: the note and app
// URLs must address actual directory indexes. This does not exercise the dev-server middleware.
const server = createServer((req, res) => {
  try {
    const pathname = new URL(req.url, "http://localhost").pathname;
    assert(pathname.startsWith(base));
    const file = resolve(root, decodeURIComponent(pathname.slice(base.length)), pathname.endsWith("/") ? "index.html" : "");
    assert(file.startsWith(root + sep));
    const bytes = readFileSync(file);
    res.setHeader("content-type", "text/html; charset=utf-8");
    res.end(bytes);
  } catch {
    res.writeHead(404).end();
  }
});
server.listen(0, "127.0.0.1");
await once(server, "listening");
const origin = `http://127.0.0.1:${server.address().port}`;
const windows = [];
function parse(html, options) {
  const dom = new JSDOM(html, options);
  windows.push(dom.window);
  return dom.window;
}

try {
  const home = parse(readFileSync(join(root, "index.html"), "utf8")).document;
  assert(home.querySelector(".note-preview")?.textContent.trim(), "home must have a prerendered body");
  assert.equal(home.querySelector('meta[property="og:url"]')?.content, `${canonical.replace(/\/$/, "")}/`);

  let notePath;
  for (const slug of readdirSync(join(root, "notes"))) {
    const html = readFileSync(join(root, "notes", slug, "index.html"), "utf8");
    if (html.includes('<meta property="og:title" content="Static apps">')) {
      notePath = `${base}notes/${slug}/`;
      break;
    }
  }
  assert(notePath, "Static apps note must exist directly under output/notes");
  const noteResponse = await fetch(origin + notePath);
  assert.equal(noteResponse.status, 200);
  const note = parse(await noteResponse.text(), { url: origin + notePath }).document;
  assert.match(note.title, /^Static apps/);
  assert.equal(note.querySelectorAll('meta[property="og:title"]').length, 1);
  assert.equal(note.querySelector('meta[property="og:type"]')?.content, "article");
  assert.equal(note.querySelector('meta[property="og:url"]')?.content,
    `${canonical.replace(/\/$/, "")}/${notePath.slice(base.length)}`);
  const link = [...note.querySelectorAll(".note-preview a")].find((a) => a.textContent === "Open the counter");
  assert(link, "runnable demo must be an SSR anchor, not just text in code or dehydrated state");
  assert.equal(link.getAttribute("href"), `${base}apps/counter/`);

  const appResponse = await fetch(link.href);
  assert.equal(appResponse.status, 200);
  const appBytes = Buffer.from(await appResponse.arrayBuffer());
  assert.deepEqual(appBytes, source, "HTTP target must serve the unchanged app, not a note/SPA fallback");
  const app = parse(appBytes.toString(), { url: link.href, runScripts: "dangerously" });
  const count = () => app.document.querySelector("#count").textContent;
  assert.equal(count(), "0");
  app.document.querySelector("#increment").click();
  app.document.querySelector("#increment").click();
  assert.equal(count(), "2");
  app.document.querySelector("#reset").click();
  assert.equal(count(), "0");
  console.log(JSON.stringify({ base, notePath, href: link.getAttribute("href"), http: 200,
    appSHA256: createHash("sha256").update(appBytes).digest("hex"), counter: "0 → 2 → 0", ogp: "ok" }));
} finally {
  windows.forEach((window) => window.close());
  server.close();
  server.closeAllConnections();
  await once(server, "close");
}
