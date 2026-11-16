//! Display-only search metadata from the same accepted engine result.
//! No interpretation as Markdown, HTML, links, or a note identity.
use serde_json::Value;
use std::collections::BTreeMap;
pub(crate) const ROW_HEIGHT: f32 = 100.;
pub(crate) const TITLE_HEIGHT: f32 = 20.;
pub(crate) const TAG_HEIGHT: f32 = 14.;
pub(crate) const REASON_HEIGHT: f32 = 14.;
pub(crate) const EXCERPT_HEIGHT: f32 = 16.;
pub(crate) const VERTICAL_PADDING: f32 = 12.;
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(crate) struct Detail {
    pub reason: Option<&'static str>,
    pub excerpt: Option<String>,
}
pub(crate) fn project(fields: &BTreeMap<String, Value>) -> Detail {
    let reason = match fields.get("match").and_then(Value::as_str) {
        Some("title") => Some("Title match"),
        Some("body") => Some("Full-text match"),
        Some("path") => Some("Filename match"),
        _ => None,
    };
    let excerpt = fields
        .get("snippet")
        .and_then(Value::as_str)
        .and_then(|raw| {
            // Inspect only a bounded prefix. Never slice UTF-8 bytes or interpret markup.
            let mut chars = raw.chars();
            let mut text: String = chars
                .by_ref()
                .take(160)
                .map(|c| {
                    if c.is_control() || c == '\u{2028}' || c == '\u{2029}' {
                        ' '
                    } else {
                        c
                    }
                })
                .collect();
            if chars.next().is_some() {
                text.push('…');
            }
            (!text.trim().is_empty()).then_some(text)
        });
    Detail { reason, excerpt }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn fields(value: Value) -> BTreeMap<String, Value> {
        serde_json::from_value(value).unwrap()
    }
    #[test]
    fn reasons_use_only_engine_discriminator() {
        for (wire, label) in [
            ("title", "Title match"),
            ("body", "Full-text match"),
            ("path", "Filename match"),
        ] {
            assert_eq!(
                project(&fields(serde_json::json!({"match":wire}))).reason,
                Some(label)
            );
        }
        for value in [
            serde_json::json!({}),
            serde_json::json!({"match":null,"snippet":42}),
            serde_json::json!({"match":"unknown","snippet":null}),
        ] {
            assert_eq!(project(&fields(value)), Detail::default());
        }
    }
    #[test]
    fn unicode_is_bounded_and_markup_remains_literal() {
        let raw = format!("{}END", "日本語🦀".repeat(50));
        let d = project(&fields(serde_json::json!({"snippet":raw})));
        let text = d.excerpt.unwrap();
        assert_eq!(text.chars().count(), 161);
        assert!(text.ends_with('…'));
        assert!(!text.contains("END"));
        let raw = "**bold** [[Note]] [link](https://example.test) <b>tag</b>";
        assert_eq!(
            project(&fields(serde_json::json!({"snippet":raw})))
                .excerpt
                .as_deref(),
            Some(raw)
        );
    }
    #[test]
    fn controls_are_display_spaces_and_missing_snippet_keeps_reason() {
        assert_eq!(
            project(&fields(
                serde_json::json!({"match":"body","snippet":"a\r\n\tb\u{0000}c"})
            ))
            .excerpt
            .as_deref(),
            Some("a   b c")
        );
        assert_eq!(
            project(&fields(
                serde_json::json!({"match":"path","snippet":"\n\t"})
            )),
            Detail {
                reason: Some("Filename match"),
                excerpt: None
            }
        );
        let old = fields(serde_json::json!({"match":"body","snippet":"old"}));
        let new = fields(serde_json::json!({"match":"title","snippet":"new"}));
        assert_ne!(project(&old), project(&new));
        assert_eq!(project(&new).excerpt.as_deref(), Some("new"));
    }
    #[test]
    fn live_row_reserves_all_four_single_lines_and_border() {
        assert!(
            ROW_HEIGHT
                >= TITLE_HEIGHT
                    + TAG_HEIGHT
                    + REASON_HEIGHT
                    + EXCERPT_HEIGHT
                    + 2. * VERTICAL_PADDING
                    + 1.
        );
    }
}
