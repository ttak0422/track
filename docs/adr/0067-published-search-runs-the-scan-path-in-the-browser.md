# 0067: Published full-text search runs the engine's scan path in the browser

## Status

Accepted (2026-08-01)

## Context

ADR 0066 moved the title-then-body search composition into `internal/track/search`
so the CLI and the web server share one search. It left the published static site
title-only, on the reasoning that the site has no server, that `notes.json` carries
no bodies, and that shipping them "would grow the bundle by the whole vault's text
and still could not reproduce bm25".

**That reasoning was wrong, in a way worth writing down.** The published site
already ships every body **twice**: once as rendered HTML in the prerendered
`notes/<slug>/index.html`, and again as `Body` inside `data/note/<slug>.json`.
Text the site already serves cannot be the thing that makes a search too heavy.
The real constraint is narrower and different: bodies must not enter `notes.json`,
which is fetched at first paint. Anything fetched only when someone searches costs
a reader who never searches nothing at all.

The bm25 half was also backwards. bm25 needs term frequencies, document
frequencies, and document lengths — all of which are derivable *from the bodies*.
It is the option that ships bodies that could reproduce ranking; the alternatives
below cannot. And the engine itself does not always have bm25: `bodySearchScan`,
the fallback for queries the trigram index cannot serve, orders by recency.

Three implementations were measured before choosing.

**A prebuilt trigram index (rejected).** The natural "static-site search" shape:
compute the same trigrams SQLite's FTS5 tokenizer produces, ship a posting list,
never ship the text. Tokenisation was validated to reproduce FTS5 trigram exactly.
It does not pay off in Japanese. CJK trigram keys are 7–9 bytes while a term
occurs in ~2 documents on average, so most of the index is keys: on a 112-note
corpus (726 KB of text) the best realistic encoding — front-coded keys, varint
delta postings — came to **398 KiB gzipped against 276 KiB for the entire corpus**.
Vocabulary grows about `n^0.785`, so the ratio improves with scale but does not
invert: still ~1.2x at a modelled 10k notes. An index larger than the text it
indexes is not an optimisation. It also cannot carry positions cheaply, so it
could only *approximate* what matched, and verification would mean fetching the
candidate notes anyway.

**An off-the-shelf client-side search library (rejected).** Measured on a
28-document Japanese corpus: Orama's default tokenizer produces **zero tokens**
for Japanese text; lunr with a Japanese segmenter missed 21% of real terms;
Pagefind indexes with a morphological analyser but queries with `Intl.Segmenter`,
and the mismatch is severe — one term occurring 9 times in a page returned 0
results, another occurring 471 times across 12 pages returned 1 — and its
maintainers state sub-word matching is out of scope. MiniSearch works if handed
`Intl.Segmenter`, but it is word-based BM25: it would answer a different question
than the live server's trigram index, which matches substrings. A published site
that disagrees with the workspace about what matched is the "second, worse search"
ADR 0066 was right to want to avoid.

## Decision

Publish the bodies as their own file and run **the engine's own scan path** over
them in the browser.

- `export-site` writes `data/search.json` — `{"bodies": {"<slug>": "<source
  markdown>"}}` for the published set only. Source Markdown, not the resolved
  body, so a snippet reads as the note is written and an expanded chart's option
  JSON never enters the corpus.
- `web/src/staticSearch.ts` mirrors `bodySearchScan` and the store's title query:
  the same OR/AND grammar, the same hierarchical `#tag` filter and rank vector,
  case-insensitive substring matching (which is what a trigram index *means*), the
  same first-matching-line snippet truncated at 120 bytes on a rune boundary, and
  the same recency order. It is line-for-line a port, and the Go and TypeScript
  sides are differentially tested against each other.
- The client fetches `search.json` on the first search that needs it, never at
  first paint, and caches the promise so concurrent keystrokes share one request.
  A bundle built before the file existed degrades to title-and-tag rather than
  failing.

## Consequences

- The search box means the same thing in all three places — CLI, workspace,
  published site. There is one grammar and one notion of what matched.
- The published site cannot rank by bm25, and orders body hits by recency. This
  is not a published-site compromise: it is exactly what the engine does whenever
  a query misses its index.
- Substring matching is what the site can honestly offer, and it is what the live
  server offers. A two-character CJK query — the case that forces the engine's own
  fallback — works on the published site for the same reason.
- A reader who never types in the search box downloads nothing extra. A reader who
  does downloads the vault's text once, having already downloaded some of it as
  the pages they read.
- The scan is linear in corpus size on each debounced keystroke. Fine at vault
  scale; a site large enough for that to hurt would want the index this ADR
  rejected, and the measurement above says to revisit only if the corpus stops
  being mostly CJK.
- `data/search.json` publishes nothing new. Every body in it is already in that
  site's prerendered HTML and its `note/<slug>.json`; the published set is the
  same set, so a note excluded from publication stays excluded.
