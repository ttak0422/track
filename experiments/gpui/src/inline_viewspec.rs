//! Resource admission only: the owned engine remains the ViewSpec evaluator.
//! Never normalize this value or send its serialization instead of the source.
use serde::de::{self, DeserializeSeed, MapAccess, SeqAccess, Visitor};
use serde_json::{Map, Number, Value};
use std::{cell::Cell, fmt};

const MAX_BYTES: usize = 64 * 1024;
const MAX_DEPTH: usize = 16;
const MAX_NODES: usize = 10_000;
const MAX_OBJECTS: usize = 512;
const UNSUPPORTED: &str = "This ViewSpec is outside the bounded inline metric preview subset";
const FILE_BACKED: &str = "File-backed ViewSpec is not supported by this native preview yet";

struct Budget {
    nodes: Cell<usize>,
    objects: Cell<usize>,
}
struct Bounded<'a> {
    budget: &'a Budget,
    depth: usize,
}
impl Bounded<'_> {
    fn child(&self) -> Bounded<'_> {
        Bounded {
            budget: self.budget,
            depth: self.depth + 1,
        }
    }
}
impl<'de> DeserializeSeed<'de> for Bounded<'_> {
    type Value = Value;
    fn deserialize<D: de::Deserializer<'de>>(self, deserializer: D) -> Result<Value, D::Error> {
        let n = self.budget.nodes.get() + 1;
        if self.depth > MAX_DEPTH || n > MAX_NODES {
            return Err(de::Error::custom("ViewSpec resource limit"));
        }
        self.budget.nodes.set(n);
        deserializer.deserialize_any(self)
    }
}
impl<'de> Visitor<'de> for Bounded<'_> {
    type Value = Value;
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("bounded unambiguous JSON")
    }
    fn visit_bool<E: de::Error>(self, v: bool) -> Result<Value, E> {
        Ok(Value::Bool(v))
    }
    fn visit_unit<E: de::Error>(self) -> Result<Value, E> {
        Ok(Value::Null)
    }
    fn visit_i64<E: de::Error>(self, v: i64) -> Result<Value, E> {
        Ok(Value::Number(v.into()))
    }
    fn visit_u64<E: de::Error>(self, v: u64) -> Result<Value, E> {
        Ok(Value::Number(v.into()))
    }
    fn visit_f64<E: de::Error>(self, v: f64) -> Result<Value, E> {
        Number::from_f64(v)
            .map(Value::Number)
            .ok_or_else(|| E::custom("Non-finite number"))
    }
    fn visit_str<E: de::Error>(self, v: &str) -> Result<Value, E> {
        if v.len() > 4096 {
            return Err(E::custom("ViewSpec string limit"));
        }
        Ok(Value::String(v.to_owned()))
    }
    fn visit_string<E: de::Error>(self, v: String) -> Result<Value, E> {
        if v.len() > 4096 {
            return Err(E::custom("ViewSpec string limit"));
        }
        Ok(Value::String(v))
    }
    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Value, A::Error> {
        let mut out = Vec::new();
        while let Some(value) = seq.next_element_seed(self.child())? {
            if out.len() == 256 {
                return Err(de::Error::custom("ViewSpec array limit"));
            }
            out.push(value);
        }
        Ok(Value::Array(out))
    }
    fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Value, A::Error> {
        let count = self.budget.objects.get() + 1;
        if count > MAX_OBJECTS {
            return Err(de::Error::custom("ViewSpec object limit"));
        }
        self.budget.objects.set(count);
        let mut out = Map::new();
        while let Some(key) = map.next_key::<String>()? {
            if key.len() > 256 || out.len() == 32 || out.contains_key(&key) {
                return Err(de::Error::custom("ViewSpec key limit or duplicate key"));
            }
            out.insert(key, map.next_value_seed(self.child())?);
        }
        Ok(Value::Object(out))
    }
}

