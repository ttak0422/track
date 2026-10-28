# Reusable metrics dashboards

The `metrics-dashboard` fence persists a Grafana classic subset definition in note content. This
complements ADR 0076's one-time importer without adding a second chart renderer or finance formulas.
The existing note editor/store owns save, reopen, titles and bindings. `data/` stays the observation
store. An AI or skill updates data through its established writer; it does not regenerate the UI.

`POST /api/metrics-dashboard?vault=<name>` accepts `{dashboard: <JSON object>, days: <integer>}`.
`days: 0` selects all observations; 1..36500 selects a trailing observation-time interval measured
from the request time. It is read-only. The endpoint validates the subset through the existing
importer and returns title/panels, each with its resolved ECharts option, unit, grid column/width,
source filename, latest observation, data modification time, selected point/missing counts, and
latest per-series provenance. An absent/none panel unit uses the single declared record unit, without numeric conversion. Query functions and SQL are not executed. Requests are limited to
1 MiB, and dashboards to 48 panels. Bare filenames are opened with os.Root so escaping symlinks fail.

The canonical metric contract remains version/name/entity/time/value; null requires missing_reason.
All optional producer metadata, including nested provenance and source versions, is retained in expandable per-series metadata. Point links accept url or href. The producer may add unit, frequency, publication_frequency, scope, source, url, asof, available_at,
fetched_at and revision. The renderer preserves these in metadata/tooltips and never derives finance
metrics, resamples a publication or turns an absent week into an observed zero. Dates/RFC3339 times
are parsed and normalized for chronological display. Latest observation is independent of the chosen
period; an empty period explicitly reports no observations while retaining that freshness reference.

Date-only observations use UTC midnight as a reference for ordering and the rolling-period cutoff;
this is not an intraday measurement. When every selected observation is date-only, axis labels stay
date-only. Mixed timestamps are ordered as UTC instants. Original supplied timestamps, precision and
timezone metadata remain available in series metadata and point details.

Panel targets must select one data file. Every declared target binding is checked before matching observations, including targets with no current matches; a missing target is never filled from another source. The live renderer accepts multiple folded metric names/labels, and rejects mixed record units. The legacy CLI importer still requires a single folded metric name. Different levels/differences/relative
changes get separate names and panels, with explicit percent versus percentage-point units. Scope
metadata distinguishes market observations from actual sector observations. A collector's fetch rate
is not the observation/publication rate. Unknown financial coverage remains the producer's concern.

React Query keys include vault, definition and selected period. Data events cancel in-flight dashboard and View Spec requests before invalidating their queries; stream open/reconnect does the same. This replaces an initial response captured before the event, rather than letting it consume the refresh and leave stale data indefinitely.
EChartsBlock owns chart instances, legend selection, resize observers, provenance clicks and unmount
disposal. `gridPos.x/w` use 24 columns; y orders panels, while fixed y/h geometry and drag editing are
not supported. Narrow surfaces stack panels. Period/legend selections are transient view state.

The site exporter resolves the same definition into a `metrics-snapshot` fence with ready-to-draw
options and metadata. Published snapshots contain the selected data, require no API or exposed JSONL,
and update only when rebuilt. Generated note references pass through the existing publication
rewriter. Malformed definitions show an inline error and the source. External Grafana frames remain
stopped until a reader explicitly loads them, including on published pages.

## Actual Grafana boundary

A `grafana` fence contains `{title,url}` for an external HTTPS classic dashboard or panel URL. It is
separate from the local subset renderer. The frontend checks scheme, path, exact app origin,
credentials, fragments, loopback hosts and query keys before offering a one-mount reader admission
for that exact URL/origin. No request/iframe exists before the click. Changing the definition or
navigating away removes admission; disconnect removes the iframe and reconnect replaces it. Admission is keyed by the document/vault identity tree and fence text, so cached note switches and included-note changes cannot carry a live frame into another document.

The frame sandbox admits scripts and same-origin for Grafana's own API/storage, but neither top
navigation, popups, forms, downloads nor a native bridge. Same-origin and loopback starting URLs are
rejected. This is an explicit trust boundary to an existing server, not a claim to constrain every
subresource or redirect controlled by an admitted server. No Track proxy, service account, credential
injection, anonymous mode or sharing setting is added. Native URL policy and main-frame-only speech
bridge checks remain unchanged. Grafana X-Frame-Options/frame-ancestors, server permissions and
cookie restrictions can still prevent use. Cross-origin load events cannot prove readiness; a visible
external fallback is always offered. Authenticated Grafana and manual WKWebView QA require a known
server and are not inferred from mocked iframe tests.

Official references: [sharing](https://grafana.com/docs/grafana/latest/visualizations/dashboards/share-dashboards-panels/),
[framing configuration](https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/#allow_embedding).
