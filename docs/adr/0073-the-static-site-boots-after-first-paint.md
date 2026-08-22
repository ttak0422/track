# 0073. The static site boots after first paint

Status: Accepted

## Context

Lighthouse's lab metrics (and the real phones they model) measure the window around the first
paint: every request a page starts before that paint lands in the render-time dependency graph,
whatever it is for. A plain `<script type="module">` entry starts its whole static import graph —
React, the router, the markdown stack, several hundred kilobytes — during head parse, tens of
milliseconds before the first pixel. On the exported help site none of that JS changes what the
first paint draws (the prerendered markup already carries title, body, and rail), yet all of it was
counted: the simulated first contentful paint sat near 3 s on pages with zero interactivity cost,
holding the Performance score in the mid-50s. The font stylesheet made it worse from a second
direction — requested from `<head>`, it pulled a third-party round trip into the same window while
changing no pixel of the paint either.

## Decision

The client entry splits in two. `main.tsx` keeps only what must run early and cheaply: the
`/index.html` URL normalization, the stale-deploy guards (`vite:preloadError`, `reloadOnce`), the
render-blocking stylesheet import, and a scheduler. Everything else — React, router, queries,
markdown, editors — moved behind `import("./boot")`, which fires after two animation frames: one
crossed frame means a paint happened, so the app's downloads start past the measured window instead
of inside it. The Google Fonts stylesheet joins the same kick; `<noscript>` keeps a synchronous
fallback for no-JS readers, and the preconnects stay in `<head>` so the injection reuses warm
connections.

Two constraints fell out of the split, both load-bearing:

- **The stylesheet import stays in the loader.** Vite emits CSS as render-blocking links for the
  entry chunk only. Letting `styles.css` ride with the boot chunk painted the prerendered markup
  bare and reflowed it wholesale when the app CSS landed — CLS 1.4, worse than the problem being
  solved.
- **The stale-deploy guards stay in the loader.** A page from an old deploy fails at its own chunk
  names; the reload-once handler has to exist before the deferred import can reject.

## Consequences

First paint is the prerendered HTML plus one small script and one stylesheet (~15 kB transferred),
so FCP and Speed Index drop to roughly the HTML+CSS floor; interactivity arrives about one frame
later than before, which nothing in the UI can observe. The live server (`track web`) shares the
loader: its blank `#root` waits one extra frame, likewise unobservable.

What remains expensive is the client's full re-render over the discarded prerendered DOM (ADR 0059
documents why hydration mismatches): LCP still waits on the boot chunks because React paints fresh
text nodes when it mounts. Shrinking that means hydrating rather than replacing, which is its own
decision.
