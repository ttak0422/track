//! Bounded, IO-free categorical chart decoding and plot-local geometry.
use serde_json::{Map, Value};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Line,
    Bar,
}
#[derive(Clone, Debug, PartialEq)]
pub struct Series {
    pub name: String,
    pub color: u32,
    pub values: Vec<Option<f64>>,
    pub show_symbol: bool,
}
#[derive(Clone, Debug, PartialEq)]
pub struct Chart {
    pub title: Option<String>,
    pub x_name: Option<String>,
    pub y_name: Option<String>,
    pub categories: Vec<String>,
    pub series: Vec<Series>,
    pub kind: Kind,
    pub domain: (f64, f64),
    pub boundary_gap: bool,
}
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Point {
    pub x: f64,
    pub y: f64,
}
#[derive(Clone, Debug, PartialEq)]
pub struct Line {
    pub series: usize,
    pub segments: Vec<Vec<Point>>,
    pub symbols: Vec<Point>,
}
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Bar {
    pub series: usize,
    pub category: usize,
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}
#[derive(Clone, Debug, PartialEq)]
pub struct Geometry {
    pub width: f64,
    pub height: f64,
    pub category_x: Vec<f64>,
    pub zero_y: Option<f64>,
    pub lines: Vec<Line>,
    pub bars: Vec<Bar>,
}
type Result<T> = std::result::Result<T, String>;
fn bad<T>(message: &str) -> Result<T> {
    Err(message.into())
}
fn object(v: &Value) -> Result<&Map<String, Value>> {
    v.as_object().ok_or_else(|| "Expected an object".into())
}
fn keys(m: &Map<String, Value>, allowed: &[&str]) -> Result<()> {
    if let Some(k) = m.keys().find(|k| !allowed.contains(&k.as_str())) {
        return Err(format!("Unsupported chart field: {k}"));
    }
    Ok(())
}
fn string(v: &Value) -> Result<&str> {
    v.as_str().ok_or_else(|| "Expected a literal string".into())
}
fn name(m: &Map<String, Value>, key: &str) -> Result<Option<String>> {
    m.get(key).map(|v| string(v).map(str::to_owned)).transpose()
}
fn boolean(m: &Map<String, Value>, key: &str, default: bool) -> Result<bool> {
    m.get(key)
        .map(|v| v.as_bool().ok_or_else(|| format!("Expected boolean {key}")))
        .unwrap_or(Ok(default))
}
fn number(v: &Value) -> Result<f64> {
    v.as_f64()
        .filter(|n| n.is_finite())
        .ok_or_else(|| "Expected a finite number".into())
}
fn scan(v: &Value, depth: usize) -> Result<()> {
    if depth > 32 {
        return bad("Chart nesting exceeds 32 levels");
    }
    match v {
        Value::String(s) if s.len() > 4096 => return bad("Chart string exceeds 4096 bytes"),
        Value::Array(a) => {
            for v in a {
                scan(v, depth + 1)?;
            }
        }
        Value::Object(m) => {
            for (k, v) in m {
                if k.len() > 4096 {
                    return bad("Chart key exceeds 4096 bytes");
                }
                scan(v, depth + 1)?;
            }
        }
        _ => (),
    }
    Ok(())
}
fn layout(v: &Value) -> Result<()> {
    if v.is_number() {
        number(v)?;
        return Ok(());
    }
    let s = string(v)?;
    if ["left", "center", "right", "top", "middle", "bottom"].contains(&s) {
        return Ok(());
    }
    if s.strip_suffix('%')
        .and_then(|s| s.parse::<f64>().ok())
        .is_some_and(|v| v.is_finite() && (0.0..=100.0).contains(&v))
    {
        return Ok(());
    }
    bad("Unsupported fixed layout value")
}
fn padding(v: &Value) -> Result<f64> {
    if v.as_f64() == Some(0.0) {
        return Ok(0.0);
    }
    let n = string(v)?
        .strip_suffix('%')
        .and_then(|s| s.parse::<f64>().ok())
        .filter(|n| n.is_finite() && (0.0..=100.0).contains(n))
        .ok_or("Unsupported axis padding")?;
    Ok(n / 100.0)
}
fn cosmetics(root: &Map<String, Value>) -> Result<()> {
    if let Some(v) = root.get("grid") {
        let m = object(v)?;
        keys(m, &["left", "right", "bottom", "containLabel"])?;
        for k in ["left", "right", "bottom"] {
            if let Some(v) = m.get(k) {
                layout(v)?;
            }
        }
        boolean(m, "containLabel", false)?;
    }
    if let Some(v) = root.get("tooltip") {
        let m = object(v)?;
        keys(m, &["trigger", "axisPointer"])?;
        if m.get("trigger").is_some_and(|v| v.as_str() != Some("axis")) {
            return bad("Unsupported tooltip trigger");
        }
        if let Some(v) = m.get("axisPointer") {
            let m = object(v)?;
            keys(m, &["type"])?;
            if !matches!(
                m.get("type").and_then(Value::as_str),
                Some("cross" | "shadow")
            ) {
                return bad("Unsupported axis pointer");
            }
        }
    }
    if let Some(v) = root.get("dataZoom") {
        let a = v
            .as_array()
            .filter(|a| a.len() == 1)
            .ok_or("Unsupported zoom configuration")?;
        let m = object(&a[0])?;
        keys(m, &["type", "xAxisIndex", "zoomOnMouseWheel"])?;
        if m.get("type").and_then(Value::as_str) != Some("inside")
            || m.get("xAxisIndex").and_then(Value::as_u64) != Some(0)
            || m.get("zoomOnMouseWheel").and_then(Value::as_str) != Some("shift")
        {
            return bad("Unsupported zoom configuration");
        }
    }
    // An empty legend introduces no hidden state. Other legend models are not supported.
    if let Some(v) = root.get("legend") {
        keys(object(v)?, &[])?;
    }
    Ok(())
}

