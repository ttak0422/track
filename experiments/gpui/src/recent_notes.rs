//! Current-process note recency only. Deliberately has no serialization or I/O.
use crate::live::NoteKey;
const LIMIT: usize = 100;
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Entry {
    pub key: NoteKey,
    pub title: String,
}
#[derive(Default)]
pub(crate) struct RecentNotes {
    launch: Option<u64>,
    revision: u64,
    entries: Vec<Entry>,
}
impl RecentNotes {
    pub fn reset_launch(&mut self, launch: u64) {
        if self.launch != Some(launch) {
            self.launch = Some(launch);
            self.entries.clear();
            self.revision = self.revision.wrapping_add(1);
        }
    }
    pub fn record(&mut self, launch: u64, key: NoteKey, title: &str) {
        self.reset_launch(launch);
        if key.note_id == 0 || key.vault.len() > 1024 || key.vault.contains('\0') {
            return;
        }
        self.entries.retain(|entry| entry.key != key);
        self.entries.insert(
            0,
            Entry {
                key,
                title: label(title),
            },
        );
        self.entries.truncate(LIMIT);
        self.revision = self.revision.wrapping_add(1);
    }
    pub fn update_title(&mut self, key: &NoteKey, title: &str) {
        if let Some(entry) = self.entries.iter_mut().find(|entry| &entry.key == key) {
            let title = label(title);
            if entry.title != title {
                entry.title = title;
                self.revision = self.revision.wrapping_add(1);
            }
        }
    }
    pub fn revision(&self) -> u64 {
        self.revision
    }
    pub fn rows(&self, scope: &str, launch_name: &str) -> Vec<Entry> {
        self.entries
            .iter()
            .filter(|entry| {
                scope.is_empty() || entry.key.vault == scope || entry.key.vault == launch_name
            })
            .cloned()
            .collect()
    }
    pub fn contains(&self, key: &NoteKey) -> bool {
        self.entries.iter().any(|entry| &entry.key == key)
    }
}
fn label(title: &str) -> String {
    let title = title.trim();
    if title.is_empty() {
        "Untitled".into()
    } else {
        title.chars().take(512).collect()
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn key(vault: &str, id: i64) -> NoteKey {
        NoteKey {
            vault: vault.into(),
            note_id: id,
        }
    }
    #[test]
    fn accepted_revisits_dedupe_and_replace_title() {
        let mut recent = RecentNotes::default();
        recent.record(1, key("main", 1), "Old");
        recent.record(1, key("main", 2), "B");
        recent.record(1, key("main", 1), "New");
        let rows = recent.rows("", "main");
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0].title, "New");
        assert_eq!(rows[1].key.note_id, 2);
        // No tab state is stored or consulted; closing a tab cannot erase a visit.
        let mut tabs = crate::tabs::Tabs::default();
        tabs.open_note("main", 1, "A");
        tabs.close(&crate::tabs::TabKey::Note {
            vault: "main".into(),
            note_id: 1,
        });
        assert!(recent.contains(&key("main", 1)));
        recent.update_title(&key("main", 2), "Updated B");
        let rows = recent.rows("", "main");
        assert_eq!(rows[0].key.note_id, 1);
        assert_eq!(rows[1].title, "Updated B");
    }
    #[test]
    fn identities_scopes_bounds_and_launch_are_independent() {
        let mut recent = RecentNotes::default();
        recent.record(1, key("main", 1), "");
        recent.record(1, key("work", 1), "Work");
        recent.record(1, key("other", 1), "Other");
        let rows = recent.rows("work", "main");
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[1].title, "Untitled");
        assert_eq!(recent.rows("", "main").len(), 3);
        for id in 2..103 {
            recent.record(1, key("work", id), "X");
        }
        assert_eq!(recent.rows("", "main").len(), 100);
        assert!(!recent.contains(&key("main", 1)));
        recent.reset_launch(2);
        assert!(recent.rows("", "main").is_empty());
    }
    #[test]
    fn no_acceptance_means_no_change_and_titles_are_bounded() {
        let mut recent = RecentNotes::default();
        recent.record(1, key("main", 1), "A");
        let before = recent.rows("", "main");
        let revision = recent.revision();
        // Failed/canceled/preview/special routes do not call record.
        assert_eq!(recent.rows("", "main"), before);
        assert_eq!(recent.revision(), revision);
        recent.record(1, key("main", 2), &"あ".repeat(600));
        assert_eq!(recent.rows("", "main")[0].title.chars().count(), 512);
    }
}
