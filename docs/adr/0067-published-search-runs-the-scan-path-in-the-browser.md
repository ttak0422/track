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

- `export-site` writes `data/search.json` — one entry per published note, its
  slug and **the published body, byte for byte what `note/<slug>.json` already
  carries**. Not the source body: every published surface replaces original asset
  file names and internal note ids with opaque slugs, and a corpus built from the
  source would be the one file in the built site that put them back. Entries are
  written in the order a body search returns hits, so the client keeps the file's
  order instead of shipping mtimes to sort by.
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

- The search box has one grammar in all three places — CLI, workspace, published
  site — and one notion of what matched.
- **Order is where they differ, and it is not a corner case.** For a query the
  index can serve — three runes or more, which is most of them — the live server
  ranks body hits by bm25 and the published site cannot, so the same notes come
  back in a different order. Recency is what the engine falls back to, and it is
  the honest ceiling here: bm25 needs an index, and this ADR chose not to ship
  one. Match set and snippets agree; ranking does not.
- Two smaller divergences are known and left alone, both from the live side
  disagreeing with itself. `%` and `_` in a query are unescaped SQL `LIKE`
  wildcards on the server's *title* path but literal everywhere else (including
  the server's own body path, which quotes each term) — the fix belongs in
  `titleMatchClause`, not here. And case folding differs three ways already
  (SQLite's ASCII-only `LIKE`, Go's `strings.ToLower`, FTS5's Unicode folding),
  so no client can agree with all of them. The port folds like the scan it
  mirrors, Go's `strings.ToLower` — which JavaScript's `toLowerCase` is not: it
  applies Unicode SpecialCasing, turning a word-final `Σ` into `ς` and expanding
  `İ` to `i` + U+0307. The port pre-maps those two characters before
  lowercasing, and the fixture both sides read keeps it there.
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
- `data/search.json` publishes nothing new, and a test pins it: each corpus body
  is compared against the body already in `note/<slug>.json`. The published set is
  the same set, so a note excluded from publication stays excluded.