pub fn decode(source: &str) -> Result<Chart> {
    if source.len() > 256 * 1024 {
        return bad("Chart exceeds 256 KiB");
    }
    let value: Value = serde_json::from_str(source).map_err(|_| "Invalid chart JSON")?;
    scan(&value, 0)?;
    let root = object(&value)?;
    keys(
        root,
        &[
            "title",
            "xAxis",
            "yAxis",
            "series",
            "color",
            "grid",
            "tooltip",
            "dataZoom",
            "legend",
            "_trackTheme",
        ],
    )?;
    // Go's embedded-option marker requests reader presentation defaults. It is
    // not data or executable behavior; native styling remains native. Accept
    // only the exact engine marker, never arbitrary extension fields.
    if root
        .get("_trackTheme")
        .is_some_and(|value| value != &Value::Bool(true))
    {
        return bad("Unsupported Track theme marker");
    }
    cosmetics(root)?;
    let title = if let Some(v) = root.get("title") {
        let m = object(v)?;
        keys(m, &["text", "left"])?;
        if let Some(v) = m.get("left") {
            layout(v)?;
        }
        name(m, "text")?
    } else {
        None
    };
    let x = object(root.get("xAxis").ok_or("Missing category axis")?)?;
    keys(x, &["type", "data", "name", "boundaryGap"])?;
    if x.get("type").and_then(Value::as_str) != Some("category") {
        return bad("Only category x axes are supported");
    }
    let categories = x
        .get("data")
        .and_then(Value::as_array)
        .filter(|a| !a.is_empty() && a.len() <= 256)
        .ok_or("Expected 1–256 categories")?
        .iter()
        .map(|v| string(v).map(str::to_owned))
        .collect::<Result<Vec<_>>>()?;
    let y_value = root.get("yAxis").ok_or("Missing value axis")?;
    let y_value = if let Some(a) = y_value.as_array() {
        if a.len() != 1 {
            return bad("Only one value axis is supported");
        }
        &a[0]
    } else {
        y_value
    };
    let y = object(y_value)?;
    keys(y, &["type", "name", "scale", "min", "max", "boundaryGap"])?;
    if y.get("type").and_then(Value::as_str) != Some("value") {
        return bad("Only value y axes are supported");
    }
    let scale = boolean(y, "scale", false)?;
    let palette = if let Some(v) = root.get("color") {
        let a = v
            .as_array()
            .filter(|a| !a.is_empty() && a.len() <= 16)
            .ok_or("Expected 1–16 colors")?;
        a.iter()
            .map(|v| {
                let s = string(v)?;
                if s.len() != 7
                    || !s.starts_with('#')
                    || !s[1..].bytes().all(|c| c.is_ascii_hexdigit())
                {
                    return bad("Colors must be #RRGGBB");
                }
                u32::from_str_radix(&s[1..], 16).map_err(|_| "Invalid color".into())
            })
            .collect::<Result<Vec<_>>>()?
    } else {
        vec![0x75a7ff, 0xf0af68, 0x6bc6a0, 0xd28be0]
    };
    let raw_series = root
        .get("series")
        .and_then(Value::as_array)
        .filter(|a| !a.is_empty() && a.len() <= 4)
        .ok_or("Expected 1–4 series")?;
    let mut series = Vec::new();
    let mut kind = None;
    let mut low = f64::INFINITY;
    let mut high = f64::NEG_INFINITY;
    for (i, v) in raw_series.iter().enumerate() {
        let m = object(v)?;
        let k = match m.get("type").and_then(Value::as_str) {
            Some("line") => Kind::Line,
            Some("bar") => Kind::Bar,
            _ => return bad("Unsupported series type"),
        };
        if kind.is_some_and(|old| old != k) {
            return bad("Mixed series types are unsupported");
        }
        kind = Some(k);
        keys(
            m,
            if k == Kind::Line {
                &[
                    "type",
                    "name",
                    "data",
                    "xAxisIndex",
                    "yAxisIndex",
                    "showSymbol",
                    "connectNulls",
                ]
            } else {
                &["type", "name", "data", "xAxisIndex", "yAxisIndex"]
            },
        )?;
        for key in ["xAxisIndex", "yAxisIndex"] {
            if m.get(key).is_some_and(|v| v.as_u64() != Some(0)) {
                return bad("Only axis index zero is supported");
            }
        }
        if boolean(m, "connectNulls", false)? {
            return bad("Connecting null observations is unsupported");
        }
        let a = m
            .get("data")
            .and_then(Value::as_array)
            .filter(|a| a.len() == categories.len())
            .ok_or("Series length must match categories")?;
        let values = a
            .iter()
            .map(|v| {
                if v.is_null() {
                    Ok(None)
                } else {
                    let n = number(v)?;
                    low = low.min(n);
                    high = high.max(n);
                    Ok(Some(n))
                }
            })
            .collect::<Result<Vec<_>>>()?;
        series.push(Series {
            name: name(m, "name")?.unwrap_or_else(|| format!("Series {}", i + 1)),
            color: palette[i % palette.len()],
            values,
            show_symbol: boolean(m, "showSymbol", true)?,
        });
    }
    if !low.is_finite() {
        return bad("No finite observations");
    }
    let kind = kind.unwrap();
    let observed = if kind == Kind::Bar {
        (low.min(0.0), high.max(0.0))
    } else {
        (low, high)
    };
    if !scale || kind == Kind::Bar {
        low = low.min(0.0);
        high = high.max(0.0);
    }
    let min = y.get("min").map(number).transpose()?;
    let max = y.get("max").map(number).transpose()?;
    if min.is_some_and(|n| n > observed.0) || max.is_some_and(|n| n < observed.1) {
        return bad("Explicit domain clips observations or baseline");
    }
    low = min.unwrap_or(low);
    high = max.unwrap_or(high);
    if low > high || (min.is_some() && max.is_some() && low == high) {
        return bad("Invalid explicit domain");
    }
    if low == high {
        let delta = (low.abs() * 0.05).max(1.0);
        if min.is_none() {
            low -= delta;
        }
        if max.is_none() {
            high += delta;
        }
    }
    let span = high - low;
    if !span.is_finite() || span <= 0.0 {
        return bad("Unrepresentable numeric domain");
    }
    if let Some(v) = y.get("boundaryGap") {
        let a = v
            .as_array()
            .filter(|a| a.len() == 2)
            .ok_or("Expected two axis padding values")?;
        let lower = padding(&a[0])?;
        let upper = padding(&a[1])?;
        if min.is_none() {
            low -= span * lower;
        }
        if max.is_none() {
            high += span * upper;
        }
    }
    if !low.is_finite() || !high.is_finite() || !(high - low).is_finite() || high <= low {
        return bad("Unrepresentable padded domain");
    }
    let boundary_gap = boolean(x, "boundaryGap", kind == Kind::Bar)?;
    if kind == Kind::Bar && !boundary_gap {
        return bad("Bar charts require category boundaryGap=true");
    }
    Ok(Chart {
        title,
        x_name: name(x, "name")?,
        y_name: name(y, "name")?,
        categories,
        series,
        kind,
        domain: (low, high),
        boundary_gap,
    })
}

