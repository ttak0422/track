# 0070: Published data lives at a content-addressed path

## Status

Accepted (2026-08-09)

## Context

GitHub Pages serves every file with `Cache-Control: max-age=600`. That is fine for
the JavaScript, because Vite gives each chunk a content-hashed name: a deploy writes
new names, so a browser holding the old ones is holding files that are still correct,
and a page never mixes two builds' code.

The data bundle had no such thing. `data/notes.json` was one mutable path, cached for
ten minutes like everything else, which means:

- **A reader does not see an update for up to ten minutes.** Not the CDN's doing —
  GitHub purges that on deploy — but the reader's own browser, which will not even
  revalidate inside the `max-age` window, ETag or no ETag.
- **A page can mix generations.** The HTML and each data file expire independently, so
  a freshly fetched page can read data from before the deploy, or a cached page can
  read data from after it. Before the bundle was locked this was merely stale;
  afterwards, a key that ever changed would make it fatal (ADR 0069).

The fix the codebase already trusts for the same problem is a path that changes with
the content. The bundle should have one too.

## Decision

**The bundle is published under a fingerprint of its own contents: `data/<generation>/…`,
with the generation baked into every page as `window.__trackData`.**

- `bundleWriter` (internal/track/site/bundle.go) stages the files, hashes what it
  wrote, and renames the staging directory to `data/<generation>` once the last file
  is in. The frontend's `dataURL` prefixes the generation it was given; empty (live
  server, Vite dev server) keeps the old flat path.
- The fingerprint is over the **plaintext** each file holds, not the bytes on disk.
  The lock uses a fresh nonce per file, so identical data encrypts differently every
  build; hashing the ciphertext would move the path on every deploy and throw away
  every reader's cache for nothing. Hashing the data means the path moves when — and
  only when — the data does.
- A page therefore names the data of its own deploy. A ten-minute-old page reads the
  bundle it was built against; a current page reads the current one, immediately, with
  no cache to wait out.

**No carry-forward.** The previous generation is not kept. The workflow already
downloads the previous deploy's `assets/` so a stale page can still load its chunks
(a blank page otherwise), but the data bundle does not need that: a missing generation
is a 404, the app reads that as "this page outlived its deploy", and the client
reloads once — the same recovery, and the same loop guard, that `vite:preloadError`
already uses. Keeping a generation would mean downloading the whole vault's bundle on
every deploy to protect a ten-minute window that self-heals.

## Consequences

- **Updates are immediate.** A reader who loads a page after a deploy gets that
  deploy's data, not whatever their browser cached ten minutes ago.
- **An unchanged vault republishes to the same path**, so a deploy that only touches
  the frontend leaves every reader's data cache valid. Pinned by
  `TestDataGenerationTracksContent`.
- **A stale page recovers by reloading**, losing whatever transient UI state it had —
  acceptable, and already the behaviour when a chunk goes missing.
- **The generation is a build-time fact**, so anything reading the bundle from outside
  the app has to find the directory (the prerender takes it from the page; the tests
  resolve it in one helper). One more placeholder in `index.html`, filled by the export
  and by the dev server the same way the site key is.
- **Assets follow the same rule.** A published attachment was already renamed to an
  opaque slug — the source file name never leaves the vault — but that slug came from
  its *path*, so replacing a file republished it at the same URL and readers kept the
  old one for the cache window. The slug now comes from the file's contents
  (`publishAssetName`), which keeps the name opaque, makes an edit land at a new URL,
  and lets identical files share one. A file that cannot be read falls back to the
  path-derived slug so the reference stays deterministic and the copy still reports it
  missing. Pinned by `TestAssetNameTracksContent`.
- **The CI asset carry-forward now does what it was written for.** It keeps the
  previous deploy's `assets/` alive for the cache window; with content-addressed names
  those files are the ones stale pages actually reference, instead of names that a
  newer deploy has since overwritten with different bytes.
