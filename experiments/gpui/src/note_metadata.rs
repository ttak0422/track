//! Read-only labels projected from accepted source, never from a live list hint.
use crate::{
    live::{NoteBody, NoteKey},
    tabs::TabKey,
};

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
        accepted_request: Option<&NoteKey>,
        snapshot_tags: &[String],
    ) -> Self {
        if !live {
            return Self {
                tags: snapshot_tags.to_vec(),
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
        let metadata = Metadata::current(
            true,
            &key("work", 42),
            Some(&body),
            Some(&body.note.key()),
            &stale,
        );
        assert_eq!(metadata.tags, ["fresh"]);
        assert_eq!(metadata.flags, "[DEPRECATED] [CONFIDENTIAL]");
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
        assert_eq!(metadata.flags, "[DEPRECATED] [CONFIDENTIAL]");
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
