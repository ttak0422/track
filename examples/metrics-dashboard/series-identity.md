# Default series identities

Synthetic data with automatic line/point cues.

```echarts
{
  "title": {
    "text": "Six series: default cues",
    "left": 16
  },
  "legend": {
    "data": [
      "Series 1",
      "Series 2",
      "Series 3",
      "Series 4",
      "Series 5",
      "Series 6"
    ],
    "top": 28,
    "left": 16
  },
  "grid": {
    "top": 72,
    "left": 40,
    "right": 24,
    "bottom": 32
  },
  "xAxis": {
    "type": "category",
    "data": [
      "1",
      "2",
      "3",
      "4",
      "5",
      "6",
      "7",
      "8"
    ]
  },
  "yAxis": {
    "type": "value"
  },
  "series": [
    {
      "type": "line",
      "name": "Series 1",
      "data": [
        1,
        3,
        2,
        4,
        3,
        5,
        4,
        6
      ]
    },
    {
      "type": "line",
      "name": "Series 2",
      "data": [
        2,
        4,
        3,
        5,
        4,
        6,
        5,
        7
      ]
    },
    {
      "type": "line",
      "name": "Series 3",
      "data": [
        3,
        5,
        4,
        6,
        5,
        7,
        6,
        8
      ]
    },
    {
      "type": "line",
      "name": "Series 4",
      "data": [
        4,
        6,
        5,
        7,
        6,
        8,
        7,
        9
      ]
    },
    {
      "type": "line",
      "name": "Series 5",
      "data": [
        5,
        7,
        6,
        8,
        7,
        9,
        8,
        10
      ]
    },
    {
      "type": "line",
      "name": "Series 6",
      "data": [
        6,
        8,
        7,
        9,
        8,
        10,
        9,
        11
      ]
    }
  ]
}
```

# Equal-color verification

All six colors are deliberately identical; distinguish the patterns, markers and text key.

```echarts
{
  "title": {
    "text": "Six series: same gray",
    "left": 16
  },
  "legend": {
    "data": [
      "Series 1",
      "Series 2",
      "Series 3",
      "Series 4",
      "Series 5",
      "Series 6"
    ],
    "top": 28,
    "left": 16
  },
  "grid": {
    "top": 72,
    "left": 40,
    "right": 24,
    "bottom": 32
  },
  "xAxis": {
    "type": "category",
    "data": [
      "1",
      "2",
      "3",
      "4",
      "5",
      "6",
      "7",
      "8"
    ]
  },
  "yAxis": {
    "type": "value"
  },
  "series": [
    {
      "type": "line",
      "name": "Series 1",
      "data": [
        1,
        3,
        2,
        4,
        3,
        5,
        4,
        6
      ]
    },
    {
      "type": "line",
      "name": "Series 2",
      "data": [
        2,
        4,
        3,
        5,
        4,
        6,
        5,
        7
      ]
    },
    {
      "type": "line",
      "name": "Series 3",
      "data": [
        3,
        5,
        4,
        6,
        5,
        7,
        6,
        8
      ]
    },
    {
      "type": "line",
      "name": "Series 4",
      "data": [
        4,
        6,
        5,
        7,
        6,
        8,
        7,
        9
      ]
    },
    {
      "type": "line",
      "name": "Series 5",
      "data": [
        5,
        7,
        6,
        8,
        7,
        9,
        8,
        10
      ]
    },
    {
      "type": "line",
      "name": "Series 6",
      "data": [
        6,
        8,
        7,
        9,
        8,
        10,
        9,
        11
      ]
    }
  ],
  "color": [
    "#777777",
    "#777777",
    "#777777",
    "#777777",
    "#777777",
    "#777777"
  ]
}
```
