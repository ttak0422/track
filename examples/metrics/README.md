# Generic metrics dashboard

This fixture contains **synthetic** CPU, memory and latency samples for two services, spanning
2026-09-18 through 2026-09-19 (UTC). It does not contact or monitor a real service.
The same schema accepts business, personal, or finance metrics produced by an external writer.

With a build containing the metrics dashboard reader, copy `services.jsonl` into your vault's
`data/`, then run (replace `main` with your vault name):

```sh
track --vault main metrics dashboard --dashboard examples/metrics/dashboard.json --out /tmp/services.md
track --vault main new --title "Service metrics sample" < /tmp/services.md
```

Open that note in the live Web workspace or Native app. It contains one dashboard with three
numeric panels, two time-series panels, and a latest-value table. Select `web-b`: its final CPU
value is 85%, memory is 71%, and latency is 275 ms. The threshold labels show 70%, 70%, and 200 ms.
Select the first date to see all panels use that period's latest values instead. A future period
shows **No data**, never zero. A metric selector applies one named series across the dashboard;
panels for other metrics become empty.

The JSON's `gridPos` controls placement on 24 columns. Readers stack panels on narrow screens.
Refresh rereads local JSONL; replacing the data file triggers live invalidation. It does not fetch
external data or recalculate indicators. `asof` and each value's timestamp refer to the samples,
not the refresh time. The stat and table panels show the latest point **per series** within the
selected period, not a sum across services. Threshold bands are presentation, independent of the
`metrics alert` evaluator and notification/capture workflow.

For finance, derive `metric` JSONL with track-finance and replace the datasource filename and
metric queries. There are no ticker, exchange, trading-calendar or RSI rules in this dashboard.

Static exports preserve the fence as source with a live-workspace notice. Standalone `track render`
continues to accept viewspec/article files, not Grafana dashboard JSON.