pub fn geometry(chart: &Chart, width: f64, height: f64) -> Result<Geometry> {
    let (low, high) = chart.domain;
    let span = high - low;
    if !width.is_finite()
        || !height.is_finite()
        || width < 32.0
        || height < 32.0
        || width > 1_000_000.0
        || height > 1_000_000.0
        || !low.is_finite()
        || !high.is_finite()
        || !span.is_finite()
        || span <= 0.0
        || chart.categories.is_empty()
        || chart.categories.len() > 256
        || chart.series.is_empty()
        || chart.series.len() > 4
        || (chart.kind == Kind::Bar && !chart.boundary_gap)
    {
        return bad("Chart geometry unavailable");
    }
    let n = chart.categories.len();
    let category_x: Vec<f64> = (0..n)
        .map(|i| {
            if chart.boundary_gap {
                (i as f64 + 0.5) / n as f64 * width
            } else if n == 1 {
                width / 2.0
            } else {
                i as f64 / (n - 1) as f64 * width
            }
        })
        .collect();
    let position = |value: f64| -> Result<f64> {
        if !value.is_finite() || value < low || value > high {
            return bad("Observation outside numeric domain");
        }
        let y = (1.0 - (value - low) / span) * height;
        if !y.is_finite() {
            return bad("Nonfinite chart position");
        }
        Ok(y.clamp(0.0, height))
    };
    let zero_y = if low <= 0.0 && high >= 0.0 {
        Some(position(0.0)?)
    } else {
        None
    };
    let mut lines = Vec::new();
    let mut bars = Vec::new();
    for (s, series) in chart.series.iter().enumerate() {
        if series.values.len() != n {
            return bad("Mismatched chart geometry");
        }
        let mut segments = Vec::new();
        let mut segment = Vec::new();
        let mut symbols = Vec::new();
        for (i, value) in series.values.iter().enumerate() {
            let Some(value) = value else {
                if !segment.is_empty() {
                    segments.push(std::mem::take(&mut segment));
                }
                continue;
            };
            let point = Point {
                x: category_x[i],
                y: position(*value)?,
            };
            if chart.kind == Kind::Line {
                segment.push(point);
                if series.show_symbol {
                    symbols.push(point);
                }
            } else {
                let baseline = zero_y.ok_or("Bar domain must include zero")?;
                let left = if i == 0 {
                    0.0
                } else {
                    (category_x[i - 1] + point.x) / 2.0
                };
                let right = if i + 1 == n {
                    width
                } else {
                    (point.x + category_x[i + 1]) / 2.0
                };
                let group_width = (right - left) * 0.8;
                let slot = group_width / chart.series.len() as f64;
                bars.push(Bar {
                    series: s,
                    category: i,
                    x: left + (right - left) * 0.1 + s as f64 * slot + slot * 0.05,
                    y: point.y.min(baseline),
                    width: slot * 0.9,
                    height: (point.y - baseline).abs(),
                });
            }
        }
        if !segment.is_empty() {
            segments.push(segment);
        }
        if chart.kind == Kind::Line {
            lines.push(Line {
                series: s,
                segments,
                symbols,
            });
        }
    }
    Ok(Geometry {
        width,
        height,
        category_x,
        zero_y,
        lines,
        bars,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn option() -> Value {
        json!({"xAxis":{"type":"category","data":["A","A","C"]},"yAxis":{"type":"value"},"series":[{"type":"line","data":[0,null,4]}]})
    }
    fn chart(v: Value) -> Chart {
        decode(&v.to_string()).unwrap()
    }
    #[test]
    fn public_engine_fixture_and_literal_labels() {
        let v = json!({"title":{"text":"<script>literal</script>","left":"center"},"xAxis":{"type":"category","boundaryGap":false,"data":["A","B","C"]},"yAxis":[{"type":"value","name":"Score","scale":true,"boundaryGap":["6%","12%"]}],"series":[{"name":"Score","type":"line","showSymbol":false,"connectNulls":false,"data":[0,null,4]}],"color":["#123AbC"],"grid":{"left":"3%","right":"4%","bottom":40,"containLabel":true},"tooltip":{"trigger":"axis","axisPointer":{"type":"cross"}},"dataZoom":[{"type":"inside","xAxisIndex":0,"zoomOnMouseWheel":"shift"}]});
        let c = chart(v);
        assert_eq!(c.title.as_deref(), Some("<script>literal</script>"));
        assert_eq!(c.series[0].color, 0x123abc);
        assert_eq!(c.domain, (-0.24, 4.48));
        assert!(!c.series[0].show_symbol);
    }
    #[test]
    fn go_embedded_theme_marker_preserves_numeric_chart_semantics() {
        // EChartsOptionJSON's categorical line option, including its embedded
        // reader marker and the ordinary grid/tooltip/zoom presentation fields.
        let mut value = json!({
            "_trackTheme":true,
            "title":{"text":"Fixture metric","left":16},
            "color":["#123abc"],
            "grid":{"left":16,"right":16,"bottom":24,"containLabel":true},
            "tooltip":{"trigger":"axis","axisPointer":{"type":"cross"}},
            "dataZoom":[{"type":"inside","xAxisIndex":0,"zoomOnMouseWheel":"shift"}],
            "xAxis":{"type":"category","data":["A","B","C"],"boundaryGap":false},
            "yAxis":[{"type":"value","scale":true,"boundaryGap":["6%","12%"]}],
            "series":[{"name":"score","type":"line","data":[0,null,4],"showSymbol":false,"connectNulls":false}]
        });
        let with_marker = chart(value.clone());
        assert_eq!(with_marker.categories, ["A", "B", "C"]);
        assert_eq!(with_marker.series[0].values, [Some(0.0), None, Some(4.0)]);
        assert_eq!(with_marker.series[0].color, 0x123abc);
        value.as_object_mut().unwrap().remove("_trackTheme");
        assert_eq!(chart(value.clone()), with_marker);
        for marker in [
            json!(false),
            Value::Null,
            json!(1),
            json!("true"),
            json!({}),
            json!([]),
        ] {
            value["_trackTheme"] = marker;
            assert!(decode(&value.to_string()).is_err());
        }
        value["_trackTheme"] = true.into();
        value["_unknownExtension"] = true.into();
        assert!(decode(&value.to_string()).is_err());
        value.as_object_mut().unwrap().remove("_unknownExtension");
        value["series"][0]["connectNulls"] = true.into();
        assert!(decode(&value.to_string()).is_err());
    }
    #[test]
    fn category_positions_and_null_segments() {
        let c = chart(option());
        assert_eq!(c.categories, vec!["A", "A", "C"]);
        let g = geometry(&c, 200.0, 100.0).unwrap();
        assert_eq!(g.category_x, vec![0.0, 100.0, 200.0]);
        assert_eq!(
            g.lines[0].segments.iter().map(Vec::len).collect::<Vec<_>>(),
            vec![1, 1]
        );
        assert_eq!(g.lines[0].symbols.len(), 2);
        assert_eq!(g.lines[0].segments[0][0].y, 100.0);
        let mut c = c;
        c.boundary_gap = true;
        assert_eq!(
            geometry(&c, 300.0, 100.0).unwrap().category_x,
            vec![50.0, 150.0, 250.0]
        );
        c.categories = vec!["one".into()];
        c.series[0].values = vec![Some(0.0)];
        for gap in [false, true] {
            c.boundary_gap = gap;
            assert_eq!(geometry(&c, 200.0, 100.0).unwrap().category_x, vec![100.0]);
        }
    }
    #[test]
    fn grouped_bars_and_signed_baseline() {
        let mut v = option();
        v["series"] = json!([{"type":"bar","data":[-2,0,4]},{"type":"bar","data":[2,null,3]}]);
        let c = chart(v);
        assert_eq!(c.domain, (-2.0, 4.0));
        assert!(c.boundary_gap);
        let g = geometry(&c, 300.0, 180.0).unwrap();
        assert_eq!(g.bars.len(), 5);
        let z = g.zero_y.unwrap();
        assert!((z - 120.0).abs() < 1e-10);
        assert!((g.bars[0].y - z).abs() < 1e-10);
        assert_eq!(g.bars[1].height, 0.0);
        assert!(g.bars[0].x + g.bars[0].width < g.bars[3].x);
        for b in &g.bars {
            assert!(b.x >= 0.0 && b.x + b.width <= 300.0);
            assert!(b.y >= 0.0 && b.y + b.height <= 180.0);
            assert_eq!(b.width, g.bars[0].width);
        }
        let mut rejected = c.clone();
        rejected.boundary_gap = false;
        assert!(geometry(&rejected, 300.0, 180.0).is_err());
        let mut unsupported = option();
        unsupported["series"] = json!([{"type":"bar","data":[1,1,1]}]);
        unsupported["xAxis"]["boundaryGap"] = json!(false);
        assert!(
            decode(&unsupported.to_string())
                .unwrap_err()
                .contains("boundaryGap=true")
        );
    }
    #[test]
    fn domains_constant_scaled_explicit_and_padding() {
        let mut v = option();
        v["series"][0]["data"] = json!([4, 4, 4]);
        v["yAxis"]["scale"] = json!(true);
        assert_eq!(chart(v.clone()).domain, (3.0, 5.0));
        v["series"][0]["data"] = json!([0, 0, 0]);
        assert_eq!(chart(v.clone()).domain, (-1.0, 1.0));
        v["series"][0]["data"] = json!([2, 3, 4]);
        assert_eq!(
            geometry(&chart(v.clone()), 100.0, 100.0).unwrap().zero_y,
            None
        );
        v["yAxis"]["min"] = json!(1);
        v["yAxis"]["max"] = json!(5);
        v["yAxis"]["boundaryGap"] = json!(["100%", "100%"]);
        assert_eq!(chart(v.clone()).domain, (1.0, 5.0));
        v["yAxis"]["min"] = json!(3);
        assert!(decode(&v.to_string()).is_err());
        v["yAxis"]["min"] = json!(1);
        v["series"][0]["type"] = json!("bar");
        assert!(decode(&v.to_string()).is_err());
    }
    #[test]
    fn fail_closed_semantics() {
        for (key, value) in [
            ("stack", json!("total")),
            ("smooth", json!(true)),
            ("markPoint", json!({})),
            ("connectNulls", json!(true)),
            ("data", json!(["0", null, 4])),
            ("data", json!([[0], null, 4])),
            ("xAxisIndex", json!(1)),
        ] {
            let mut v = option();
            v["series"][0][key] = value;
            assert!(decode(&v.to_string()).is_err(), "{key}");
        }
        for (key, value) in [
            ("dataset", json!({})),
            ("yAxis", json!([{"type":"value"},{"type":"value"}])),
            ("legend", json!({"selected":{"hidden":false}})),
            (
                "dataZoom",
                json!([{"type":"inside","xAxisIndex":0,"zoomOnMouseWheel":"shift","start":10}]),
            ),
            ("color", json!(["url(https://example.com)"])),
        ] {
            let mut v = option();
            v[key] = value;
            assert!(decode(&v.to_string()).is_err(), "{key}");
        }
        let mut v = option();
        v["series"]
            .as_array_mut()
            .unwrap()
            .push(json!({"type":"bar","data":[1,2,3]}));
        assert!(decode(&v.to_string()).is_err());
    }
    #[test]
    fn limits_nulls_and_malformed_inputs() {
        for s in ["", "null", "[]", "{", "{\"x\":NaN}"] {
            assert!(decode(s).is_err());
        }
        let mut v = option();
        v["series"][0]["data"] = json!([null, null, null]);
        assert_eq!(
            decode(&v.to_string()).unwrap_err(),
            "No finite observations"
        );
        let mut v = option();
        v["title"] = json!({"text":"x".repeat(4097)});
        assert!(decode(&v.to_string()).is_err());
        assert!(decode(&" ".repeat(256 * 1024 + 1)).is_err());
        let mut v = option();
        v["xAxis"]["data"] = json!(vec!["x"; 257]);
        assert!(decode(&v.to_string()).is_err());
        let mut v = option();
        v["series"] = json!(vec![json!({"type":"line","data":[1,2,3]}); 5]);
        assert!(decode(&v.to_string()).is_err());
        let mut nested = json!(0);
        for _ in 0..34 {
            nested = json!([nested]);
        }
        assert!(decode(&nested.to_string()).is_err());
    }
    #[test]
    fn finite_extremes_and_resize_fail_closed() {
        let mut v = option();
        v["series"][0]["data"] = json!([-1e308, 0, 1e308]);
        assert!(decode(&v.to_string()).is_err());
        v["series"][0]["data"] = json!([0, 5e-324, 1e-323]);
        let c = chart(v);
        let g = geometry(&c, 400.0, 300.0).unwrap();
        assert!(
            g.lines[0]
                .symbols
                .iter()
                .all(|p| p.x.is_finite() && p.y.is_finite())
        );
        for (w, h) in [
            (f64::NAN, 100.0),
            (100.0, f64::INFINITY),
            (0.0, 100.0),
            (31.0, 100.0),
        ] {
            assert!(geometry(&c, w, h).is_err());
        }
        let g2 = geometry(&c, 800.0, 600.0).unwrap();
        assert_eq!(g2.lines[0].symbols[1].x, g.lines[0].symbols[1].x * 2.0);
        let mut c = c;
        c.series[0].values[0] = Some(f64::INFINITY);
        assert!(geometry(&c, 100.0, 100.0).is_err());
    }
    #[test]
    fn explicit_line_bounds_can_override_automatic_zero() {
        let mut v = option();
        v["series"][0]["data"] = json!([2, 3, 4]);
        v["yAxis"]["min"] = json!(1);
        assert_eq!(chart(v).domain, (1.0, 4.0));
    }
    #[test]
    fn palette_order_and_source_unchanged() {
        let source = "{\"xAxis\":{\"type\":\"category\",\"data\":[\"Z\"]},\"yAxis\":{\"type\":\"value\"},\"color\":[\"#112233\"],\"series\":[{\"type\":\"line\",\"name\":\"first\",\"data\":[1]},{\"type\":\"line\",\"name\":\"second\",\"data\":[2]}]}";
        let copy = source.to_owned();
        let c = decode(source).unwrap();
        assert_eq!(source, copy);
        assert_eq!(c.series[0].color, c.series[1].color);
        assert_eq!(c.series[1].name, "second");
    }
}
