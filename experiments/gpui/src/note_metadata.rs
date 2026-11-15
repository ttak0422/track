//! Read-only labels projected from accepted source, never from a live list hint.
use crate::{
    live::{NoteBody, NoteKey},
    tabs::TabKey,
};

/// Display classification only. Flags never confer permissions or change editing.
#[derive(Debug, PartialEq, Eq)]
pub enum Flag {
    Deprecated,
    Confidential,
    Other(String),
}

impl Flag {
    fn from_source(value: &str) -> Option<Self> {
        match value {
            "" => None,
            "DEPRECATED" => Some(Self::Deprecated),
            "CONFIDENTIAL" => Some(Self::Confidential),
            other => Some(Self::Other(other.to_owned())),
        }
    }

    pub fn label(&self) -> &str {
        match self {
            Self::Deprecated => "DEPRECATED",
            Self::Confidential => "CONFIDENTIAL",
            Self::Other(value) => value,
        }
    }
}

#[derive(Debug, Default, PartialEq, Eq)]
pub struct Metadata {
    pub tags: Vec<String>,
    pub flags: Vec<Flag>,
    pub milestone: Option<&'static str>,
}

impl Metadata {
    pub fn current(
        live: bool,
        active: &TabKey,
        body: Option<&NoteBody>,
        accepted_request: Option<&NoteKey>,
        snapshot_tags: &[String],
    ) -> Self {
        if !live {
            return Self {
                tags: snapshot_tags
                    .iter()
                    .filter(|tag| !tag.is_empty())
                    .cloned()
                    .collect(),
                ..Self::default()
            };
        }
        let Some(body) = body.filter(|body| {
            accepted_request.is_some_and(|request| {
                matches!(active, TabKey::Note { vault, note_id }
                    if vault == &request.vault && *note_id == request.note_id)
                    && request.note_id == body.note.note_id
                    // Only a source accepted for the launch-vault alias may carry
                    // a canonical named vault. Never treat every empty route as a match.
                    && (request.vault.is_empty() || request.vault == body.note.vault)
            })
        }) else {
            return Self::default();
        };
        Self {
            tags: body
                .note
                .tags
                .iter()
                .filter(|tag| !tag.is_empty())
                .cloned()
                .collect(),
            flags: body
                .note
                .flags
                .iter()
                .filter_map(|flag| Flag::from_source(flag))
                .collect(),
            milestone: recorded_milestone(&body.note.additional),
        }
    }
}

// These optional fields are evidence only: absence may mean a failed sidecar
// read. Never infer unread/new, and never record a milestone from rendering.
fn recorded_milestone(
    fields: &std::collections::BTreeMap<String, serde_json::Value>,
) -> Option<&'static str> {
    let valid = |key| {
        fields
            .get(key)
            .and_then(serde_json::Value::as_i64)
            .is_some_and(|seconds| seconds > 0)
    };
    if valid("read_at") {
        Some("Recorded read")
    } else if valid("seen_at") {
        Some("Recorded seen")
    } else {
        None
    }
}

fn flag_labels<'a>(flags: impl Iterator<Item = &'a str>) -> String {
    flags
        .map(|flag| format!("[{flag}]"))
        .collect::<Vec<_>>()
        .join(" ")
}

pub fn relation_flags(value: &serde_json::Value) -> String {
    flag_labels(
        value
            .get("flags")
            .and_then(|v| v.as_array())
            .into_iter()
            .flatten()
            .filter_map(|v| v.as_str()),
    )
}

