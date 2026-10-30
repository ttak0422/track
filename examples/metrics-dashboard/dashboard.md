A completed dashboard using **35 days of fictional cache-hit readings** for Checkout and Search.
It opens with all observations; no collector or Grafana server is needed.

```metrics-dashboard
{
  "title": "Cache observations \u00b7 synthetic",
  "panels": [
    {
      "type": "timeseries",
      "title": "Cache hit rate (%)",
      "gridPos": {
        "x": 0,
        "y": 0,
        "w": 24
      },
      "datasource": {
        "type": "track",
        "uid": "observations.jsonl"
      },
      "targets": [
        {
          "expr": "demo_level",
          "legendFormat": "{{entity}}"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "pct"
        }
      }
    },
    {
      "type": "timeseries",
      "title": "Daily change (pp)",
      "gridPos": {
        "x": 0,
        "y": 1,
        "w": 12
      },
      "datasource": {
        "type": "track",
        "uid": "observations.jsonl"
      },
      "targets": [
        {
          "expr": "demo_change_pp",
          "legendFormat": "{{entity}}"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "percentage_point"
        }
      }
    },
    {
      "type": "timeseries",
      "title": "Relative change (%)",
      "gridPos": {
        "x": 12,
        "y": 1,
        "w": 12
      },
      "datasource": {
        "type": "track",
        "uid": "observations.jsonl"
      },
      "targets": [
        {
          "expr": "demo_change_pct",
          "legendFormat": "{{entity}}"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "pct"
        }
      }
    }
  ]
}
```

## Read and explore the example

At the latest sample, Checkout is **84%** (+2 percentage points, +2.44% relative to 82%) and
Search is **68%** (−1 percentage point, −1.45% relative to 69%). Hover the final points to inspect
the actual plotted values. These are synthetic demonstration values, not service measurements.

The temporary demo shifts dates to today (UTC). Try **Period → Last 7 days** to focus the lines,
then **All observations** to restore the full history. The committed data instead ends 2026-10-29;
recent periods can be empty when those fixed dates have aged.
Click **Search** in a chart legend to hide that line; click again to restore it. The wide first panel
shows levels; the two smaller panels keep percentage-point and relative-percent changes separate.
The Checkout collection gap breaks the line instead of becoming zero. Expand its missing-observation
details to see the reason. **Refresh** rereads the saved data; without a data edit it preserves the same values.
