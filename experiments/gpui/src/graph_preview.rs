//! Full-graph preview lifecycle foundation; not wired to the local reader graph.
//! Clock values are monotonic milliseconds supplied by the UI. Reads are emitted
//! only by `advance`; transport/UI must never infer a target from current selection.
#![allow(dead_code)]

pub const OPEN_DELAY_MS: u64 = 260;
pub const CLOSE_DELAY_MS: u64 = 220;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Target {
    pub vault: String,
    pub note_id: i64,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Scope {
    pub session: u64,
    pub graph: u64,
    pub vault: String,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Read {
    pub scope: Scope,
    pub target: Target,
    pub token: u64,
}
/// Diagnostics travel with the body, including successfully fetched empty bodies.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Content {
    pub title: String,
    pub body: String,
    pub read_only: bool,
    pub partial: bool,
    pub diagnostics: Vec<String>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum State {
    Loading,
    Ready(Content),
    Failed(String),
}
#[derive(Clone, Debug, PartialEq)]
pub struct Preview {
    pub read: Read,
    /// Placement is captured once per target, not updated by pointer movement.
    pub anchor: (f32, f32),
    pub state: State,
    pointer_inside: bool,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum PinResult {
    Pinned,
    NothingToPin,
    /// UI must display this limitation, never replace existing pinned content.
    CapacityReached,
}
#[derive(Default)]
pub struct Previews {
    scope: Option<Scope>,
    serial: u64,
    pending: Option<(Target, (f32, f32), u64)>,
    opener: Option<Target>,
    close_at: Option<u64>,
    pub transient: Option<Preview>,
    pub pinned: Option<Preview>,
}
impl Previews {
    pub fn set_scope(&mut self, scope: Scope) {
        if self.scope.as_ref() == Some(&scope) {
            return;
        }
        if self.scope.as_ref().map(|s| s.session) != Some(scope.session) {
            self.pinned = None;
        }
        self.clear_transient();
        self.scope = Some(scope);
    }
    /// Call immediately when the owned server fails or is replaced.
    pub fn invalidate_session(&mut self) {
        self.clear_transient();
        self.pinned = None;
        self.scope = None;
    }
    pub fn navigate(&mut self) {
        self.clear_transient();
    }
    fn clear_transient(&mut self) {
        self.transient = None;
        self.pending = None;
        self.opener = None;
        self.close_at = None;
    }
    pub fn hover(&mut self, target: Target, anchor: (f32, f32), now: u64) {
        if !self.scope.as_ref().is_some_and(|s| s.vault == target.vault) {
            return;
        }
        if self.opener.as_ref() == Some(&target) {
            return;
        }
        self.opener = Some(target.clone());
        self.pending = None;
        self.close_at = None;
        if self
            .transient
            .as_ref()
            .is_some_and(|p| p.read.target == target)
        {
            return;
        }
        // Switching targets invalidates the old read immediately, even before B opens.
        self.transient = None;
        if self
            .pinned
            .as_ref()
            .is_some_and(|p| p.read.target == target)
        {
            return;
        }
        self.pending = Some((target, anchor, now.saturating_add(OPEN_DELAY_MS)));
    }
    pub fn leave_opener(&mut self, now: u64) {
        if self.opener.is_none() && self.pending.is_none() {
            return;
        }
        self.opener = None;
        self.pending = None;
        if !self.transient.as_ref().is_some_and(|p| p.pointer_inside) {
            self.close_at = Some(now.saturating_add(CLOSE_DELAY_MS));
        }
    }
    /// Identity is the card's captured Read, not its current slot. It survives
    /// pin/unpin; events from removed/replaced cards are ignored.
    pub fn enter_preview(&mut self, identity: &Read) -> bool {
        if let Some(preview) = self.transient.as_mut().filter(|p| &p.read == identity) {
            preview.pointer_inside = true;
            self.close_at = None;
            return true;
        }
        if let Some(preview) = self.pinned.as_mut().filter(|p| &p.read == identity) {
            preview.pointer_inside = true;
            return true;
        }
        false
    }
    pub fn leave_preview(&mut self, identity: &Read, now: u64) -> bool {
        if let Some(preview) = self.transient.as_mut().filter(|p| &p.read == identity) {
            preview.pointer_inside = false;
            if self.opener.as_ref() != Some(&preview.read.target) {
                self.close_at = Some(now.saturating_add(CLOSE_DELAY_MS));
            }
            return true;
        }
        if let Some(preview) = self.pinned.as_mut().filter(|p| &p.read == identity) {
            preview.pointer_inside = false;
            return true;
        }
        false
    }
    /// Adapter keeps one wake for this deadline; unchanged deadlines need no new task.
    pub fn next_deadline(&self) -> Option<u64> {
        self.pending
            .as_ref()
            .map(|(_, _, at)| *at)
            .into_iter()
            .chain(self.close_at)
            .min()
    }
    pub fn advance(&mut self, now: u64) -> Option<Read> {
        if self.close_at.is_some_and(|deadline| now >= deadline) {
            self.transient = None;
            self.close_at = None;
        }
        if !self
            .pending
            .as_ref()
            .is_some_and(|(_, _, deadline)| now >= *deadline)
        {
            return None;
        }
        let (target, anchor, _) = self.pending.take()?;
        let scope = self.scope.clone()?;
        self.serial = self
            .serial
            .checked_add(1)
            .expect("preview request counter exhausted");
        let read = Read {
            scope,
            target,
            token: self.serial,
        };
        self.transient = Some(Preview {
            read: read.clone(),
            anchor,
            state: State::Loading,
            pointer_inside: false,
        });
        Some(read)
    }
    pub fn complete(&mut self, read: &Read, result: Result<Content, String>) -> bool {
        if self.scope.as_ref().map(|s| s.session) != Some(read.scope.session) {
            return false;
        }
        let target = if self.pinned.as_ref().is_some_and(|p| &p.read == read) {
            self.pinned.as_mut()
        } else {
            // Graph changes remove transient slots; an explicitly unpinned slot
            // may retain an earlier graph identity while its read finishes.
            self.transient.as_mut().filter(|p| &p.read == read)
        };
        let Some(preview) = target else {
            return false;
        };
        preview.state = match result {
            Ok(content) => State::Ready(content),
            Err(error) => State::Failed(error),
        };
        true
    }
    /// Action identity is resolved at dispatch, not from a rendered slot label.
    pub fn close_identity(&mut self, identity: &Read) -> bool {
        if self.transient.as_ref().is_some_and(|p| &p.read == identity) {
            self.close_transient();
            return true;
        }
        if self.pinned.as_ref().is_some_and(|p| &p.read == identity) {
            self.close_pinned();
            return true;
        }
        false
    }
    pub fn pin_identity(&mut self, identity: &Read) -> PinResult {
        if !self.transient.as_ref().is_some_and(|p| &p.read == identity) {
            return PinResult::NothingToPin;
        }
        self.pin()
    }
    pub fn unpin_identity(&mut self, identity: &Read, now: u64) -> bool {
        if !self.pinned.as_ref().is_some_and(|p| &p.read == identity) {
            return false;
        }
        self.unpin(now);
        true
    }
    pub fn pin(&mut self) -> PinResult {
        if self.transient.is_none() {
            return PinResult::NothingToPin;
        }
        if self.pinned.is_some() {
            return PinResult::CapacityReached;
        }
        self.pinned = self.transient.take();
        self.close_at = None;
        PinResult::Pinned
    }
    /// Unpin is explicit: it replaces the disposable transient, never another pin.
    pub fn unpin(&mut self, now: u64) {
        let Some(preview) = self.pinned.take() else {
            return;
        };
        let opener = self
            .opener
            .take()
            .filter(|target| target == &preview.read.target);
        self.clear_transient();
        self.opener = opener;
        if !preview.pointer_inside && self.opener.is_none() {
            self.close_at = Some(now.saturating_add(CLOSE_DELAY_MS));
        }
        self.transient = Some(preview);
    }
    pub fn close_transient(&mut self) {
        self.clear_transient();
    }
    pub fn close_pinned(&mut self) {
        if self
            .pinned
            .as_ref()
            .is_some_and(|p| self.opener.as_ref() == Some(&p.read.target))
        {
            self.opener = None;
        }
        self.pinned = None;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn scope(session: u64, graph: u64, vault: &str) -> Scope {
        Scope {
            session,
            graph,
            vault: vault.into(),
        }
    }
    fn target(vault: &str, note_id: i64) -> Target {
        Target {
            vault: vault.into(),
            note_id,
        }
    }
    fn model() -> Previews {
        let mut p = Previews::default();
        p.set_scope(scope(1, 1, "fixture"));
        p
    }
    fn content(body: &str) -> Content {
        Content {
            title: "Fixture".into(),
            body: body.into(),
            read_only: true,
            partial: true,
            diagnostics: vec!["omitted include".into()],
        }
    }
    fn open(p: &mut Previews, id: i64, now: u64) -> Read {
        p.hover(target("fixture", id), (12., 20.), now);
        p.advance(now + 260).unwrap()
    }
    #[test]
    fn exact_delay_and_coalesced_fake_reads() {
        let mut p = model();
        let mut reads = Vec::new();
        p.hover(target("fixture", 1), (12., 20.), 0);
        p.hover(target("fixture", 1), (200., 300.), 100);
        assert!(p.advance(259).is_none());
        reads.extend(p.advance(260));
        reads.extend(p.advance(500));
        assert_eq!(reads.len(), 1);
        assert_eq!(p.transient.as_ref().unwrap().anchor, (12., 20.));
        p.hover(target("fixture", 1), (4., 5.), 600);
        assert!(p.advance(1000).is_none());
    }
    #[test]
    fn leaving_before_intent_never_reads() {
        let mut p = model();
        p.hover(target("fixture", 1), (0., 0.), 0);
        p.leave_opener(259);
        assert!(p.advance(1000).is_none());
    }
    #[test]
    fn late_a_cannot_replace_b_or_resurrect_closed() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        p.hover(target("fixture", 2), (7., 9.), 300);
        assert!(!p.complete(&a, Ok(content("A"))));
        let b = p.advance(560).unwrap();
        assert_eq!(p.transient.as_ref().unwrap().state, State::Loading);
        assert!(p.complete(&b, Ok(content("B"))));
        p.close_transient();
        assert!(!p.complete(&b, Ok(content("late B"))));
        let again = open(&mut p, 2, 600);
        assert_ne!(again.token, b.token);
    }
    #[test]
    fn pointer_bridge_exact_close_boundary() {
        let mut p = model();
        open(&mut p, 1, 0);
        p.leave_opener(300);
        p.advance(519);
        assert!(p.transient.is_some());
        let identity = p.transient.as_ref().unwrap().read.clone();
        p.enter_preview(&identity);
        p.advance(600);
        assert!(p.transient.is_some());
        p.leave_preview(&identity, 700);
        p.advance(919);
        assert!(p.transient.is_some());
        p.advance(920);
        assert!(p.transient.is_none());
    }
    #[test]
    fn pin_survives_navigation_and_capacity_is_explicit() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        p.leave_opener(300);
        assert_eq!(p.pin(), PinResult::Pinned);
        p.advance(900);
        p.navigate();
        p.set_scope(scope(1, 2, "other"));
        assert!(p.complete(&a, Ok(content("pinned A"))));
        assert!(p.pinned.is_some());
        p.set_scope(scope(1, 3, "fixture"));
        open(&mut p, 2, 1000);
        assert_eq!(p.pin(), PinResult::CapacityReached);
        assert_eq!(p.pinned.as_ref().unwrap().read, a);
        p.unpin(1500);
        p.advance(1719);
        assert!(p.transient.is_some());
        p.advance(1720);
        assert!(p.transient.is_none());
    }
    #[test]
    fn graph_vault_session_and_pending_are_isolated() {
        let mut p = model();
        let a = open(&mut p, 7, 0);
        p.set_scope(scope(1, 2, "other"));
        assert!(!p.complete(&a, Ok(content("wrong"))));
        p.hover(target("fixture", 7), (0., 0.), 300);
        assert!(p.advance(1000).is_none());
        p.hover(target("other", 7), (1., 2.), 1000);
        let b = p.advance(1260).unwrap();
        assert_ne!(a.target, b.target);
        p.pin();
        p.set_scope(scope(2, 2, "other"));
        assert!(p.pinned.is_none());
        assert!(!p.complete(&b, Ok(content("old session"))));
        p.hover(target("other", 8), (0., 0.), 1400);
        p.set_scope(scope(2, 3, "other"));
        assert!(p.advance(2000).is_none());
    }
    #[test]
    fn pinned_dedupe_and_unpinned_inflight_keeps_exact_target() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        p.pin();
        p.navigate();
        p.hover(target("fixture", 1), (90., 80.), 300);
        assert!(p.next_deadline().is_none());
        assert!(p.advance(1000).is_none());
        p.set_scope(scope(1, 2, "other"));
        p.unpin(1100);
        assert_eq!(p.next_deadline(), Some(1320));
        assert!(p.complete(&a, Ok(content("exact pinned target"))));
        assert_eq!(
            p.transient.as_ref().unwrap().read.target,
            target("fixture", 1)
        );
        p.advance(1320);
        assert!(!p.complete(&a, Ok(content("closed"))));
    }
    #[test]
    fn unpin_under_pointer_waits_for_exact_card_leave() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        p.pin();
        p.leave_opener(300);
        assert!(p.enter_preview(&a));
        p.unpin(400);
        p.advance(1000);
        assert!(p.transient.is_some());
        assert!(p.next_deadline().is_none());
        assert!(p.leave_preview(&a, 1100));
        p.advance(1319);
        assert!(p.transient.is_some());
        p.advance(1320);
        assert!(p.transient.is_none());
    }
    #[test]
    fn pinned_pointer_never_holds_different_transient_and_stale_events_are_ignored() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        p.pin();
        let b = open(&mut p, 2, 300);
        p.leave_opener(600);
        assert!(p.enter_preview(&a));
        p.advance(820);
        assert!(p.transient.is_none());
        assert!(p.pinned.is_some());
        let c = open(&mut p, 3, 900);
        p.leave_opener(1200);
        assert!(!p.enter_preview(&b));
        assert!(!p.leave_preview(&b, 1300));
        assert_eq!(p.next_deadline(), Some(1420));
        assert!(p.enter_preview(&c));
        assert!(p.leave_preview(&a, 1300));
        p.advance(2000);
        assert!(p.transient.is_some());
    }
    #[test]
    fn repeated_background_moves_do_not_postpone_close() {
        let mut p = model();
        open(&mut p, 1, 0);
        p.leave_opener(300);
        p.leave_opener(400);
        p.leave_opener(519);
        assert_eq!(p.next_deadline(), Some(520));
        p.advance(520);
        assert!(p.transient.is_none());
    }
    #[test]
    fn stale_actions_follow_identity_never_rendered_slot() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        assert_eq!(p.pin_identity(&a), PinResult::Pinned);
        let b = open(&mut p, 2, 300);
        assert_eq!(p.pin_identity(&a), PinResult::NothingToPin);
        assert!(p.close_identity(&a));
        assert_eq!(p.transient.as_ref().unwrap().read, b);
        assert_eq!(p.pin_identity(&b), PinResult::Pinned);
        assert!(!p.unpin_identity(&a, 700));
        assert_eq!(p.pinned.as_ref().unwrap().read, b);
        assert!(!p.close_identity(&a));
    }
    #[test]
    fn honest_empty_partial_and_error_states() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        assert!(p.complete(&a, Ok(content(""))));
        assert_eq!(
            p.transient.as_ref().unwrap().state,
            State::Ready(content(""))
        );
        let b = open(&mut p, 2, 300);
        assert_eq!(p.transient.as_ref().unwrap().state, State::Loading);
        assert!(p.complete(&b, Err("unavailable".into())));
        assert_eq!(
            p.transient.as_ref().unwrap().state,
            State::Failed("unavailable".into())
        );
        p.invalidate_session();
        assert!(p.transient.is_none());
        assert!(!p.complete(&b, Ok(content("late"))));
    }
}
