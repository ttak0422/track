// The page's first script, and deliberately almost the only thing it loads before first paint.
//
// The prerendered markup is the whole first paint: title, body, rail. Everything the client adds —
// React, the router, the markdown stack — is needed to make that markup interactive, not to make it
// visible, so this loader waits out one frame and only then imports ./app and the font stylesheet.
// Starting those downloads during the paint instead (the way a plain <script type="module"> does)
// puts several hundred kilobytes of JS plus a third-party font CSS on the network before any pixel
// lands, which is exactly the window render-time metrics measure: the page would sit in their
// dependency graph even though none of it affects what is drawn. One frame later, the reader has
// already seen the note; the downloads then cost nothing visible.
//
// The stale-deploy guards live here rather than in ./app because they are what a stale deploy fails
// before: if this deploy's chunks are gone, the import below rejects, and vite:preloadError must
// already have its handler to reload onto the new one.
import { reloadOnce, setStalePageHandler, STATIC_MODE } from "./runtime";

// The one thing this loader does load eagerly: the stylesheet. It is render-blocking on purpose —
// without it the prerendered markup paints bare and reflows wholesale when the app CSS lands with
// the boot chunk below, scoring as a viewport-wide layout shift.
import "./styles.css";

// Each prerendered route is a directory index (/notes/<id>/index.html). A host — or Lighthouse, or a
// direct link — may serve it at the explicit .../index.html URL; the router only knows the directory
// route, so normalize the address to the directory before the app initializes (otherwise the client
// would replace the correct prerendered content with a not-found).
if (STATIC_MODE && window.location.pathname.endsWith("/index.html")) {
  const dir = window.location.pathname.slice(0, -"index.html".length);
  window.history.replaceState(window.history.state, "", dir + window.location.search + window.location.hash);
}

window.addEventListener("vite:preloadError", (event) => {
  if (reloadOnce()) event.preventDefault();
});

setStalePageHandler(reloadOnce);

// The font stylesheet joins the app past the paint for the same reason: requested from the head it
// would drag a third-party round trip into the metrics' critical path while changing no pixel of the
// first paint (the fallback stack draws the text; display=swap trades up when the faces land). The
// noscript block in index.html keeps the no-JS story synchronous.
const FONT_STYLESHEET = "https://fonts.googleapis.com/css2?family=IBM+Plex+Sans+JP:wght@400;500;600;700&display=swap";

function loadFontStylesheet(): void {
  const link = document.createElement("link");
  link.rel = "stylesheet";
  link.href = FONT_STYLESHEET;
  document.head.appendChild(link);
}

export function afterFirstPaint(boot: () => void): void {
  let started = false;
  const kick = () => {
    if (started) return;
    started = true;
    boot();
  };
  // A hidden tab fires no animation frames, so it cannot wait for them.
  if (document.hidden) {
    setTimeout(kick, 0);
    return;
  }
  // Two frames: the first rAF callback still runs ahead of its own frame's paint, so one crossing is
  // what guarantees a paint happened. The timer is the belt to rAF's braces — a throttled or hidden
  // mid-flight tab must still reach the app eventually.
  requestAnimationFrame(() => requestAnimationFrame(kick));
  setTimeout(kick, 300);
}

afterFirstPaint(() => {
  loadFontStylesheet();
  void import("./boot");
});
