//! Read-only labels projected from accepted source, never from a live list hint.
use crate::{live::NoteBody, tabs::TabKey};

#[derive(Debug, Default, PartialEq, Eq)]
pub struct Metadata {
    pub tags: Vec<String>,
    pub flags: String,
}

impl Metadata {
    pub fn current(
        live: bool,
        active: &TabKey,
        body: Option<&NoteBody>,
        snapshot_tags: &[String],
    ) -> Self {
        if !live {
            return Self {
                tags: snapshot_tags.to_vec(),
                ..Self::default()
            };
        }
        let Some(body) = body.filter(|body| {
            matches!(active, TabKey::Note { vault, note_id }
            if vault == &body.note.vault && *note_id == body.note.note_id)
        }) else {
            return Self::default();
        };
        Self {
            tags: body.note.tags.clone(),
            flags: flag_labels(body.note.flags.iter().map(String::as_str)),
        }
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
    fn accepted_source_wins_over_snapshot_even_when_empty() {
        let mut body = body();
        let stale = vec!["stale".into()];
        let metadata = Metadata::current(true, &key("work", 42), Some(&body), &stale);
        assert_eq!(metadata.tags, ["fresh"]);
        assert_eq!(metadata.flags, "[DEPRECATED] [CONFIDENTIAL]");
        body.note.tags.clear();
        body.note.flags.clear();
        assert_eq!(
            Metadata::current(true, &key("work", 42), Some(&body), &stale),
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
                Metadata::current(true, &active, Some(&body), &["stale".into()]),
                Metadata::default()
            );
        }
        assert_eq!(
            Metadata::current(true, &key("work", 42), None, &["stale".into()]),
            Metadata::default()
        );
        assert_eq!(
            Metadata::current(false, &key("work", 42), None, &["fixture".into()]).tags,
            ["fixture"]
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
