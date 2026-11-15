//! Bounded note recency model. Fixture persistence is owned by SessionStore.
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
    #[cfg(test)]
    pub fn snapshot(&self) -> Vec<Entry> {
        self.entries.clone()
    }
    /// Session metadata may only cross into the writer bound to this launch.
    pub fn snapshot_for_launch(&self, launch: u64) -> Vec<Entry> {
        if self.launch == Some(launch) {
            self.entries.clone()
        } else {
            Vec::new()
        }
    }
    /// Reset first; installing saved metadata is not a visit or a note read.
    pub fn restore(&mut self, launch: u64, expected_revision: u64, entries: Vec<Entry>) -> bool {
        if self.revision != expected_revision {
            return false;
        }
        self.reset_launch(launch);
        self.entries = entries;
        self.revision = self.revision.wrapping_add(1);
        true
    }
    pub fn record(&mut self, launch: u64, key: NoteKey, title: &str) {
        self.reset_launch(launch);
        if key.note_id <= 0 || key.vault.len() > 1024 || key.vault.contains('\0') {
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
    fn rejected_restore_never_stages_previous_launch_but_keeps_newer_current_visits() {
        let mut model = RecentNotes::default();
        model.record(1, key("main", 1), "Previous launch");
        assert!(model.snapshot_for_launch(2).is_empty());
        // Rejected startup restore resets stale UI rows before staging metadata.
        model.reset_launch(2);
        let staged = model.snapshot_for_launch(2);
        assert!(staged.is_empty());
        assert!(model.rows("", "main").is_empty());
        let startup_revision = model.revision();
        model.record(2, key("work", 2), "New accepted visit");
        assert!(!model.restore(2, startup_revision, vec![]));
        // Same-launch reset is inert: a delayed restore cannot erase this visit.
        model.reset_launch(2);
        let staged = model.snapshot_for_launch(2);
        assert_eq!(staged.len(), 1);
        assert_eq!(staged[0].key, key("work", 2));
        assert!(model.snapshot_for_launch(1).is_empty());
    }
    #[test]
    fn restore_is_not_a_visit_and_cannot_replace_a_newer_acceptance() {
        let saved = vec![Entry {
            key: key("work", 1),
            title: "Saved".into(),
        }];
        let mut model = RecentNotes::default();
        let ticket = model.revision();
        assert!(model.restore(7, ticket, saved.clone()));
        model.reset_launch(7);
        assert_eq!(model.snapshot(), saved);
        let ticket = model.revision();
        model.record(7, key("main", 2), "New");
        assert!(!model.restore(7, ticket, saved));
        assert_eq!(model.snapshot()[0].key, key("main", 2));
        model.record(7, key("main", -1), "Invalid");
        assert_eq!(model.snapshot().len(), 2);
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
