# Reading dashboard

```dashboard
title: Reading
```

## Read this year

```track-query :layout table
saved: reading-this-year
```

The `count` of the table above is the number of books finished in 2026; each row's `pages` is that
book's length. There is no SUM in the query language by design (ADR 0033), so a numeric total comes
from a chart instead — the treemap and bar below.

## Pages by genre

```viewspec
{ "version": 2, "mark": "treemap", "title": "Pages by genre",
  "data": { "kind": "metric", "source": "genre-pages.jsonl" },
  "encoding": {
    "x": { "field": "name", "type": "nominal", "title": "Genre" },
    "size": { "field": "value", "title": "Pages" },
    "color": { "field": "value", "title": "Pages" } } }
```

## Unread backlog (積読)

```track-query :layout table
saved: reading-unread
```

## By author

```track-query :layout table
saved: reading-author
```
