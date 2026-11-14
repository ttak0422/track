//! Read-only projection of relationships from an already accepted note response.
use crate::live::NoteKey;
use serde_json::Value;
use std::collections::BTreeMap;

#[derive(Debug, PartialEq, Eq)]
pub struct Reference {
    pub key: NoteKey,
    pub title: String,
    pub flags: String,
}

#[derive(Default, Debug)]
pub struct ExternalReferences {
    pub references: Vec<Reference>,
    pub notices: Vec<String>,
}

impl ExternalReferences {
    pub fn from_related(related: &BTreeMap<String, Value>) -> Self {
        let mut result = Self::default();
        for field in ["external", "unavailable"] {
            let Some(value) = related.get(field) else {
                continue;
            };
            let Some(entries) = value.as_array() else {
                result.notices.push(format!(
                    "Could not read {field} relationships: expected a list."
                ));
                continue;
            };
            for (index, entry) in entries.iter().enumerate() {
                let valid = if field == "external" {
                    parse_reference(entry).map(|reference| result.references.push(reference))
                } else {
                    parse_unavailable(entry).map(|notice| result.notices.push(notice))
                };
                if valid.is_none() {
                    result.notices.push(format!(
                        "Could not read {field} entry {}: invalid relationship data.",
                        index + 1
                    ));
                }
            }
        }
        result
    }

    pub fn visible(&self) -> bool {
        !self.references.is_empty() || !self.notices.is_empty()
    }
}

fn parse_reference(entry: &Value) -> Option<Reference> {
    // No inherited scope: an external link must explicitly identify its vault.
    Some(Reference {
        key: NoteKey {
            vault: entry.get("vault")?.as_str()?.to_owned(),
            note_id: entry.get("note_id")?.as_i64().filter(|id| *id > 0)?,
        },
        title: entry.get("title")?.as_str()?.to_owned(),
        flags: crate::note_metadata::relation_flags(entry),
    })
}

fn parse_unavailable(entry: &Value) -> Option<String> {
    let name = entry.get("name")?.as_str()?;
    let detail = match entry.get("error") {
        None => "",
        Some(value) => value.as_str()?,
    };
    // Never render the optional local filesystem path.
    Some(if detail.is_empty() {
        format!("vault “{name}” could not be checked")
    } else {
        format!("vault “{name}” could not be checked: {detail}")
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn project(value: Value) -> ExternalReferences {
        ExternalReferences::from_related(&serde_json::from_value(value).unwrap())
    }

    #[test]
    fn absent_and_empty_lists_have_no_section() {
        for value in [json!({}), json!({"external":[],"unavailable":[]})] {
            assert!(!project(value).visible());
        }
    }

    #[test]
    fn preserves_order_and_vault_identity_including_launch_scope() {
        let result = project(json!({"external":[
            {"vault":"work","note_id":42,"title":"A","unknown":true},
            {"vault":"research","note_id":42,"title":"B"},
            {"vault":"","note_id":42,"title":"C"}
        ]}));
        assert_eq!(
            result
                .references
                .iter()
                .map(|r| r.key.vault.as_str())
                .collect::<Vec<_>>(),
            ["work", "research", ""]
        );
        assert_ne!(result.references[0].key, result.references[1].key);
        assert!(result.notices.is_empty());
    }

    #[test]
    fn flags_are_labels_without_changing_cross_vault_targets() {
        let result = project(json!({"external":[
            {"vault":"work","note_id":42,"title":"A","flags":["DEPRECATED","CONFIDENTIAL"]},
            {"vault":"other","note_id":42,"title":"A","flags":["future"]}
        ]}));
        assert_eq!(result.references[0].flags, "[DEPRECATED] [CONFIDENTIAL]");
        assert_eq!(result.references[1].flags, "[future]");
        assert_ne!(result.references[0].key, result.references[1].key);
    }

    #[test]
    fn unavailable_only_and_mixed_results_do_not_expose_path() {
        for external in [json!([]), json!([{"vault":"work","note_id":2,"title":"A"}])] {
            let result = project(
                json!({"external":external,"unavailable":[{"name":"archive","path":"/private/vault","error":"offline"},{"name":"other"}],"read_state":{"status":"partial"}}),
            );
            assert!(result.visible());
            assert_eq!(
                result.notices,
                [
                    "vault “archive” could not be checked: offline",
                    "vault “other” could not be checked"
                ]
            );
        }
    }

    #[test]
    fn malformed_entries_are_disclosed_and_never_navigable() {
        for entry in [
            json!({"note_id":1,"title":"A"}),
            json!({"vault":null,"note_id":1,"title":"A"}),
            json!({"vault":7,"note_id":1,"title":"A"}),
            json!({"vault":"work","note_id":0,"title":"A"}),
            json!({"vault":"work","note_id":-1,"title":"A"}),
            json!({"vault":"work","note_id":"1","title":"A"}),
            json!({"vault":"work","note_id":1.5,"title":"A"}),
            json!({"vault":"work","note_id":18446744073709551615u64,"title":"A"}),
            json!({"vault":"work","note_id":1}),
            json!(null),
        ] {
            let result = project(json!({"external":[entry]}));
            assert!(result.references.is_empty());
            assert_eq!(result.notices.len(), 1);
            assert!(result.visible());
        }
    }

    #[test]
    fn malformed_arrays_and_notices_are_not_verified_empty() {
        for value in [
            json!({"external":null}),
            json!({"external":{}}),
            json!({"unavailable":false}),
            json!({"unavailable":[{"vault":"wrong-field"}]}),
            json!({"unavailable":[{"name":"archive","error":null}]}),
        ] {
            assert!(project(value).visible());
        }
    }

    #[test]
    fn projection_does_not_mutate_partial_diagnostics_or_drop_valid_neighbors() {
        let related: BTreeMap<String,Value> = serde_json::from_value(json!({"read_state":{"phase":"partial"},"external":[{}, {"vault":"research","note_id":3,"title":"Valid"}]})).unwrap();
        let before = related.clone();
        let result = ExternalReferences::from_related(&related);
        assert_eq!(related, before);
        assert_eq!(result.references.len(), 1);
        assert_eq!(result.notices.len(), 1);
    }
}
