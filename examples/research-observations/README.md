# Research observations

A fully synthetic, offline-input example of separate valuation, news-derived attention, and business evidence series. Nothing here is market data, an investment signal, or an estimate of true value. Units remain separate; there is no combined score.

From the repository root:

```sh
nix develop --command go run ./cmd/track render --spec examples/research-observations/article.json --out /tmp/research-observations.html
nix develop --command go run ./cmd/track render --spec examples/research-observations/attention.viewspec.json --renderer svg --out /tmp/research-attention.svg
```

The HTML article loads the existing pinned renderer scripts from CDNs; the SVG is self-contained. The chart has two finite runs separated by the October 3 outage. October 5 is an observed zero, so it remains a real plotted value. The article's evidence table shows every missing reason, including observations with no visible chart mark. All `example.com` links and source IDs are placeholders.

`available_at`, `unit`, `claim_type`, and `source_id` are producer-owned extra columns displayed through existing channels. They are not a new core provenance schema, revision store, freshness calculation, or as-of query. Fetching, classification, calculations, and historical revision storage remain external to Track.
