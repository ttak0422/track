//! Bounded projections and paging for existing accepted reader relations.
use crate::shell::LinkSource;
use serde_json::Value;

pub(crate) const LIMIT: usize = 4096;
pub(crate) fn entries(value: Option<&Value>) -> Result<Option<&[Value]>, &'static str> {
    let Some(value) = value else {
        return Ok(None);
    };
    let rows = value
        .as_array()
        .ok_or("Relation data unavailable (invalid list).")?;
    if rows.len() > LIMIT {
        return Err("Relation data exceeds the 4096-entry limit.");
    }
    if rows.iter().any(|r| {
        r.get("title")
            .and_then(Value::as_str)
            .is_none_or(|s| s.len() > 65536)
            || r.get("note_id")
                .and_then(Value::as_i64)
                .is_none_or(|id| id <= 0)
            || r.get("vault").is_some_and(|v| {
                v.as_str()
                    .is_none_or(|s| s.len() > 1024 || s.contains('\0'))
            })
    }) {
        return Err("Relation data unavailable (invalid entry).");
    }
    Ok(Some(rows))
}
pub(crate) fn summary(section: &str, count: usize, incomplete: bool) -> String {
    if incomplete {
        if count == 0 {
            "Relations unavailable or incomplete; reload after recovery.".into()
        } else {
            format!("{count} available entries · incomplete or cached")
        }
    } else if count == 0 {
        format!("No {section}.")
    } else {
        format!("{count} entries")
    }
}
pub(crate) fn scroll_id(section: &str, source: Option<&LinkSource>, page: usize) -> String {
    format!("{section}-rows:{source:?}:{page}")
}
pub(crate) fn page(total: usize, requested: usize) -> (usize, usize, std::ops::Range<usize>) {
    let pages = total.div_ceil(100);
    let page = requested.min(pages.saturating_sub(1));
    let start = page * 100;
    (page, pages, start..(start + 100).min(total))
}
#[derive(Default)]
pub(crate) struct Pages {
    source: Option<LinkSource>,
    children: usize,
    backlinks: usize,
}
impl Pages {
    pub fn current(&mut self, source: Option<&LinkSource>, section: &str) -> usize {
        if self.source.as_ref() != source {
            self.source = source.cloned();
            self.children = 0;
            self.backlinks = 0;
        }
        if section == "children" {
            self.children
        } else {
            self.backlinks
        }
    }
    pub fn set(&mut self, source: &LinkSource, section: &str, page: usize) {
        if self.source.as_ref() == Some(source) {
            if section == "children" {
                self.children = page
            } else {
                self.backlinks = page
            }
        }
    }
}
pub(crate) fn allows(
    source: &LinkSource,
    current: Option<&LinkSource>,
    rows: Option<&Value>,
    index: usize,
    row: &Value,
    available: bool,
) -> bool {
    available
        && current == Some(source)
        && entries(rows)
            .ok()
            .flatten()
            .and_then(|rows| rows.get(index))
            == Some(row)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn source() -> LinkSource {
        LinkSource {
            note: crate::live::NoteKey {
                vault: "work".into(),
                note_id: 1,
            },
            etag: "a".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        }
    }
    fn row(id: i64) -> Value {
        serde_json::json!({"note_id":id,"title":format!("note{id}"),"flags":["PIN"]})
    }
    #[test]
    fn relation_partial_or_cached_reads_never_claim_complete_counts() {
        for diagnostic in ["partial", "cache_used"] {
            let fields =
                serde_json::from_value(serde_json::json!({"read_state":{diagnostic:true}}))
                    .unwrap();
            let incomplete = crate::workspace_activity::incomplete(&fields);
            assert!(incomplete);
            assert!(summary("backlinks", 0, incomplete).contains("unavailable or incomplete"));
            assert!(summary("children", 3, incomplete).contains("incomplete or cached"));
        }
        assert_eq!(summary("backlinks", 0, false), "No backlinks.");
        assert_eq!(summary("children", 3, false), "3 entries");
        let source = source();
        assert_ne!(
            scroll_id("children", Some(&source), 0),
            scroll_id("children", Some(&source), 1)
        );
        let mut next = source.clone();
        next.request += 1;
        assert_ne!(
            scroll_id("children", Some(&source), 0),
            scroll_id("children", Some(&next), 0)
        );
    }
    #[test]
    fn relation_projection_preserves_order_and_distinguishes_absent_invalid_and_empty() {
        assert!(entries(None).unwrap().is_none());
        let empty = serde_json::json!([]);
        assert!(entries(Some(&empty)).unwrap().unwrap().is_empty());
        let data = serde_json::json!([row(5), row(2)]);
        assert_eq!(
            entries(Some(&data)).unwrap().unwrap(),
            data.as_array().unwrap()
        );
        for bad in [
            Value::Null,
            serde_json::json!({}),
            serde_json::json!([{"note_id":0,"title":"x"}]),
            serde_json::json!([{"note_id":1,"title":"x","vault":null}]),
            serde_json::json!([{"note_id":1}]),
            serde_json::json!(vec![row(1); 4097]),
        ] {
            assert!(entries(Some(&bad)).is_err());
        }
    }
    #[test]
    fn relation_pages_reach_every_entry_and_reset_on_source_revision() {
        for n in [0usize, 1, 100, 101, 4096] {
            let mut all = Vec::new();
            for p in 0..n.div_ceil(100) {
                all.extend(page(n, p).2);
            }
            assert_eq!(all, (0..n).collect::<Vec<_>>());
            assert_eq!(page(n, usize::MAX).2.end, n);
        }
        let source = source();
        let mut pages = Pages::default();
        assert_eq!(pages.current(Some(&source), "children"), 0);
        pages.set(&source, "children", 5);
        assert_eq!(pages.current(Some(&source), "children"), 5);
        assert_eq!(pages.current(Some(&source), "backlinks"), 0);
        let mut next = source.clone();
        next.etag = "b".into();
        assert_eq!(pages.current(Some(&next), "children"), 0);
        pages.set(&source, "children", 9);
        assert_eq!(pages.current(Some(&next), "children"), 0);
    }
    #[test]
    fn relation_navigation_requires_current_exact_source_and_index_membership() {
        let source = source();
        let entry = row(2);
        let rows = serde_json::json!([entry.clone()]);
        assert!(allows(&source, Some(&source), Some(&rows), 0, &entry, true));
        assert!(!allows(
            &source,
            Some(&source),
            Some(&rows),
            0,
            &entry,
            false
        ));
        assert!(!allows(
            &source,
            Some(&source),
            Some(&rows),
            1,
            &entry,
            true
        ));
        assert!(!allows(
            &source,
            Some(&source),
            Some(&rows),
            0,
            &row(3),
            true
        ));
        for i in 0..6 {
            let mut current = source.clone();
            match i {
                0 => current.note.vault = "other".into(),
                1 => current.note.note_id += 1,
                2 => current.etag = "b".into(),
                3 => current.storage_generation += 1,
                4 => current.launch_generation += 1,
                _ => current.request += 1,
            };
            assert!(!allows(
                &source,
                Some(&current),
                Some(&rows),
                0,
                &entry,
                true
            ));
        }
        let foreign = serde_json::json!({"note_id":2,"title":"note2","vault":"other"});
        assert!(!allows(
            &source,
            Some(&source),
            Some(&serde_json::json!([foreign])),
            0,
            &entry,
            true
        ));
    }
}