pub fn relation_title(title: &str, flags: &str) -> String {
    if flags.is_empty() {
        title.to_owned()
    } else {
        format!("{title} {flags}")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn body() -> NoteBody {
        serde_json::from_value(json!({"note_id":42,"vault":"work","title":"A","file_kind":"note","path":"a","body":"","etag":"e","tags":["fresh"],"flags":["DEPRECATED","CONFIDENTIAL"]})).unwrap()
    }
    fn key(vault: &str, note_id: i64) -> TabKey {
        TabKey::Note {
            vault: vault.into(),
            note_id,
        }
    }
    #[test]
    fn flags_classify_exact_names_and_preserve_inert_unknown_text() {
        assert_eq!(Flag::from_source(""), None);
        assert_eq!(Flag::from_source("DEPRECATED"), Some(Flag::Deprecated));
        assert_eq!(Flag::from_source("CONFIDENTIAL"), Some(Flag::Confidential));
        for value in [
            "deprecated",
            " CONFIDENTIAL",
            "FUTURE_RULE",
            "<script>",
            "[x](url)",
            "日本語",
            " ",
        ] {
            let flag = Flag::from_source(value).unwrap();
            assert_eq!(flag, Flag::Other(value.into()));
            assert_eq!(flag.label(), value);
        }
        assert_eq!(Flag::Deprecated.label(), "DEPRECATED");
        assert_eq!(Flag::Confidential.label(), "CONFIDENTIAL");
    }

    #[test]
    fn empty_labels_are_omitted_without_discarding_other_metadata() {
        let mut body = body();
        body.note.tags = vec!["".into(), "日本語".into(), "[x](url)".into()];
        body.note.flags = vec!["".into(), "FUTURE_RULE".into(), "CONFIDENTIAL".into()];
        let metadata = Metadata::current(
            true,
            &key("work", 42),
            Some(&body),
            Some(&body.note.key()),
            &[],
        );
        assert_eq!(metadata.tags, ["日本語", "[x](url)"]);
        assert_eq!(
            metadata.flags,
            [Flag::Other("FUTURE_RULE".into()), Flag::Confidential]
        );
        assert!(
            Metadata::current(false, &key("work", 42), None, None, &["".into()])
                .tags
                .is_empty()
        );
    }

    #[test]
    fn accepted_source_wins_over_snapshot_even_when_empty() {
        let mut body = body();
        let stale = vec!["stale".into()];
        let metadata = Metadata::current(
            true,
            &key("work", 42),
            Some(&body),
            Some(&body.note.key()),
            &stale,
        );
        assert_eq!(metadata.tags, ["fresh"]);
        assert_eq!(metadata.flags, [Flag::Deprecated, Flag::Confidential]);
        body.note.tags.clear();
        body.note.flags.clear();
        assert_eq!(
            Metadata::current(
                true,
                &key("work", 42),
                Some(&body),
                Some(&body.note.key()),
                &stale
            ),
            Metadata::default()
        );
    }
    #[test]
    fn cleared_source_and_identity_changes_never_fall_back_to_live_snapshot() {
        let body = body();
        for active in [
            key("other", 42),
            key("work", 43),
            TabKey::Home,
            TabKey::Graph,
        ] {
            assert_eq!(
                Metadata::current(
                    true,
                    &active,
                    Some(&body),
                    Some(&body.note.key()),
                    &["stale".into()]
                ),
                Metadata::default()
            );
        }
        assert_eq!(
            Metadata::current(true, &key("work", 42), None, None, &["stale".into()]),
            Metadata::default()
        );
        assert_eq!(
            Metadata::current(false, &key("work", 42), None, None, &["fixture".into()]).tags,
            ["fixture"]
        );
    }
    #[test]
    fn launch_alias_requires_the_request_that_accepted_this_body() {
        let body = body();
        let launch = NoteKey {
            vault: String::new(),
            note_id: 42,
        };
        let route = key("", 42);
        let metadata = Metadata::current(true, &route, Some(&body), Some(&launch), &[]);
        assert_eq!(metadata.tags, ["fresh"]);
        assert_eq!(metadata.flags, [Flag::Deprecated, Flag::Confidential]);
        assert_eq!(
            Metadata::current(true, &key("other", 42), Some(&body), Some(&launch), &[]),
            Metadata::default()
        );
        // A named-vault source cannot be reused under the launch alias or another vault.
        assert_eq!(
            Metadata::current(true, &route, Some(&body), Some(&body.note.key()), &[]),
            Metadata::default()
        );
        let wrong = NoteKey {
            vault: "other".into(),
            note_id: 42,
        };
        assert_eq!(
            Metadata::current(true, &key("other", 42), Some(&body), Some(&wrong), &[]),
            Metadata::default()
        );
        assert_eq!(
            Metadata::current(true, &route, Some(&body), None, &[]),
            Metadata::default()
        );
        assert_eq!(
            Metadata::current(true, &route, None, Some(&launch), &[]),
            Metadata::default()
        );
    }

    #[test]
    fn recorded_milestones_require_valid_positive_seconds_and_prefer_read() {
        for invalid in [
            json!(null),
            json!(0),
            json!(-1),
            json!("123"),
            json!(true),
            json!([]),
            json!({}),
            json!(1.5),
            json!(u64::MAX),
        ] {
            let fields =
                serde_json::from_value(json!({"read_at":invalid,"seen_at":invalid})).unwrap();
            assert_eq!(recorded_milestone(&fields), None);
        }
        for seconds in [1_i64, 1794653381, 253402300800, i64::MAX] {
            let fields = serde_json::from_value(json!({"seen_at":seconds})).unwrap();
            assert_eq!(recorded_milestone(&fields), Some("Recorded seen"));
            let fields = serde_json::from_value(json!({"seen_at":seconds,"read_at":1})).unwrap();
            assert_eq!(recorded_milestone(&fields), Some("Recorded read"));
        }
        let fields = serde_json::from_value(json!({"seen_at":1,"read_at":"bad"})).unwrap();
        assert_eq!(recorded_milestone(&fields), Some("Recorded seen"));
        assert_eq!(recorded_milestone(&Default::default()), None);
    }
    #[test]
    fn recorded_milestones_follow_only_current_accepted_source() {
        let mut body = body();
        body.note
            .additional
            .insert("read_at".into(), json!(1794653381));
        let request = body.note.key();
        assert_eq!(
            Metadata::current(true, &key("work", 42), Some(&body), Some(&request), &[]).milestone,
            Some("Recorded read")
        );
        for active in [key("foreign", 42), key("work", 43), TabKey::Home] {
            assert_eq!(
                Metadata::current(true, &active, Some(&body), Some(&request), &[]).milestone,
                None
            );
        }
        assert_eq!(
            Metadata::current(true, &key("work", 42), Some(&body), None, &[]).milestone,
            None
        );
        assert_eq!(
            Metadata::current(false, &key("work", 42), Some(&body), Some(&request), &[]).milestone,
            None
        );
        body.note.additional.remove("read_at");
        assert_eq!(
            Metadata::current(true, &key("work", 42), Some(&body), Some(&request), &[]).milestone,
            None
        );
        body.note
            .additional
            .insert("seen_at".into(), json!(1794653381));
        assert_eq!(
            Metadata::current(true, &key("work", 42), Some(&body), Some(&request), &[]).milestone,
            Some("Recorded seen")
        );
        assert_eq!(
            Metadata::current(true, &key("work", 42), None, Some(&request), &[]).milestone,
            None
        );
    }

    #[test]
    fn relations_label_all_string_flags_without_interpreting_them() {
        for value in [json!({}), json!({"flags":null}), json!({"flags":[]})] {
            assert_eq!(relation_title("A", &relation_flags(&value)), "A");
        }
        let value = json!({"flags":["DEPRECATED","CONFIDENTIAL","<future>",9]});
        assert_eq!(
            relation_title("A", &relation_flags(&value)),
            "A [DEPRECATED] [CONFIDENTIAL] [<future>]"
        );
    }
}