fn object(value: &Value) -> Result<&Map<String, Value>, String> {
    value.as_object().ok_or_else(|| UNSUPPORTED.into())
}
fn keys(value: &Map<String, Value>, allowed: &[&str]) -> Result<(), String> {
    if value.keys().all(|key| allowed.contains(&key.as_str())) {
        Ok(())
    } else {
        Err(UNSUPPORTED.into())
    }
}
fn required<'a>(obj: &'a Map<String, Value>, key: &str) -> Result<&'a Value, String> {
    obj.get(key).ok_or_else(|| UNSUPPORTED.into())
}
fn text(value: &Value) -> Result<&str, String> {
    value.as_str().ok_or_else(|| UNSUPPORTED.into())
}
fn nonempty(value: &Value) -> Result<&str, String> {
    let s = text(value)?;
    if s.trim().is_empty() {
        Err(UNSUPPORTED.into())
    } else {
        Ok(s)
    }
}
fn optional_text(obj: &Map<String, Value>, key: &str) -> Result<(), String> {
    if let Some(value) = obj.get(key) {
        text(value)?;
    }
    Ok(())
}
fn channel(value: &Value, quantitative: bool) -> Result<(), String> {
    let obj = object(value)?;
    keys(
        obj,
        if quantitative {
            &["field", "title", "type", "domain"]
        } else {
            &["field", "title", "type"]
        },
    )?;
    if nonempty(required(obj, "field")?)?.len() > 256 {
        return Err(UNSUPPORTED.into());
    }
    optional_text(obj, "title")?;
    if let Some(kind) = obj.get("type") {
        if text(kind)?
            != if quantitative {
                "quantitative"
            } else {
                "nominal"
            }
        {
            return Err(UNSUPPORTED.into());
        }
    }
    if let Some(domain) = obj.get("domain") {
        let domain = domain.as_array().ok_or(UNSUPPORTED)?;
        if domain.len() != 2 {
            return Err(UNSUPPORTED.into());
        }
        let a = domain[0].as_f64().ok_or(UNSUPPORTED)?;
        let b = domain[1].as_f64().ok_or(UNSUPPORTED)?;
        if !a.is_finite() || !b.is_finite() || a >= b {
            return Err(UNSUPPORTED.into());
        }
    }
    Ok(())
}

