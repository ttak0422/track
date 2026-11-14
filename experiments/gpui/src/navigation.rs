//! Bounded, session-only page history, independent of tab recency and titles.
use crate::tabs::TabKey;
const LIMIT: usize = 100;
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
    /// Startup may still choose its configured Home. An accepted empty Home is
    /// instead a real route, even though closing the last tab left no tab entry.
    pub fn reload_route(&self, tabs: &crate::tabs::Tabs) -> Option<TabKey> {
        tabs.active().map(|t| t.key.clone()).or_else(|| {
            (self.revision != 0)
                .then(|| self.entries[self.cursor].clone())
                .flatten()
        })
    }

    pub fn record(&mut self, route: Option<TabKey>) {
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
