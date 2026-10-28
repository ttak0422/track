# Sector observations

Synthetic weekly observations. Each panel preserves its own unit.

```metrics-dashboard
{
  "title": "Sector observations (synthetic)",
  "panels": [
    {
      "type": "timeseries",
      "title": "Level (synthetic)",
      "gridPos": {
        "x": 0,
        "y": 0,
        "w": 12
      },
      "datasource": {
        "type": "track",
        "uid": "sectors.jsonl"
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
      "title": "Change in percentage points",
      "gridPos": {
        "x": 12,
        "y": 0,
        "w": 12
      },
      "datasource": {
        "type": "track",
        "uid": "sectors.jsonl"
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
        "x": 0,
        "y": 1,
        "w": 24
      },
      "datasource": {
        "type": "track",
        "uid": "sectors.jsonl"
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