/// Admit only bounded inline metric data. Errors are fixed text, never source excerpts.
pub fn admit(spec: &str) -> Result<(), String> {
    if spec.len() > MAX_BYTES {
        return Err("ViewSpec exceeds the 64 KiB preview limit".into());
    }
    let budget = Budget {
        nodes: Cell::new(0),
        objects: Cell::new(0),
    };
    let mut de = serde_json::Deserializer::from_str(spec);
    let value = Bounded {
        budget: &budget,
        depth: 1,
    }
    .deserialize(&mut de)
    .map_err(|_| "ViewSpec must be bounded JSON without duplicate keys".to_string())?;
    de.end().map_err(|_| UNSUPPORTED.to_string())?;
    let root = object(&value)?;
    keys(root, &["version", "mark", "title", "data", "encoding"])?;
    if required(root, "version")?.as_f64() != Some(2.0)
        || !matches!(text(required(root, "mark")?)?, "line" | "bar")
    {
        return Err(UNSUPPORTED.into());
    }
    optional_text(root, "title")?;
    let data = object(required(root, "data")?)?;
    if data.contains_key("source") {
        return Err(FILE_BACKED.into());
    }
    keys(data, &["kind", "records"])?;
    if text(required(data, "kind")?)? != "metric" {
        return Err(UNSUPPORTED.into());
    }
    let records = required(data, "records")?.as_array().ok_or(UNSUPPORTED)?;
    if records.is_empty() || records.len() > 256 {
        return Err(UNSUPPORTED.into());
    }
    for record in records {
        let record = object(record)?;
        if record.values().any(|v| v.is_array() || v.is_object())
            || required(record, "version")?.as_f64() != Some(1.0)
        {
            return Err(UNSUPPORTED.into());
        }
        nonempty(required(record, "name")?)?;
        nonempty(required(record, "time")?)?;
        optional_text(record, "entity")?;
        optional_text(record, "missing_reason")?;
        let missing = record
            .get("missing_reason")
            .and_then(Value::as_str)
            .is_some_and(|s| !s.trim().is_empty());
        let v = required(record, "value")?;
        if !(v.is_number() || v.is_null()) || v.is_null() != missing {
            return Err(UNSUPPORTED.into());
        }
    }
    let encoding = object(required(root, "encoding")?)?;
    keys(encoding, &["x", "y"])?;
    channel(required(encoding, "x")?, false)?;
    let y = required(encoding, "y")?.as_array().ok_or(UNSUPPORTED)?;
    if y.is_empty() || y.len() > 4 {
        return Err(UNSUPPORTED.into());
    }
    for series in y {
        channel(series, true)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn fixture() -> Value {
        json!({"version":2,"mark":"line","data":{"kind":"metric","records":[
            {"version":1,"name":"score","time":"A","value":0},
            {"version":1,"name":"score","time":"B","value":null,"missing_reason":"gap"},
            {"version":1,"name":"score","time":"C","value":4}
        ]},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}})
    }
    fn passes(v: &Value) -> bool {
        admit(&v.to_string()).is_ok()
    }
    #[test]
    fn accepts_zero_gap_and_boundaries_without_rewriting() {
        let mut v = fixture();
        assert!(passes(&v));
        v["mark"] = json!("bar");
        v["data"]["records"] = json!(vec![v["data"]["records"][0].clone(); 256]);
        v["encoding"]["y"] = json!(vec![
            json!({"field":"value","domain":[-1,4],"type":"quantitative"});
            4
        ]);
        assert!(passes(&v));
        v["data"]["records"].as_array_mut().unwrap().push(json!({}));
        assert!(!passes(&v));
    }
    #[test]
    fn rejects_ambiguous_json_everywhere() {
        for s in [
            r#"{"version":2,"version":2}"#,
            r#"{"data":{"source":"","source":null}}"#,
            r#"{"a":[{"x":1,"\u0078":2}]}"#,
        ] {
            assert!(admit(s).is_err());
        }
        assert!(admit(&("[".repeat(17) + "0" + &"]".repeat(17))).is_err());
        assert!(admit(&" ".repeat(MAX_BYTES + 1)).is_err());
        assert!(admit("1e999").is_err());
        assert!(admit(&(fixture().to_string() + " null")).is_err());
    }
    #[test]
    fn rejects_all_binding_and_transform_expansions() {
        for key in ["filter", "sort", "limit", "overlays", "source", "future"] {
            let mut v = fixture();
            v[key] = json!([]);
            assert!(!passes(&v));
        }
        for source in [json!(""), json!("../escape"), json!(null), json!([])] {
            let mut v = fixture();
            v["data"]["source"] = source;
            assert_eq!(admit(&v.to_string()).unwrap_err(), FILE_BACKED);
        }
        for key in ["Source", "sources", "url", "path"] {
            let mut v = fixture();
            v["data"][key] = json!("");
            assert!(!passes(&v));
        }
        for key in ["window", "axis", "mark", "href"] {
            let mut v = fixture();
            v["encoding"]["y"][0][key] = json!({});
            assert!(!passes(&v));
        }
        let mut v = fixture();
        v["data"]["records"] = json!([]);
        assert!(!passes(&v));
    }
    #[test]
    fn enforces_scalar_records_and_missing_values() {
        for invalid in [json!("4"), json!(true), json!([]), json!({})] {
            let mut v = fixture();
            v["data"]["records"][0]["value"] = invalid;
            assert!(!passes(&v));
        }
        let mut v = fixture();
        v["data"]["records"][0]["source"] = json!("/inert/column");
        assert!(passes(&v));
        v["data"]["records"][0]["source"] = json!({"path":"/x"});
        assert!(!passes(&v));
        let mut v = fixture();
        v["data"]["records"][1]["missing_reason"] = json!(" ");
        assert!(!passes(&v));
        let mut v = fixture();
        v["data"]["records"][0]["missing_reason"] = json!("gap");
        assert!(!passes(&v));
    }
    #[test]
    fn enforces_channel_limits_and_strings() {
        let mut v = fixture();
        v["encoding"]["y"] = json!(vec![json!({"field":"value"}); 5]);
        assert!(!passes(&v));
        for domain in [json!([2, 1]), json!([1, 1]), json!([0]), json!(["0", 1])] {
            let mut v = fixture();
            v["encoding"]["y"][0]["domain"] = domain;
            assert!(!passes(&v));
        }
        let mut v = fixture();
        v["title"] = json!("x".repeat(4097));
        assert!(!passes(&v));
        let mut v = fixture();
        v["encoding"]["x"]["field"] = json!("x".repeat(257));
        assert!(!passes(&v));
        let mut v = fixture();
        v["data"]["records"][0]["x".repeat(257)] = json!(0);
        assert!(!passes(&v));
        let mut v = fixture();
        for n in 0..29 {
            v["data"]["records"][0][format!("k{n}")] = json!(0);
        }
        assert!(!passes(&v));
    }
}
