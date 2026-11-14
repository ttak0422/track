//! Full-graph preview lifecycle foundation; not wired to the local reader graph.
//! Clock values are monotonic milliseconds supplied by the UI. Reads are emitted
//! only by `advance`; transport/UI must never infer a target from current selection.
#![allow(dead_code)]

pub const MAX_PINS: usize = 4;

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
/// Session-only pinned card rectangle in workspace-local pixels.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct CardRect {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
}
impl CardRect {
    pub fn bounded(self, viewport: (f32, f32)) -> Option<Self> {
        if ![
            self.x,
            self.y,
            self.width,
            self.height,
            viewport.0,
            viewport.1,
        ]
        .iter()
        .all(|v| v.is_finite())
            || viewport.0 <= 0.
            || viewport.1 <= 0.
        {
            return None;
        }
        let left = 12_f32.min(viewport.0 / 4.);
        let top = 160_f32.min((viewport.1 - 13.).max(0.));
        let available_w = (viewport.0 - left * 2.).max(1.);
        let available_h = (viewport.1 - top - 12.).max(1.);
        let width = self.width.clamp(280_f32.min(available_w), available_w);
        let height = self.height.clamp(180_f32.min(available_h), available_h);
        Some(Self {
            x: self.x.clamp(left, (viewport.0 - left - width).max(left)),
            y: self.y.clamp(top, (viewport.1 - 12. - height).max(top)),
            width,
            height,
        })
    }
}
#[derive(Clone, Debug)]
struct CardGesture {
    identity: Read,
    start: (f32, f32),
    rect: CardRect,
    resize: bool,
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
    pub placement_slot: Option<usize>,
    pub rect: Option<CardRect>,
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
    pub pinned: Vec<Preview>,
    pub raised: Option<Read>,
    gesture: Option<CardGesture>,
}
impl Previews {
    pub fn geometry_identity(&self) -> Option<&Read> {
        self.gesture.as_ref().map(|g| &g.identity)
    }
    pub fn geometry_active(&self) -> bool {
        self.gesture.is_some()
    }
    pub fn end_geometry_for(&mut self, identity: &Read) {
        if self.geometry_identity() == Some(identity) {
            self.end_geometry();
        }
    }
    pub fn end_geometry(&mut self) {
        self.gesture = None;
    }
    pub fn begin_geometry(
        &mut self,
        identity: &Read,
        start: (f32, f32),
        rect: CardRect,
        resize: bool,
        viewport: (f32, f32),
    ) -> bool {
        if !start.0.is_finite()
            || !start.1.is_finite()
            || !self.pinned.iter().any(|p| &p.read == identity)
        {
            return false;
        }
        let Some(rect) = rect.bounded(viewport) else {
            return false;
        };
        self.raised = Some(identity.clone());
        self.gesture = Some(CardGesture {
            identity: identity.clone(),
            start,
            rect,
            resize,
        });
        true
    }
    pub fn update_geometry(
        &mut self,
        identity: &Read,
        pointer: (f32, f32),
        viewport: (f32, f32),
        pressed: bool,
    ) -> bool {
        let Some(gesture) = self.gesture.as_ref() else {
            return false;
        };
        if &gesture.identity != identity {
            return false;
        }
        if !pressed {
            self.end_geometry();
            return false;
        }
        let Some(preview) = self.pinned.iter_mut().find(|p| p.read == gesture.identity) else {
            self.gesture = None;
            return false;
        };
        if !pointer.0.is_finite() || !pointer.1.is_finite() {
            return false;
        }
        let (dx, dy) = (pointer.0 - gesture.start.0, pointer.1 - gesture.start.1);
        let mut rect = gesture.rect;
        if gesture.resize {
            rect.width += dx;
            rect.height += dy;
        } else {
            rect.x += dx;
            rect.y += dy;
        }
        let Some(mut rect) = rect.bounded(viewport) else {
            return false;
        };
        if gesture.resize {
            // Resizing keeps the top-left fixed and stops at the viewport edge.
            let origin = gesture.rect.bounded(viewport).unwrap();
            rect.x = origin.x;
            rect.y = origin.y;
            rect.width = rect.width.min((viewport.0 - rect.x - 12.).max(1.));
            rect.height = rect.height.min((viewport.1 - rect.y - 12.).max(1.));
        }
        preview.rect = Some(rect);
        true
    }
    pub fn set_scope(&mut self, scope: Scope) {
        if self.scope.as_ref() == Some(&scope) {
            return;
        }
        if self.scope.as_ref().map(|s| s.session) != Some(scope.session) {
            self.pinned.clear();
            self.raised = None;
        }
        self.end_geometry();
        self.clear_transient();
        self.scope = Some(scope);
    }
    /// Call immediately when the owned server fails or is replaced.
    pub fn invalidate_session(&mut self) {
        self.end_geometry();
        self.clear_transient();
        self.pinned.clear();
        self.raised = None;
        self.scope = None;
    }
    pub fn navigate(&mut self) {
        self.end_geometry();
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
        self.raised = None;
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
        if self.pinned.iter().any(|p| p.read.target == target) {
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
        if let Some(preview) = self.pinned.iter_mut().find(|p| &p.read == identity) {
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
        if let Some(preview) = self.pinned.iter_mut().find(|p| &p.read == identity) {
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
            placement_slot: None,
            rect: None,
            state: State::Loading,
            pointer_inside: false,
        });
        Some(read)
    }
    pub fn complete(&mut self, read: &Read, result: Result<Content, String>) -> bool {
        if self.scope.as_ref().map(|s| s.session) != Some(read.scope.session) {
            return false;
        }
        let target = if self.pinned.iter().any(|p| &p.read == read) {
            self.pinned.iter_mut().find(|p| &p.read == read)
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
        if self
            .gesture
            .as_ref()
            .is_some_and(|g| &g.identity == identity)
        {
            self.end_geometry();
        }
        if self.raised.as_ref() == Some(identity) {
            self.raised = None;
        }
        if self.transient.as_ref().is_some_and(|p| &p.read == identity) {
            self.close_transient();
            return true;
        }
        if let Some(index) = self.pinned.iter().position(|p| &p.read == identity) {
            let removed = self.pinned.remove(index);
            if self.opener.as_ref() == Some(&removed.read.target) {
                self.opener = None;
            }
            return true;
        }
        false
    }
    /// Raising changes paint order only; content and captured read stay intact.
    pub fn raise_identity(&mut self, identity: &Read) -> bool {
        if !self.pinned.iter().any(|p| &p.read == identity) {
            return false;
        }
        self.raised = Some(identity.clone());
        true
    }
    pub fn pin_identity(&mut self, identity: &Read) -> PinResult {
        if !self.transient.as_ref().is_some_and(|p| &p.read == identity) {
            return PinResult::NothingToPin;
        }
        self.pin()
    }
    pub fn unpin_identity(&mut self, identity: &Read, now: u64) -> bool {
        let Some(index) = self.pinned.iter().position(|p| &p.read == identity) else {
            return false;
        };
        // Explicit Unpin replaces only the disposable transient, never another pin.
        let mut preview = self.pinned.remove(index);
        preview.rect = None;
        if self
            .gesture
            .as_ref()
            .is_some_and(|g| &g.identity == identity)
        {
            self.end_geometry();
        }
        // Unpin must expose the new transient even if a different pin was raised.
        self.raised = None;
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
        true
    }
    pub fn pin(&mut self) -> PinResult {
        if self.transient.is_none() {
            return PinResult::NothingToPin;
        }
        if self.pinned.len() >= MAX_PINS {
            return PinResult::CapacityReached;
        }
        let slot = (0..MAX_PINS)
            .find(|slot| !self.pinned.iter().any(|p| p.placement_slot == Some(*slot)))
            .unwrap();
        let mut preview = self.transient.take().unwrap();
        preview.placement_slot = Some(slot);
        self.pinned.push(preview);
        self.close_at = None;
        PinResult::Pinned
    }
    pub fn close_transient(&mut self) {
        self.clear_transient();
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
        assert!(!p.pinned.is_empty());
        p.set_scope(scope(1, 3, "fixture"));
        for id in 2..=4 {
            open(&mut p, id, 1000 + id as u64 * 300);
            assert_eq!(p.pin(), PinResult::Pinned);
        }
        let extra = open(&mut p, 5, 2500);
        assert_eq!(p.pin(), PinResult::CapacityReached);
        assert_eq!(p.transient.as_ref().unwrap().read, extra);
        assert_eq!(p.pinned.len(), MAX_PINS);
        assert_eq!(p.pinned[0].read, a);
        p.unpin_identity(&a, 2800);
        p.advance(3019);
        assert!(p.transient.is_some());
        p.advance(3020);
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
        assert!(p.pinned.is_empty());
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
        p.unpin_identity(&a, 1100);
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
        p.unpin_identity(&a, 400);
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
        assert!(!p.pinned.is_empty());
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
        assert_eq!(p.pinned[0].read, b);
        assert!(!p.close_identity(&a));
    }
    #[test]
    fn multiple_pins_finish_out_of_order_and_actions_keep_other_identities() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        p.pin_identity(&a);
        let b = open(&mut p, 2, 300);
        p.pin_identity(&b);
        let c = open(&mut p, 3, 600);
        p.pin_identity(&c);
        assert!(p.complete(&c, Ok(content("C"))));
        assert!(p.complete(&a, Ok(content("A"))));
        let before = p.pinned.clone();
        assert!(p.raise_identity(&a));
        assert_eq!(p.raised.as_ref(), Some(&a));
        assert_eq!(p.pinned, before); // no content, token, anchor or placement movement
        assert!(p.close_identity(&b));
        assert!(!p.complete(&b, Ok(content("closed B"))));
        assert!(!p.raise_identity(&b));
        assert_eq!(p.pinned[0], before[0]);
        assert_eq!(p.pinned[1], before[2]);
        assert!(p.unpin_identity(&a, 1000));
        assert_eq!(p.transient.as_ref().unwrap().read, a);
        assert_eq!(p.pinned, [before[2].clone()]);
        assert!(!p.unpin_identity(&b, 1100));
    }
    #[test]
    fn same_id_in_two_vaults_remains_distinct_and_session_clears_all() {
        let mut p = model();
        let a = open(&mut p, 7, 0);
        p.pin_identity(&a);
        p.set_scope(scope(1, 2, "other"));
        p.hover(target("other", 7), (20., 30.), 300);
        let b = p.advance(560).unwrap();
        p.pin_identity(&b);
        assert_eq!(p.pinned.len(), 2);
        p.navigate();
        p.hover(target("other", 7), (90., 90.), 600);
        assert!(p.advance(1000).is_none());
        assert!(p.complete(&b, Ok(content("other"))));
        assert!(p.complete(&a, Ok(content("fixture"))));
        p.raise_identity(&a);
        p.set_scope(scope(2, 3, "other"));
        assert!(p.pinned.is_empty() && p.transient.is_none() && p.raised.is_none());
        assert!(!p.complete(&a, Ok(content("late"))));
        assert!(!p.complete(&b, Ok(content("late"))));
    }
    #[test]
    fn unpin_exposes_transient_even_when_another_pin_was_raised() {
        let mut p = model();
        let a = open(&mut p, 1, 0);
        p.pin_identity(&a);
        let b = open(&mut p, 2, 300);
        p.pin_identity(&b);
        let survivor = p.pinned[0].clone();
        p.raise_identity(&a);
        assert!(p.unpin_identity(&b, 700));
        assert!(p.raised.is_none()); // renderer's normal order puts transient last
        assert_eq!(p.transient.as_ref().unwrap().read, b);
        assert_eq!(p.pinned, [survivor]);
    }

    #[test]
    fn capacity_preserves_all_cards_and_reuses_only_vacated_placement() {
        let mut p = model();
        let mut reads = Vec::new();
        for id in 1..=4 {
            let read = open(&mut p, id, id as u64 * 300);
            p.pin_identity(&read);
            reads.push(read);
        }
        let extra = open(&mut p, 5, 1600);
        let before = p.pinned.clone();
        let transient = p.transient.clone();
        assert_eq!(p.pin_identity(&extra), PinResult::CapacityReached);
        assert_eq!(p.pinned, before);
        assert_eq!(p.transient, transient);
        p.close_identity(&reads[1]);
        assert_eq!(p.pin_identity(&extra), PinResult::Pinned);
        assert_eq!(
            p.pinned.last().unwrap().placement_slot,
            before[1].placement_slot
        );
        assert_eq!(p.pinned[0], before[0]);
        assert_eq!(p.pinned[1], before[2]);
        p.invalidate_session();
        assert!(p.pinned.is_empty() && p.transient.is_none());
        for read in reads.iter().chain([&extra]) {
            assert!(!p.complete(read, Ok(content("late"))));
        }
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

#[cfg(test)]
mod geometry_tests {
    use super::*;
    fn pin(p: &mut Previews, id: i64) -> Read {
        let now = id as u64 * 1000;
        p.hover(
            Target {
                vault: "fixture".into(),
                note_id: id,
            },
            (0., 0.),
            now,
        );
        let read = p.advance(now + OPEN_DELAY_MS).unwrap();
        assert_eq!(p.pin_identity(&read), PinResult::Pinned);
        read
    }
    fn model() -> Previews {
        let mut p = Previews::default();
        p.set_scope(Scope {
            session: 1,
            graph: 1,
            vault: "fixture".into(),
        });
        p
    }
    fn rect() -> CardRect {
        CardRect {
            x: 20.,
            y: 170.,
            width: 380.,
            height: 250.,
        }
    }
    #[test]
    fn move_resize_clamp_keep_headers_and_actions_reachable() {
        let mut p = model();
        let read = pin(&mut p, 1);
        for resize in [false, true] {
            for pointer in [(-10000., -10000.), (10000., 10000.)] {
                assert!(p.begin_geometry(&read, (0., 0.), rect(), resize, (680., 480.)));
                assert!(p.update_geometry(&read, pointer, (680., 480.), true));
                let r = p.pinned[0].rect.unwrap();
                assert!(r.x >= 12. && r.y >= 160.);
                assert!(r.x + r.width <= 668. && r.y + r.height <= 468.);
                assert!(r.width >= 280. && r.height >= 180.);
                if resize {
                    assert_eq!((r.x, r.y), (20., 170.));
                }
            }
        }
        let r = rect().bounded((300., 200.)).unwrap();
        assert!(r.width > 0. && r.height > 0. && r.y + r.height <= 188.);
        assert!(rect().bounded((f32::NAN, 480.)).is_none());
        assert!(!p.update_geometry(&read, (f32::INFINITY, 0.), (680., 480.), true));
    }
    #[test]
    fn geometry_survives_raise_other_close_and_navigation_but_not_invalidation() {
        let mut p = model();
        let a = pin(&mut p, 1);
        let b = pin(&mut p, 2);
        p.begin_geometry(&a, (0., 0.), rect(), false, (1000., 800.));
        p.update_geometry(&a, (50., 70.), (1000., 800.), true);
        let saved = p.pinned[0].rect;
        p.raise_identity(&b);
        p.close_identity(&b);
        p.navigate();
        assert_eq!(p.pinned[0].rect, saved);
        assert!(!p.geometry_active());
        p.invalidate_session();
        assert!(p.pinned.is_empty() && !p.geometry_active());
    }
    #[test]
    fn stale_identity_and_released_button_cannot_move_cards() {
        let mut p = model();
        let a = pin(&mut p, 1);
        p.begin_geometry(&a, (0., 0.), rect(), false, (680., 480.));
        p.close_identity(&a);
        let b = pin(&mut p, 2);
        p.begin_geometry(&b, (0., 0.), rect(), false, (680., 480.));
        assert!(!p.update_geometry(&a, (20., 20.), (680., 480.), true));
        p.end_geometry_for(&a);
        assert!(p.geometry_active());
        assert!(p.pinned[0].rect.is_none());
        assert!(!p.update_geometry(&b, (20., 20.), (680., 480.), false));
        assert!(!p.geometry_active());
        assert!(p.pinned[0].rect.is_none());
        p.begin_geometry(&b, (0., 0.), rect(), true, (680., 480.));
        p.unpin_identity(&b, 3000);
        assert!(!p.geometry_active());
        assert!(!p.begin_geometry(&b, (0., 0.), rect(), true, (680., 480.)));
        assert!(p.transient.as_ref().unwrap().rect.is_none());
    }
}
