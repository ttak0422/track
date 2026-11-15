//! Literal typed properties, not Markdown. No I/O or navigation authority.
use crate::markdown::{self, LinkTarget};
use serde_json::Value;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Property {
    pub index: usize,
    pub key: String,
    pub value: String,
    kind: Option<String>,
    line: Option<i64>,
    pub target: Option<LinkTarget>,
}
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Projection {
    pub rows: Vec<Property>,
    pub unavailable: bool,
}
pub fn project(props: &Value) -> Projection {
    let Some(entries) = props.as_array() else {
        return Projection {
            unavailable: true,
            ..Default::default()
        };
    };
    let mut projection = Projection::default();
    for (index, entry) in entries.iter().enumerate() {
        let (Some(key), Some(value)) = (
            entry.get("key").and_then(Value::as_str),
            entry.get("value").and_then(Value::as_str),
        ) else {
            projection.unavailable = true;
            continue;
        };
        let kind = entry.get("type").and_then(Value::as_str);
        let line = entry.get("line").and_then(Value::as_i64);
        projection.unavailable |= kind.is_none() || line.is_none();
        if key == "up" && kind == Some("link") {
            continue;
        }
        let target = (kind == Some("link") && line.is_some())
            .then(|| target(value))
            .flatten();
        projection.rows.push(Property {
            index,
            key: key.into(),
            value: value.into(),
            kind: kind.map(str::to_owned),
            line,
            target,
        });
    }
    projection
}
fn target(value: &str) -> Option<LinkTarget> {
    let (key, fragment) = value
        .trim()
        .split_once('#')
        .map_or((value.trim(), None), |(key, fragment)| {
            (key.trim(), Some(format!("#{fragment}")))
        });
    if key.is_empty() {
        return None;
    }
    let target = LinkTarget::Wiki {
        key: key.into(),
        fragment,
        authored_label: true,
    };
    match markdown::fragment_anchor(&[], &target) {
        Ok(_) | Err(markdown::FragmentError::MissingHeading(_)) => Some(target),
        _ => None,
    }
}
impl Property {
    /// Revalidate the exact original row without cloning the property's array.
    pub fn current(&self, props: &Value) -> bool {
        let Some(entry) = props.as_array().and_then(|rows| rows.get(self.index)) else {
            return false;
        };
        entry.get("key").and_then(Value::as_str) == Some(self.key.as_str())
            && entry.get("value").and_then(Value::as_str) == Some(self.value.as_str())
            && entry.get("type").and_then(Value::as_str) == self.kind.as_deref()
            && entry.get("line").and_then(Value::as_i64) == self.line
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn exact_types_up_order_and_duplicate_values_are_preserved() {
        let p = project(&json!([
            {"key":"up","value":"Parent","type":"link","line":1},
            {"key":"up","value":"literal","type":"string","line":2},
            {"key":"up","value":"unknown","type":"future","line":3},
            {"key":"UP","value":"日本語","type":"link","line":4},
            {"key":"same","value":"B","type":"Link","line":5},
            {"key":"same","value":"B","type":"link","line":6}
        ]));
        assert!(!p.unavailable);
        assert_eq!(
            p.rows.iter().map(|r| r.value.as_str()).collect::<Vec<_>>(),
            ["literal", "unknown", "日本語", "B", "B"]
        );
        assert_eq!(
            p.rows
                .iter()
                .map(|r| r.target.is_some())
                .collect::<Vec<_>>(),
            [false, false, true, false, true]
        );
    }
    #[test]
    fn malformed_props_remain_unavailable_not_invented_targets() {
        for props in [Value::Null, json!({}), json!([null, {"key":"a","value":7}])] {
            let p = project(&props);
            assert!(p.unavailable);
            assert!(p.rows.is_empty());
        }
        let p = project(&json!([{"key":"a","value":"visible","type":"link"}]));
        assert!(p.unavailable);
        assert_eq!(p.rows[0].value, "visible");
        assert!(p.rows[0].target.is_none());
        assert!(!project(&json!([])).unavailable);
    }
    #[test]
    fn heading_uses_existing_flow_unsupported_fragments_stay_inert() {
        let t = target("Other#Detail").unwrap();
        assert_eq!(markdown::resolve_term(&t).as_deref(), Some("Other"));
        assert!(
            markdown::fragment_anchor(&markdown::parse("## Detail"), &t)
                .unwrap()
                .is_some()
        );
        for value in ["#Detail", "Other#^block", "Other#h-detail", "Other#%20"] {
            assert!(target(value).is_none());
        }
        for value in [
            "https://example.com",
            "file:///tmp/file",
            "[[literal]]",
            "A|literal",
        ] {
            let t = target(value).unwrap();
            assert!(matches!(t, LinkTarget::Wiki { .. }));
            assert!(crate::external_links::destination(&t).is_none());
            assert_eq!(markdown::resolve_term(&t).as_deref(), Some(value));
        }
    }
    #[test]
    fn exact_row_revalidation_rejects_replacement_and_bad_shapes() {
        let mut props = json!([{"key":"ref","value":"B","type":"link","line":1}]);
        let row = project(&props).rows.remove(0);
        assert!(row.current(&props));
        props[0]["type"] = json!("string");
        assert!(!row.current(&props));
        assert!(!row.current(&Value::Null));
    }
}
