# Explore a completed metrics dashboard

This example opens with three populated charts: a wide cache-hit rate chart for **Checkout** and
**Search**, plus separate percentage-point and relative-percent change charts. It contains 35 days
of synthetic daily readings, including one visible Checkout collection gap. It needs no collector,
credentials or Grafana server.

From the repository root, build the complete CLI (including its frontend), then launch the demo:

```sh
nix build .#track-cli
python3 examples/metrics-dashboard/demo.py --track ./result/bin/track
```

If this branch is already installed, `python3 examples/metrics-dashboard/demo.py` uses `track` from PATH.

Open **http://127.0.0.1:18765**. The script creates a temporary vault and isolated configuration/cache,
shifts sample dates to end today (UTC), and opens the saved dashboard. Ctrl+C stops it and removes
the temporary vault. To use another local port, add `--port 18766`.

| Look at | What you should see |
| --- | --- |
| Level chart, final points | Checkout **84%**, Search **68%** |
| Change chart, final points | Checkout **+2 pp**, Search **−1 pp** |
| Relative change, final points | Checkout **+2.44%**, Search **−1.45%** (rounded here) |
| Period → Last 7 days | Seven recent daily observations per service |
| Period → All observations | 35 days, a visible gap, and the bottom zoom slider |
| Search legend entry | Click to hide that line in the selected chart; click again to restore |
| Hover a point | Its date, service and actual value |
| Missing-observation details | The Checkout collection gap and reason, without inventing zero |
| Refresh | Rereads the same data file; unchanged data produces the same values |

These values are fictional service observations. Calculations are supplied by the fixture; Track
renders them and does not derive domain metrics. A level of 82% → 84% is **+2 percentage points**,
or **+2.44% relative change**; the two panels use different scales.

## Keep the example in an existing vault

Copy `data/observations.jsonl` into that vault's `data/observations.jsonl`, then create the saved note:

```sh
track new --title 'Cache observations' < examples/metrics-dashboard/dashboard.md
```

`dashboard.md` is the ready-to-render note, and `grafana.json` is the same three-panel definition.
The committed data has fixed dates ending 2026-10-29: **All observations** always shows the example,
while trailing periods can be empty as those dates age. The temporary demo shifts dates so its
period controls remain useful. Data-file changes refresh the saved dashboard without regenerating
the note; a writer/skill can update the same canonical JSONL.

To exercise the one-time importer separately:

```sh
track metrics dashboard --dashboard examples/metrics-dashboard/grafana.json \
  --data-dir examples/metrics-dashboard/data --out /tmp/cache-import.md
```

## Track charts and external Grafana are different

The `metrics-dashboard` fence renders Track charts from local JSONL. It supports Period, Refresh,
legend selection and chart zoom in the live workspace. Static site export keeps a populated snapshot
with tooltips/legend/zoom, but has no live Period or Refresh control.

A `grafana` fence instead displays an **existing external Grafana server**, after explicit Load.
It uses that server's dashboards, data and permissions. The help URL ending in `example.test` is a
placeholder to replace with your own server; it is not a working demo. This example creates no
Grafana service, changes no sharing/authentication setting and makes no external connection.
