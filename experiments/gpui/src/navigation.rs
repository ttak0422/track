//! Bounded page history with explicit fixture snapshot support, independent of tab recency and titles.
use crate::tabs::TabKey;
pub const LIMIT: usize = 100;
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Snapshot {
    pub routes: Vec<TabKey>,
    pub cursor: usize,
}
impl Default for Snapshot {
    fn default() -> Self {
        History::default().snapshot()
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Ticket {
    revision: u64,
    index: usize,
    pub route: Option<TabKey>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct History {
    entries: Vec<Option<TabKey>>,
    cursor: usize,
    revision: u64,
}
impl Default for History {
    fn default() -> Self {
        Self {
            entries: vec![Some(TabKey::Home)],
            cursor: 0,
            revision: 0,
        }
    }
}
impl History {
    pub fn snapshot(&self) -> Snapshot {
        Snapshot {
            routes: self
                .entries
                .iter()
                .map(|r| r.clone().unwrap_or(TabKey::Home))
                .collect(),
            cursor: self.cursor,
        }
    }
    /// Explicit fixture restoration is a new safe Home visit, not an auto-replay.
    /// Discard the saved forward branch and retain at most 99 prior routes.
    pub fn restore_home(snapshot: Snapshot) -> Option<Self> {
        if snapshot.routes.is_empty()
            || snapshot.routes.len() > LIMIT
            || snapshot.cursor >= snapshot.routes.len()
            || snapshot.routes.iter().any(
                |r| matches!(r, TabKey::Tag { tag, scope } if !TabKey::valid_tag_route(tag, scope)),
            )
        {
            return None;
        }
        let mut entries: Vec<_> = snapshot
            .routes
            .into_iter()
            .take(snapshot.cursor + 1)
            .map(Some)
            .collect();
        if entries.last() != Some(&Some(TabKey::Home)) {
            entries.push(Some(TabKey::Home));
        }
        if entries.len() > LIMIT {
            entries.remove(0);
        }
        Some(Self {
            cursor: entries.len() - 1,
            entries,
            revision: 0,
        })
    }

    /// Startup may still choose its configured Home. An accepted empty Home is
    /// instead a real route, even though closing the last tab left no tab entry.
    pub fn reload_route(&self, tabs: &crate::tabs::Tabs) -> Option<TabKey> {
        if matches!(self.current(), Some(TabKey::Tag { .. })) {
            return self.current().cloned();
        }
        tabs.active().map(|t| t.key.clone()).or_else(|| {
            (self.revision != 0)
                .then(|| self.entries[self.cursor].clone())
                .flatten()
        })
    }

    pub fn current(&self) -> Option<&TabKey> {
        self.entries[self.cursor].as_ref()
    }

    pub fn record(&mut self, route: Option<TabKey>) {
        if matches!(&route, Some(TabKey::Tag { tag, scope }) if !TabKey::valid_tag_route(tag, scope))
        {
            return;
        }
        let route = Some(route.unwrap_or(TabKey::Home));
        if self.entries[self.cursor] == route {
            return;
        }
        self.entries.truncate(self.cursor + 1);
        self.entries.push(route);
        if self.entries.len() > LIMIT {
            self.entries.remove(0);
        }
        self.cursor = self.entries.len() - 1;
        self.revision += 1;
    }
    pub fn peek(&self, forward: bool) -> Option<Ticket> {
        let index = if forward {
            self.cursor.checked_add(1)?
        } else {
            self.cursor.checked_sub(1)?
        };
        Some(Ticket {
            revision: self.revision,
            index,
            route: self.entries.get(index)?.clone(),
        })
    }
    pub fn accepts(&self, ticket: &Ticket) -> bool {
        ticket.revision == self.revision
            && self.entries.get(ticket.index) == Some(&ticket.route)
            && self.cursor.abs_diff(ticket.index) == 1
    }
    pub fn commit(&mut self, ticket: &Ticket) -> bool {
        if !self.accepts(ticket) {
            return false;
        }
        self.cursor = ticket.index;
        self.revision += 1;
        true
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn note(vault: &str, note_id: i64) -> Option<TabKey> {
        Some(TabKey::Note {
            vault: vault.into(),
            note_id,
        })
    }
    #[test]
    fn chronological_routes_keep_note_identity_and_discard_abandoned_forward() {
        let tag = TabKey::Tag {
            tag: "garden".into(),
            scope: String::new(),
        };
        let mut tabs = crate::tabs::Tabs::default();
        let note = tabs.open_note("fixture", 42001, "Orchard");
        let routes = [tag, note.clone(), TabKey::Calendar, TabKey::Graph];
        let mut history = History::default();
        for route in &routes {
            history.record(Some(route.clone()));
        }
        tabs.update_title(&note, "Renamed orchard");
        for route in routes[..3].iter().rev() {
            let ticket = history.peek(false).unwrap();
            assert_eq!(ticket.route.as_ref(), Some(route));
            assert!(history.commit(&ticket));
            // Route adapters record the accepted destination again. This must
            // preserve the Forward branch rather than append another visit.
            history.record(Some(route.clone()));
            if route == &note {
                assert_eq!(tabs.get(route).unwrap().title, "Renamed orchard");
            }
        }
        for route in &routes[1..] {
            let ticket = history.peek(true).unwrap();
            assert_eq!(ticket.route.as_ref(), Some(route));
            assert!(history.commit(&ticket));
            history.record(Some(route.clone()));
        }
        assert!(history.peek(true).is_none());
        assert!(history.commit(&history.peek(false).unwrap()));
        let abandoned = history.peek(true).unwrap();
        let work = TabKey::Tag {
            tag: "work".into(),
            scope: String::new(),
        };
        history.record(Some(work.clone()));
        assert_eq!(history.current(), Some(&work));
        assert!(history.peek(true).is_none());
        assert!(!history.commit(&abandoned));
    }

    #[test]
    fn calendar_back_forward_and_restoration_keep_safe_home() {
        let mut history = History::default();
        history.record(Some(TabKey::Calendar));
        history.record(note("work", 42));
        let back = history.peek(false).unwrap();
        assert_eq!(back.route, Some(TabKey::Calendar));
        assert!(history.commit(&back));
        assert_eq!(history.peek(true).unwrap().route, note("work", 42));
        let restored = History::restore_home(history.snapshot()).unwrap();
        assert_eq!(restored.current(), Some(&TabKey::Home));
        assert_eq!(restored.peek(false).unwrap().route, Some(TabKey::Calendar));
    }
    #[test]
    fn tag_page_history_keeps_retained_tabs_and_safe_home_restore() {
        let mut tabs = crate::tabs::Tabs::default();
        let a = tabs.open_note("one", 7, "A");
        let tag = TabKey::Tag {
            tag: "a/b".into(),
            scope: "".into(),
        };
        let mut h = History::default();
        h.record(Some(a.clone()));
        h.record(Some(tag.clone()));
        assert_eq!(h.reload_route(&tabs), Some(tag.clone()));
        assert_eq!(tabs.active().unwrap().key, a);
        let b = tabs.open_note("two", 7, "B");
        h.record(Some(b.clone()));
        let back = h.peek(false).unwrap();
        assert_eq!(back.route, Some(tag.clone()));
        // Merely obtaining a pending transition must not move the cursor.
        assert_eq!(h.current(), Some(&b));
        assert!(h.commit(&back));
        assert_eq!(h.reload_route(&tabs), Some(tag.clone()));
        assert!(h.commit(&h.peek(false).unwrap()));
        assert_eq!(h.current(), Some(&a));
        assert!(h.commit(&h.peek(true).unwrap()));
        assert_eq!(h.current(), Some(&tag));
        let restored = History::restore_home(h.snapshot()).unwrap();
        assert_eq!(restored.current(), Some(&TabKey::Home));
        assert_eq!(restored.peek(false).unwrap().route, Some(tag));
    }

    #[test]
    fn invalid_tag_history_fails_closed_without_moving_cursor() {
        let mut h = History::default();
        let original = h.clone();
        let invalid = TabKey::Tag {
            tag: "a".into(),
            scope: "other".into(),
        };
        h.record(Some(invalid.clone()));
        assert_eq!(h, original);
        assert!(
            History::restore_home(Snapshot {
                routes: vec![invalid],
                cursor: 0
            })
            .is_none()
        );
    }

    #[test]
    fn boundaries_branching_and_duplicate_refresh() {
        let mut h = History::default();
        assert!(h.peek(false).is_none());
        assert!(h.peek(true).is_none());
        h.record(note("a", 1));
        h.record(Some(TabKey::Graph));
        let t = h.peek(false).unwrap();
        assert!(h.commit(&t));
        h.record(note("a", 1)); // reload/title completions do not truncate Forward
        assert!(h.peek(true).is_some());
        h.record(Some(TabKey::Home));
        assert!(h.peek(true).is_none());
        assert!(!h.commit(&t));
    }
    #[test]
    fn reload_preserves_routes_but_does_not_override_initial_home_policy() {
        let mut h = History::default();
        let mut tabs = crate::tabs::Tabs::default();
        assert_eq!(h.reload_route(&tabs), None);
        let a = tabs.open_note("one", 7, "A");
        h.record(Some(a.clone()));
        assert_eq!(h.reload_route(&tabs), Some(a.clone()));
        tabs.close(&a);
        h.record(None);
        assert_eq!(h.reload_route(&tabs), Some(TabKey::Home));
        tabs.open_graph();
        h.record(Some(TabKey::Graph));
        assert_eq!(h.reload_route(&tabs), Some(TabKey::Graph));
    }
    #[test]
    fn rail_availability_survives_home_graph_and_note_round_trip() {
        let mut h = History::default();
        h.record(note("one", 7));
        h.record(Some(TabKey::Graph));
        assert!(h.peek(false).is_some());
        assert!(h.peek(true).is_none());
        assert!(h.commit(&h.peek(false).unwrap()));
        assert!(h.commit(&h.peek(false).unwrap()));
        assert!(h.peek(false).is_none());
        assert!(h.peek(true).is_some());
        assert!(h.commit(&h.peek(true).unwrap()));
        assert_eq!(h.peek(true).unwrap().route, Some(TabKey::Graph));
    }
    #[test]
    fn accepted_async_read_and_title_completion_preserve_forward() {
        let mut h = History::default();
        let mut tabs = crate::tabs::Tabs::default();
        let a = tabs.open_note("one", 7, "Loading");
        h.record(Some(a.clone())); // request acceptance, before any GET result
        let b = tabs.open_note("two", 7, "Other");
        h.record(Some(b.clone()));
        let back = h.peek(false).unwrap();
        assert!(h.commit(&back));
        tabs.open_note("one", 7, "Resolved title");
        h.record(Some(a.clone())); // accepted GET fills the same route
        assert_eq!(h.peek(true).unwrap().route, Some(b));
        let before = h.clone();
        tabs.update_title(&a, "Refreshed title");
        assert_eq!(h, before); // failures/title refreshes do not mutate history
        tabs.close(&a);
        tabs.open_note("one", 7, "Reopened");
        h.record(Some(a));
        assert_eq!(h, before);
    }
    #[test]
    fn bounded_vault_aware_and_peek_is_non_mutating() {
        let mut h = History::default();
        h.record(note("one", 7));
        h.record(note("two", 7));
        let before = h.clone();
        let t = h.peek(false).unwrap();
        assert_eq!(h.peek(false), Some(t.clone()));
        assert_eq!(h, before);
        assert_eq!(t.route, note("one", 7));
        for id in 0..200 {
            h.record(note("one", id));
        }
        assert_eq!(h.entries.len(), LIMIT);
        assert!(!h.commit(&t));
        for _ in 1..LIMIT {
            assert!(h.commit(&h.peek(false).unwrap()));
        }
        assert!(h.peek(false).is_none());
    }
}

#[cfg(test)]
mod restoration_tests {
    use super::*;
    #[test]
    fn restart_home_retains_past_and_truncates_forward_without_replaying() {
        let mut history = History::default();
        history.record(Some(TabKey::Note {
            vault: "one".into(),
            note_id: 7,
        }));
        history.record(Some(TabKey::Note {
            vault: "two".into(),
            note_id: 7,
        }));
        history.commit(&history.peek(false).unwrap());
        let mut restored = History::restore_home(history.snapshot()).unwrap();
        assert_eq!(restored.snapshot().routes.last(), Some(&TabKey::Home));
        assert!(restored.peek(true).is_none());
        let back = restored.peek(false).unwrap();
        assert_eq!(
            back.route,
            Some(TabKey::Note {
                vault: "one".into(),
                note_id: 7
            })
        );
        restored.commit(&back);
        assert_eq!(restored.peek(true).unwrap().route, Some(TabKey::Home));
        restored.record(Some(TabKey::Graph));
        assert!(restored.peek(true).is_none());
    }
    #[test]
    fn restore_bounds_empty_invalid_cursor_and_repeated_home() {
        assert!(
            History::restore_home(Snapshot {
                routes: vec![],
                cursor: 0
            })
            .is_none()
        );
        assert!(
            History::restore_home(Snapshot {
                routes: vec![TabKey::Home],
                cursor: 1
            })
            .is_none()
        );
        assert!(
            History::restore_home(Snapshot {
                routes: vec![TabKey::Home; 101],
                cursor: 0
            })
            .is_none()
        );
        let mut history = History::default();
        for id in 1..=150 {
            history.record(Some(TabKey::Note {
                vault: "one".into(),
                note_id: id,
            }));
        }
        let restored = History::restore_home(history.snapshot()).unwrap();
        assert_eq!(restored.snapshot().routes.len(), 100);
        assert_eq!(
            History::restore_home(restored.snapshot())
                .unwrap()
                .snapshot(),
            restored.snapshot()
        );
    }
}
