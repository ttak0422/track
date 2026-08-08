# 0069: The published data bundle is locked

## Status

Accepted (2026-08-09)

## Context

`track export-site` publishes a vault as a static site: prerendered pages a reader
(and a crawler) is meant to read, plus a data bundle under `<out>/data` the app runs
on — the note list, the link graph, the `up` forest, the dated tasks, the full-text
corpus, and one file per note.

The pages are the published work. The bundle is something else: it is the vault's
structure, in machine shape, at one URL each. `graph.json` was the whole link graph
as a plain fetch; `search.json` was every published body in one file. A site that
publishes prose for people to read was also, without saying so, publishing a dataset
for anyone to take in a single `wget -r`.

There is no way to stop that with access control — a static site on GitHub Pages has
no server to check anything, and the reader's browser must be able to read the data
by definition. What can change is whether taking the dataset is an ordinary fetch or
a deliberate act. Terms attach to the second one and not to the first: a mechanism
that has to be worked around is a mechanism someone chose to work around.

## Decision

**Every file in the published data bundle is locked, and reading it is a defined
conversion the site's own key opens.**

Each file is gzipped, encrypted with AES-256-GCM, and written as `<name>.bin`:

```
nonce (12 bytes) || AES-256-GCM(gzip(json))
```

The key is 32 bytes derived from the site's public identity —
`sha256("track-site-lock\0" + base-url + "\0" + root-note-title)` — so a rebuild of
the same site produces the same key and two sites do not share one. It travels in
the page as `window.__trackLock`, because the reader's browser has to open the data.

One mechanism, three holders of the key: `internal/track/site/lock.go` locks the
bundle, `web/src/lock.ts` opens it in the browser (the only place the frontend reads
published data), and `web/scripts/prerender.mjs` — which holds both ends, since it
reads the bundle to render pages and locks the dehydrated react-query cache it
inlines into each page. That last part matters: an unlocked copy of the state in the
HTML would hand out exactly what the bundle keeps locked, one page at a time.

**The lock is not confidentiality, and nothing here pretends otherwise.** The key
ships with the site. Anyone who wants the data can have it. What they cannot do is
have it *by accident*, or claim the bytes were lying in the open: getting at them
takes reading the key out of the page and running a specific conversion over the
files. That is the whole point — a door, with a key hanging next to it, is still a
door.

**Generated chart data is part of the bundle.** A `.viewspec.json` asset is resolved
at build time into an ECharts option — the series, point by point, in machine shape —
and published for the embed to fetch. That is display data by any reading, so it is
locked too, as `<slug>.echarts.bin`. The reference in the body still says
`.echarts.json`, because the extension is how the embed knows it is a chart; the
fetch swaps it, exactly as the bundle does. Media the author attached (images, PDFs,
diagram sources) is published as media: a locked `<img src>` is not a thing, and an
attachment is the page's content rather than a dataset behind it.

**What stays readable.** The prerendered pages keep their prose in the markup. They
are what the site publishes, what search engines index, and what makes the first
paint fast; locking them would be locking the front door of a shop. The bundle is the
warehouse behind it.

## Consequences

- **A secure context is required.** `crypto.subtle` is unavailable on `file://` and
  on plain HTTP to a non-localhost host, so the published site needs HTTPS (GitHub
  Pages, any host) or localhost (`make site-serve`). Path routing already ruled out
  `file://`; serving the built directory over plain HTTP on a LAN address now breaks
  it too.
- **Transfer size is unchanged.** The bundle was served gzipped by the host and is
  now gzipped by the exporter; encrypted bytes do not compress, so the wire cost is
  the same ±28 bytes per file. The inlined page state got *smaller*: it is gzipped
  now, where before it was raw JSON in the HTML.
- **The published files no longer end in `.json`.** They hold ciphertext, so they are
  `.bin`. Callers on both sides still name them by what they hold (`notes.json`) and
  the extension is swapped at the boundary — one line in `writeJSONFile`, one in
  `staticData`.
- **Hydration became async.** Opening the inlined state is a promise, so the client
  awaits it alongside the router before its first render (`web/src/main.tsx`).
- **Anything reading the bundle must hold the key**, including tests. That is a
  feature: the test that reads `notes.json` performs the same conversion a reader
  does, so the format cannot drift on one side without failing on the other. The
  cross-language fixture in `web/src/lock.test.ts` — a blob Go locked, opened by the
  TypeScript side — is what pins the two implementations to one mechanism.

## Alternatives considered

- **Leave the bundle as JSON.** Honest and simple, and what shipped until now. It
  makes the dataset a free-standing publication with nothing to say about its reuse.
- **Obfuscate without a key** (base64, a fixed byte transform, a private container
  format). Same effort to undo, but nothing to point at afterwards: there is no key,
  so there is no lock, only an inconvenience.
- **Stop publishing the bundle and render everything server-side at build time.** The
  site would lose its graph, its search, and its client navigation — the reading
  experience is the reason the static export runs the real frontend at all.
- **Per-build random keys.** Rejected for deterministic output: the same vault
  published twice should produce the same bytes. The nonce is random per file, which
  is where randomness is actually needed.
