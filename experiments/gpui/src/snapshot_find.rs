//! Viewer-local rendered-text Find; no host reader or filesystem authority.
use crate::{
    markdown::Block,
    reader_find::{self, Hit, Matches},
};
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Identity {
    pub snapshot: u64,
    pub selection: u64,
    pub file: String,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Ticket {
    identity: Identity,
    revision: u64,
    pub hit: Hit,
}
#[derive(Default)]
pub(crate) struct Model {
    pub open: bool,
    pub query: String,
    pub matches: Matches,
    pub active: Option<usize>,
    identity: Option<Identity>,
    revision: u64,
}
impl Model {
    pub fn bind(&mut self, identity: Option<Identity>, blocks: &[Block]) -> bool {
        if self.identity == identity {
            return false;
        }
        self.identity = identity;
        self.evaluate(blocks);
        true
    }
    pub fn evaluate(&mut self, blocks: &[Block]) {
        self.revision = self.revision.wrapping_add(1);
        self.matches = if self.open && self.identity.is_some() {
            reader_find::search(blocks, &self.query)
        } else {
            Matches::default()
        };
        self.active = (!self.matches.hits.is_empty()).then_some(0);
    }
    pub fn step(&mut self, forward: bool) {
        self.active = reader_find::step(self.active, self.matches.hits.len(), forward);
        self.revision = self.revision.wrapping_add(1);
    }
    pub fn close(&mut self) {
        self.open = false;
        self.matches = Matches::default();
        self.active = None;
        self.revision = self.revision.wrapping_add(1);
    }
    pub fn action_current(&self, expected: &Identity, current: &Identity, revision: u64) -> bool {
        self.open && expected == current && revision == self.revision
    }
    pub fn revision(&self) -> u64 {
        self.revision
    }
    pub fn current(&self, current: Option<&Identity>) -> bool {
        self.open && current.is_some() && self.identity.as_ref() == current
    }
    pub fn ticket(&self, current: Option<&Identity>) -> Option<Ticket> {
        let identity = self.identity.as_ref()?;
        if !self.open || Some(identity) != current {
            return None;
        }
        Some(Ticket {
            identity: identity.clone(),
            revision: self.revision,
            hit: self.matches.hits.get(self.active?)?.clone(),
        })
    }
    pub fn reveal_current(
        &self,
        ticket: &Ticket,
        current: Option<&Identity>,
        same_anchors: bool,
        active_window: bool,
        composing: bool,
    ) -> bool {
        same_anchors && active_window && !composing && self.accepts(ticket, current)
    }
    pub fn accepts(&self, ticket: &Ticket, current: Option<&Identity>) -> bool {
        self.ticket(current).as_ref() == Some(ticket)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn owner(selection: u64) -> Identity {
        Identity {
            snapshot: 1,
            selection,
            file: "a.md".into(),
        }
    }
    #[test]
    fn stale_tickets_retire_for_query_close_file_and_snapshot() {
        let blocks = crate::markdown::parse("# 日本語\n\nHello **WORLD**\n\nhello world");
        let mut m = Model {
            open: true,
            query: "hello world".into(),
            ..Default::default()
        };
        m.bind(Some(owner(1)), &blocks);
        assert_eq!(m.matches.hits.len(), 2);
        let first = m.ticket(Some(&owner(1))).unwrap();
        assert!(m.action_current(&owner(1), &owner(1), m.revision()));
        assert!(!m.action_current(&owner(1), &owner(2), m.revision()));
        assert!(m.reveal_current(&first, Some(&owner(1)), true, true, false));
        assert!(!m.reveal_current(&first, Some(&owner(1)), false, true, false));
        assert!(!m.reveal_current(&first, Some(&owner(1)), true, false, false));
        assert!(!m.reveal_current(&first, Some(&owner(1)), true, true, true));
        m.step(true);
        assert!(!m.accepts(&first, Some(&owner(1))));
        let second = m.ticket(Some(&owner(1))).unwrap();
        m.step(true);
        assert_eq!(m.active, Some(0));
        m.step(false);
        assert_eq!(m.active, Some(1));
        m.bind(None, &blocks);
        assert!(m.matches.hits.is_empty());
        assert!(!m.accepts(&second, Some(&owner(1))));
        m.bind(Some(owner(2)), &blocks);
        assert_eq!(m.matches.hits.len(), 2);
        assert!(!m.accepts(&first, Some(&owner(2))));
        let ticket = m.ticket(Some(&owner(2))).unwrap();
        m.query = "日本語".into();
        m.evaluate(&blocks);
        assert!(!m.accepts(&ticket, Some(&owner(2))));
        let ticket = m.ticket(Some(&owner(2))).unwrap();
        m.close();
        assert!(!m.accepts(&ticket, Some(&owner(2))));
        assert!(m.matches.hits.is_empty());
    }
}
