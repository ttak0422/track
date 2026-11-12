//! In-memory reader tabs. No persistence, editor state, or implicit I/O.
//! Entries are kept in presentation/MRU order: the active tab is always first.

pub const ACTIVE_WIDTH: u32 = 336;
pub const INACTIVE_WIDTH: u32 = 168;
pub const OVERFLOW_WIDTH: u32 = 44;

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum TabKey {
    Home,
    Note { vault: String, note_id: i64 },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Tab {
    pub key: TabKey,
    pub title: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Tabs {
    entries: Vec<Tab>,
}

/// Width is the space available to tabs, after any vault switcher or window chrome.
/// A narrow viewport clips the active tab rather than silently compressing its width.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct TabLayout {
    pub visible: Vec<TabKey>,
    pub overflow: Vec<TabKey>,
}
impl TabLayout {
    pub fn overflow_count(&self) -> usize {
        self.overflow.len()
    }
}

impl Tabs {
    pub fn entries(&self) -> &[Tab] {
        &self.entries
    }

    pub fn active(&self) -> Option<&Tab> {
        self.entries.first()
    }

    pub fn get(&self, key: &TabKey) -> Option<&Tab> {
        self.entries.iter().find(|tab| &tab.key == key)
    }

    pub fn open_home(&mut self) -> TabKey {
        self.open(TabKey::Home, "Home".into())
    }

    pub fn open_note(
        &mut self,
        vault: impl Into<String>,
        note_id: i64,
        title: impl Into<String>,
    ) -> TabKey {
        self.open(
            TabKey::Note {
                vault: vault.into(),
                note_id,
            },
            title.into(),
        )
    }

    fn open(&mut self, key: TabKey, title: String) -> TabKey {
        if let Some(index) = self.entries.iter().position(|tab| tab.key == key) {
            let mut tab = self.entries.remove(index);
            tab.title = title;
            self.entries.insert(0, tab);
        } else {
            self.entries.insert(
                0,
                Tab {
                    key: key.clone(),
                    title,
                },
            );
        }
        key
    }

    /// Unknown identities are a no-op; the caller must fetch/open them explicitly.
    pub fn activate(&mut self, key: &TabKey) -> bool {
        let Some(index) = self.entries.iter().position(|tab| &tab.key == key) else {
            return false;
        };
        if index != 0 {
            let tab = self.entries.remove(index);
            self.entries.insert(0, tab);
        }
        true
    }

    /// Closing the active tab selects its right-hand neighbor. Closing any other tab
    /// leaves the active identity unchanged. Closing the last tab leaves an empty model;
    /// the shell can show Home without inventing a persisted Home tab.
    pub fn close(&mut self, key: &TabKey) -> bool {
        let Some(index) = self.entries.iter().position(|tab| &tab.key == key) else {
            return false;
        };
        self.entries.remove(index);
        true
    }

    /// Refresh a title without changing focus or recency order.
    pub fn update_title(&mut self, key: &TabKey, title: impl Into<String>) -> bool {
        let Some(tab) = self.entries.iter_mut().find(|tab| &tab.key == key) else {
            return false;
        };
        tab.title = title.into();
        true
    }

    pub fn layout(&self, available_width: u32) -> TabLayout {
        if self.entries.is_empty() {
            return TabLayout::default();
        }
        let inactive = self.entries.len() - 1;
        let all_fit = u64::from(ACTIVE_WIDTH) + inactive as u64 * u64::from(INACTIVE_WIDTH)
            <= u64::from(available_width);
        let visible_count = if all_fit {
            self.entries.len()
        } else {
            // Keep at least the active tab. Overflow remains reachable at the strip's
            // right edge even when the active title needs to be clipped by the shell.
            let budget = available_width.saturating_sub(ACTIVE_WIDTH + OVERFLOW_WIDTH);
            1 + (budget / INACTIVE_WIDTH) as usize
        }
        .min(self.entries.len());
        TabLayout {
            visible: self.entries[..visible_count]
                .iter()
                .map(|tab| tab.key.clone())
                .collect(),
            overflow: self.entries[visible_count..]
                .iter()
                .map(|tab| tab.key.clone())
                .collect(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_model_has_no_active_or_overflow() {
        let tabs = Tabs::default();
        assert!(tabs.active().is_none());
        assert_eq!(tabs.layout(1000), TabLayout::default());
    }

    #[test]
    fn reopening_updates_title_and_deduplicates() {
        let mut tabs = Tabs::default();
        let a = tabs.open_note("work", 7, "Old title");
        tabs.open_note("work", 8, "Other");
        assert_eq!(tabs.open_note("work", 7, "New title"), a);
        assert_eq!(tabs.entries().len(), 2);
        assert_eq!(tabs.active().unwrap().title, "New title");
        assert_eq!(tabs.active().unwrap().key, a);
    }

    #[test]
    fn same_id_in_different_vaults_is_distinct() {
        let mut tabs = Tabs::default();
        let a = tabs.open_note("one", 7, "Same");
        let b = tabs.open_note("two", 7, "Same");
        let launch = tabs.open_note("", 7, "Same");
        assert_ne!(a, b);
        assert_ne!(a, launch);
        assert_eq!(tabs.entries().len(), 3);
        assert!(tabs.activate(&a));
        assert_eq!(tabs.active().unwrap().key, a);
    }

    #[test]
    fn activation_moves_active_leftmost_without_duplicates() {
        let mut tabs = Tabs::default();
        let a = tabs.open_note("", 1, "A");
        let b = tabs.open_note("", 2, "B");
        let c = tabs.open_note("", 3, "C");
        tabs.activate(&a);
        tabs.activate(&a);
        assert_eq!(tabs.layout(1000).visible, vec![a, c, b]);
    }

    #[test]
    fn closing_active_selects_neighbor_then_empty() {
        let mut tabs = Tabs::default();
        let a = tabs.open_note("", 1, "A");
        let b = tabs.open_note("", 2, "B");
        assert!(tabs.close(&b));
        assert_eq!(tabs.active().unwrap().key, a);
        assert!(tabs.close(&a));
        assert!(tabs.active().is_none());
    }

    #[test]
    fn closing_inactive_preserves_active() {
        let mut tabs = Tabs::default();
        let a = tabs.open_note("", 1, "A");
        let b = tabs.open_note("", 2, "B");
        tabs.close(&a);
        assert_eq!(tabs.active().unwrap().key, b);
    }

    #[test]
    fn unknown_actions_do_not_change_model() {
        let mut tabs = Tabs::default();
        tabs.open_note("", 1, "A");
        let before = tabs.clone();
        assert!(!tabs.activate(&TabKey::Home));
        assert!(!tabs.close(&TabKey::Home));
        assert!(!tabs.update_title(&TabKey::Home, "Missing"));
        assert_eq!(tabs, before);
    }

    #[test]
    fn home_is_unique_and_not_a_note_named_home() {
        let mut tabs = Tabs::default();
        let note = tabs.open_note("", 0, "Home");
        tabs.open_home();
        tabs.open_home();
        assert_eq!(tabs.entries().len(), 2);
        assert_eq!(tabs.active().unwrap().key, TabKey::Home);
        assert_eq!(tabs.get(&note).unwrap().title, "Home");
    }

    #[test]
    fn background_title_update_does_not_change_order() {
        let mut tabs = Tabs::default();
        let a = tabs.open_note("", 1, "A");
        let b = tabs.open_note("", 2, "B");
        tabs.update_title(&a, "Renamed 日本語");
        assert_eq!(tabs.active().unwrap().key, b);
        assert_eq!(tabs.get(&a).unwrap().title, "Renamed 日本語");
    }

    #[test]
    fn exact_fit_has_no_overflow() {
        let mut tabs = Tabs::default();
        for id in 0..3 {
            tabs.open_note("", id, "Note");
        }
        let layout = tabs.layout(ACTIVE_WIDTH + 2 * INACTIVE_WIDTH);
        assert_eq!(layout.visible.len(), 3);
        assert_eq!(layout.overflow_count(), 0);
    }

    #[test]
    fn overflow_reserves_its_control_and_activation_promotes() {
        let mut tabs = Tabs::default();
        let oldest = tabs.open_note("", 0, "Oldest");
        for id in 1..5 {
            tabs.open_note("", id, "Note");
        }
        let width = ACTIVE_WIDTH + INACTIVE_WIDTH + OVERFLOW_WIDTH;
        let layout = tabs.layout(width);
        assert_eq!(layout.visible.len(), 2);
        assert_eq!(layout.overflow_count(), 3);
        assert!(layout.overflow.contains(&oldest));
        tabs.activate(&oldest);
        assert_eq!(tabs.layout(width).visible[0], oldest);
        assert_eq!(tabs.entries().len(), 5);
    }

    #[test]
    fn tiny_width_keeps_active_identity_and_overflow() {
        let mut tabs = Tabs::default();
        let a = tabs.open_note("", 1, "A");
        let b = tabs.open_note("", 2, "B");
        assert_eq!(
            tabs.layout(0),
            TabLayout {
                visible: vec![b],
                overflow: vec![a]
            }
        );
    }
}
