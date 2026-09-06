# Reading dashboard prototype

A prototype "reading dashboard" built by composing three existing track features, with no new engine
surface:

- **Typed note properties** (ADR 0032) — a reading note carries `author`, `genre`, `pages`, `status`,
  `rating`, and `finished` as props (inline `key:: value` fields in the body, typed by the config's
  `properties:` schema).
- **The note query language** (ADR 0033) — `TABLE …` fences over those props give count-based views:
  books read this year, the unread backlog (積読), and titles by author.
- **The View Spec** (ADR 0021 / 0024 / 0030) — a `treemap` (pages by genre) and a `bar` (pages by
  author) chart drawn over the same books expressed as canonical `metric` JSONL records.

The dashboard is the combination, not a new engine feature. It answers each of the four questions a
reading log asks, using the smallest tool that fits:

| Question                        | Surface              | How |
| ------------------------------- | -------------------- | --- |
| Books read this year (count)    | `track-query` fence  | `TABLE … WHERE props.status = done AND 2025-12-31 < props.finished`; the result's `count` is the number read. |
| Pages (total, by genre)         | `viewspec` treemap   | `treemap` leaves = books, area = `pages`, grouped by `genre`; group bands sum the pages. |
| Unread backlog (積読)            | `track-query` fence  | `TABLE … WHERE props.status = unread`. |
| By author                       | `track-query` + `bar`| `TABLE … SORT props.author` lists titles; the `bar` chart ranks authors by pages. |

## Files

- `notes/` — sample reading notes. Each book is one note whose body carries its props as inline
  fields (`author::`, `genre::`, `pages::`, `status::`, `rating::`, `finished::`). Those are indexed
  by ADR 0032 and read by `props.<key>` in queries.
- `config.yml.snippet` — the `properties:` schema (so `pages`/`rating` are typed `number`, `status`
  is checked against an enum, `finished` is a `date`) and `queries:` saved queries the fences below
  reuse. Merge into `<vault>/.track/config.yml`.
- `data/books.jsonl` — the same books as canonical `metric` records (one per book); `value` = pages,
  extra fields `author`/`genre`/`status` ride along. This is the "books as data" source the charts
  draw from. track never fetches data; a future `track-fetch-*` book importer (or manual curation)
  would produce this file.
- `data/genre-pages.jsonl` — `books.jsonl` pre-aggregated: one `metric` record per genre, `value` =
  total pages. A `bar` ranking (ジャンル別ページ数) draws directly from it.
- `dashboard.md` — the prototype landing note. It embeds the `track-query` fences and `viewspec`
  fences that produce the four views, so the whole dashboard renders in the live web workspace and
  the static export from one note.
- `genre-treemap.viewspec.json`, `genre-bar.viewspec.json`, `author-bar.viewspec.json` — standalone
  spec files with inline `data.records`, so they render with `track render` and no data file.

## Try it

```sh
# Charts, straight from the spec files (no vault needed):
track render --spec examples/reading/genre-treemap.viewspec.json --out /tmp/genre.html
track render --spec examples/reading/author-bar.viewspec.json    --out /tmp/author.html

# The query views, against a vault seeded with notes/ and config.yml.snippet:
track query 'TABLE title, props.pages, props.rating FROM #book WHERE props.status = done AND 2025-12-31 < props.finished AND props.finished < 2027-01-01 SORT props.finished DESC'
track query 'TABLE title, props.author, props.pages FROM #book WHERE props.status = unread'
```

The `dashboard.md` note uses `saved:` fence bodies, so the named queries below resolve when the note
renders; `track-query` fences expand to Markdown tables and `viewspec` fences expand to charts in both
deployments.
