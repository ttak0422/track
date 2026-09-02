# 0071. Static exports publish a sitemap

Status: Accepted

## Context

`track export-site` writes real HTML files for the root, selected notes, graph and empty views, used
tag routes, and—when `--calendar` is enabled—the calendar and day routes. The export also copies
assets and encrypted data files into the same output directory. Walking that directory cannot tell
pages from those non-page resources, and the Go export side is the only place that has both the
published note selection and the resolved `slug:` values.

The build already receives the absolute deployment URL through `--base-url`. The Pages and preview
workflows supply it from `configure-pages`; local builds intentionally leave it empty. A sitemap
cannot contain relative locators, and a build without a deployment URL must not guess one.

## Decision

The Go site exporter writes `sitemap.xml` from the same route inventory used to write crawlable HTML:

- `/`, every selected note at `/notes/<published-slug>/`, `/graph/`, and `/empty/`;
- every used tag and hierarchical tag ancestor at `/tags/<tag>/`;
- `/calendar/` and every activity or dated-task `/day/<date>/` only with `--calendar`.

The sitemap never lists assets, bundle files, `/tasks/`, or routes omitted by the export. Note routes
and the root route carry `lastmod` from the note body's indexed file mtime, formatted as UTC RFC 3339;
generated view routes omit it because no single source mtime is authoritative for them.

`--base-url` is trimmed of trailing slashes and must be an absolute HTTP(S) URL without a query or
fragment. When it is absent, the export omits both `sitemap.xml` and `robots.txt`. When present, the
export writes `robots.txt` with an absolute `Sitemap:` directive. This keeps local or otherwise
unconfigured exports valid while making deployed exports discoverable.

## Consequences

- Sitemap URL generation agrees with pinned note slugs instead of deriving a second note address.
- The sitemap is generated before assets are copied and does not depend on an output-directory walk.
- A deployment that wants crawler discovery must provide the same base URL already required for
  absolute canonical/social metadata; GitHub Pages and PR previews already do so.
