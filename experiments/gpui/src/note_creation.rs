//! Pure single-flight owner for the fixture-only title dialog. No transport.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Phase {
    #[default]
    Editing,
    Pending,
    Created,
    Uncertain,
}
#[derive(Default)]
pub struct Flow {
    pub phase: Phase,
    sequence: u64,
    pub title: String,
    pub launch: u64,
}
impl Flow {
    pub fn begin(&mut self, title: &str, launch: u64) -> Option<u64> {
        if self.phase != Phase::Editing || title.trim().is_empty() || launch == 0 {
            return None;
        }
        self.sequence = self.sequence.checked_add(1)?;
        self.title = title.trim().into();
        self.launch = launch;
        self.phase = Phase::Pending;
        Some(self.sequence)
    }
    pub fn finish(&mut self, ticket: u64, phase: Phase) -> bool {
        if self.sequence != ticket || self.phase != Phase::Pending || phase == Phase::Pending {
            return false;
        }
        self.phase = phase;
        true
    }
    pub fn can_dismiss(&self) -> bool {
        matches!(self.phase, Phase::Editing | Phase::Created)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn empty_title_or_unknown_launch_never_admitted() {
        let mut state = Flow::default();
        assert_eq!(state.begin(" \n", 1), None);
        assert_eq!(state.begin("Alpha", 0), None);
        assert_eq!(state.phase, Phase::Editing);
    }
    #[test]
    fn double_submission_freezes_title_and_destination() {
        let mut state = Flow::default();
        let ticket = state.begin(" 雪 / \"x\" ", 1).unwrap();
        assert!(state.begin("Other", 2).is_none());
        assert_eq!(state.title, "雪 / \"x\"");
        assert_eq!(state.launch, 1);
        assert!(!state.can_dismiss());
        assert!(state.finish(ticket, Phase::Created));
        assert!(!state.finish(ticket, Phase::Editing));
        assert!(state.begin("Again", 1).is_none());
    }
    #[test]
    fn rejected_request_needs_new_explicit_attempt_and_stale_response_is_ignored() {
        let mut state = Flow::default();
        let old = state.begin("Alpha", 1).unwrap();
        assert!(state.finish(old, Phase::Editing));
        let current = state.begin("Beta", 1).unwrap();
        assert!(!state.finish(old, Phase::Created));
        assert!(state.finish(current, Phase::Created));
    }
    #[test]
    fn uncertain_is_not_cancelable_or_retriable() {
        let mut state = Flow::default();
        let ticket = state.begin("Alpha", 1).unwrap();
        assert!(state.finish(ticket, Phase::Uncertain));
        assert_eq!(state.title, "Alpha");
        assert!(!state.can_dismiss());
        assert!(state.begin("Alpha", 1).is_none());
        assert!(!state.finish(ticket, Phase::Editing));
    }
}
