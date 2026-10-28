# Saved metrics dashboard

Synthetic weekly observations demonstrate levels in percent, absolute changes in percentage points,
and relative changes in percent. They are not actual financial readings or sector credit data.

Copy `data/sectors.jsonl` to your vault's `data/sectors.jsonl`. Create the saved note through the CLI:

```sh
track new --title 'Sector observations' < examples/metrics-dashboard/dashboard.md
```

Open the note in the web workspace or native shell. Change Period, select legend series, and append
observations with the existing writer/skill; Refresh resolves the same definition. A null week breaks
the line. To exercise the historical one-time importer separately:

```sh
track metrics dashboard --dashboard examples/metrics-dashboard/grafana.json \
  --data-dir examples/metrics-dashboard/data --out /tmp/sector-import.md
```

A static site export includes a snapshot of selected observations/metadata. The remote `grafana`
fence is a separate feature: it connects to an existing HTTPS server only after a reader action, and
keeps an external fallback. The fixture does not create or call a Grafana server.
