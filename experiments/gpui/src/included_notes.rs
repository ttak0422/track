//! Bounded engine-selected excerpts, never parsed as documents or navigation targets.
use crate::live::Error;
use serde_json::Value;

#[derive(Clone, Debug)]
pub(crate) struct Include {
    pub line: u64,
    pub source_line: i64,
    pub note_id: Option<i64>,
    pub kind: Option<String>,
    pub etag: Option<String>,
    pub title: Option<String>,
    pub caption: String,
    pub excerpt: String,
    pub bad_options: Vec<String>,
    pub error: Option<String>,
    pub vault: String,
    pub explicit_origin: bool,
}
/// One accepted render response; never installed into the source reader.
#[derive(Clone, Debug)]
pub(crate) struct Response {
    pub markdown: String,
    pub entries: Vec<Include>,
}
impl std::ops::Deref for Response {
    type Target = [Include];
    fn deref(&self) -> &Self::Target {
        &self.entries
    }
}
impl<'a> IntoIterator for &'a Response {
    type Item = &'a Include;
    type IntoIter = std::slice::Iter<'a, Include>;
    fn into_iter(self) -> Self::IntoIter {
        self.entries.iter()
    }
}
fn invalid() -> Error {
    Error::Shape("Invalid included-note response or exceeded limits (32 entries, 256 KiB excerpts, 16 KiB metadata, 1 KiB origin/revision, 32 diagnostics).".into())
}
fn text(v: &Value, limit: usize) -> Result<&str, Error> {
    v.as_str().filter(|s| s.len() <= limit).ok_or_else(invalid)
}
fn optional(v: &Value, name: &str, limit: usize) -> Result<Option<String>, Error> {
    v.get(name)
        .map(|v| text(v, limit).map(str::to_owned))
        .transpose()
}
pub(crate) fn decode(value: Value, host_vault: &str) -> Result<Response, Error> {
    let markdown = text(value.get("markdown").ok_or_else(invalid)?, 16 * 1024 * 1024)?;
    let extent = markdown.split('\n').count() as u64;
    let entries = value
        .get("includes")
        .and_then(Value::as_array)
        .filter(|a| a.len() <= 32)
        .ok_or_else(invalid)?;
    let mut seen = std::collections::HashSet::new();
    let mut total = 0usize;
    let mut result = Vec::new();
    for v in entries {
        let line = v
            .get("line")
            .and_then(Value::as_u64)
            .filter(|l| *l < extent && seen.insert(*l))
            .ok_or_else(invalid)?;
        let source_line = v
            .get("source_line")
            .and_then(Value::as_i64)
            .filter(|n| *n >= -1)
            .ok_or_else(invalid)?;
        let note_id = v
            .get("note_id")
            .map(|n| n.as_i64().filter(|n| *n > 0).ok_or_else(invalid))
            .transpose()?;
        let kind = optional(v, "kind", 1024)?;
        if kind.as_ref().is_some_and(|k| k != "note" && k != "journal") {
            return Err(invalid());
        }
        let etag = optional(v, "etag", 1024)?;
        if etag.as_ref().is_some_and(String::is_empty) {
            return Err(invalid());
        }
        let error = optional(v, "error", 16384)?;
        if !error.as_ref().is_some_and(|e| !e.is_empty())
            && (note_id.is_none() || kind.is_none() || etag.is_none())
        {
            return Err(invalid());
        }
        let caption = text(v.get("caption").ok_or_else(invalid)?, 16384)?.to_owned();
        let title = optional(v, "title", 16384)?;
        let vault = optional(v, "vault", 1024)?.unwrap_or_else(|| host_vault.into());
        let lines = v
            .get("lines")
            .and_then(Value::as_array)
            .ok_or_else(invalid)?;
        let mut excerpt = String::new();
        for (i, line) in lines.iter().enumerate() {
            let s = text(line, 256 * 1024)?;
            total = total
                .checked_add(s.len() + usize::from(i > 0))
                .ok_or_else(invalid)?;
            if total > 256 * 1024 {
                return Err(invalid());
            }
            if i > 0 {
                excerpt.push('\n');
            }
            excerpt.push_str(s);
        }
        let mut bad_options = Vec::new();
        if let Some(options) = v.get("bad_options") {
            for option in options
                .as_array()
                .filter(|a| a.len() <= 32)
                .ok_or_else(invalid)?
            {
                bad_options.push(text(option, 1024)?.to_owned());
            }
        }
        result.push(Include {
            line,
            source_line,
            note_id,
            kind,
            etag,
            title,
            caption,
            excerpt,
            bad_options,
            error,
            vault,
            explicit_origin: v.get("vault").is_some(),
        });
    }
    Ok(Response {
        markdown: markdown.to_owned(),
        entries: result,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn entry() -> Value {
        json!({"line":0,"source_line":-1,"note_id":42,"kind":"note","etag":"revision","caption":"alias","title":"Title","lines":["日本語\r","<img src=x>","![[Other]]"],"bad_options":["bad=1"]})
    }
    fn response(e: Value) -> Value {
        json!({"markdown":"host\n","includes":[e]})
    }
    #[test]
    fn includes_preserve_literal_snapshots_and_vault_presence() {
        for origin in [None, Some(""), Some("foreign")] {
            let mut e = entry();
            if let Some(origin) = origin {
                e["vault"] = json!(origin);
            }
            let r = decode(response(e), "host").unwrap();
            assert_eq!(r[0].vault, origin.unwrap_or("host"));
            assert_eq!(r[0].explicit_origin, origin.is_some());
            assert_eq!(r[0].excerpt, "日本語\r\n<img src=x>\n![[Other]]");
            assert_eq!(r[0].source_line, -1);
            assert_eq!(r[0].bad_options, ["bad=1"]);
        }
        let r = decode(
            response(
                json!({"line":0,"source_line":0,"caption":"","lines":[],"error":"unresolved"}),
            ),
            "host",
        )
        .unwrap();
        assert_eq!(r[0].note_id, None);
        assert_eq!(r[0].error.as_deref(), Some("unresolved"));
    }
    #[test]
    fn includes_require_explicit_array_and_valid_identities() {
        assert!(
            decode(json!({"markdown":"","includes":[]}), "")
                .unwrap()
                .is_empty()
        );
        for v in [
            json!({"markdown":""}),
            json!({"markdown":"","includes":null}),
            json!({"markdown":"","includes":{}}),
        ] {
            assert!(decode(v, "").is_err());
        }
        for (field, value) in [
            ("line", json!(-1)),
            ("line", json!(2)),
            ("line", json!(0.5)),
            ("source_line", json!(-2)),
            ("note_id", json!(0)),
            ("kind", json!("other")),
            ("etag", json!("")),
            ("lines", json!([1])),
            ("vault", Value::Null),
            ("caption", Value::Null),
            ("bad_options", Value::Null),
        ] {
            let mut e = entry();
            e[field] = value;
            assert!(decode(response(e), "").is_err(), "{field}");
        }
        let mut e = entry();
        e.as_object_mut().unwrap().remove("etag");
        assert!(decode(response(e), "").is_err());
        assert!(decode(json!({"markdown":"host","includes":[entry(),entry()]}), "").is_err());
    }
    #[test]
    fn includes_bound_text_metadata_and_entry_count() {
        for (field, value) in [
            ("lines", json!(["x".repeat(256 * 1024), ""])),
            ("caption", json!("x".repeat(16385))),
            ("etag", json!("x".repeat(1025))),
            ("bad_options", json!(vec!["x"; 33])),
        ] {
            let mut e = entry();
            e[field] = value;
            assert!(decode(response(e), "").is_err());
        }
        assert!(
            decode(
                json!({"markdown":"x\n".repeat(33),"includes":vec![entry();33]}),
                ""
            )
            .is_err()
        );
    }
}
