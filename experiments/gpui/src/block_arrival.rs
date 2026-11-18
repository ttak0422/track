//! Session-only arrival paint. No Find, text, layout, clipboard or persistence state.
use crate::code_copy::Document;
#[derive(Clone, Debug, PartialEq)]
pub struct Source {
    pub document: Document,
    pub index: usize,
    pub id: String,
}
#[derive(Clone, Debug, PartialEq)]
pub struct Ticket {
    pub source: Source,
    serial: u64,
}
#[derive(Default)]
pub struct State {
    pub active: Option<Ticket>,
    serial: u64,
}
impl State {
    pub fn show(&mut self, source: Source) -> Ticket {
        self.serial = self.serial.wrapping_add(1);
        let ticket = Ticket {
            source,
            serial: self.serial,
        };
        self.active = Some(ticket.clone());
        ticket
    }
    pub fn retain(&mut self, current: Option<&Source>) {
        if self
            .active
            .as_ref()
            .is_some_and(|ticket| Some(&ticket.source) != current)
        {
            self.active = None;
        }
    }
    pub fn clear(&mut self) {
        self.active = None;
    }
    pub fn expire(&mut self, ticket: &Ticket) -> bool {
        if self.active.as_ref() != Some(ticket) {
            return false;
        }
        self.clear();
        true
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn source() -> Source {
        Source {
            document: Document {
                tab: Some(crate::tabs::TabKey::Note {
                    vault: "work".into(),
                    note_id: 42,
                }),
                request: 1,
                etag: Some("rev".into()),
                storage: Some(2),
                launch: Some(3),
            },
            index: 4,
            id: "block".into(),
        }
    }
    #[test]
    fn newer_same_target_and_different_target_ignore_old_timers() {
        let mut state = State::default();
        let first = state.show(source());
        let second = state.show(source());
        assert!(!state.expire(&first));
        assert!(state.expire(&second));
        assert!(!state.expire(&second));
        let mut other = source();
        other.index += 1;
        let third = state.show(other);
        assert!(!state.expire(&second));
        assert_eq!(state.active, Some(third));
    }
    #[test]
    fn navigation_revision_scope_epoch_and_modal_invalidate_without_revival() {
        let base = source();
        let mut changes = vec![base.clone(); 8];
        changes[0].document.request += 1;
        changes[1].document.etag = Some("other".into());
        changes[2].document.storage = Some(4);
        changes[3].document.launch = Some(4);
        changes[4].document.tab = Some(crate::tabs::TabKey::Note {
            vault: "other".into(),
            note_id: 42,
        });
        changes[5].document.tab = Some(crate::tabs::TabKey::Note {
            vault: "work".into(),
            note_id: 43,
        });
        changes[6].index += 1;
        changes[7].id.push('X');
        for current in changes.iter().map(Some).chain(std::iter::once(None)) {
            let mut state = State::default();
            let old = state.show(base.clone());
            state.retain(current);
            assert!(state.active.is_none());
            state.retain(Some(&base));
            assert!(state.active.is_none());
            assert!(!state.expire(&old));
        }
    }
    #[test]
    fn explicit_clear_and_matching_source_do_not_create_or_extend_cue() {
        let mut state = State::default();
        state.retain(Some(&source()));
        assert!(state.active.is_none());
        let ticket = state.show(source());
        state.retain(Some(&source()));
        assert_eq!(state.active, Some(ticket.clone()));
        state.clear();
        assert!(!state.expire(&ticket));
    }
}
