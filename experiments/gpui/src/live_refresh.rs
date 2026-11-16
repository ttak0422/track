//! Current-note refresh scheduling only. Counters invalidate, never identify notes.
const DEBOUNCE_MS: u64 = 500;
const MIN_INTERVAL_MS: u64 = 2000;
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) struct Signal {
    pub change: u64,
    pub gaps: u64,
    pub connected: bool,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Ticket<O> {
    pub owner: O,
    pub generation: u64,
    pub serial: u64,
    pub signal: Signal,
}
pub(crate) struct Model<O> {
    pub enabled: bool,
    owner: Option<O>,
    generation: u64,
    serial: u64,
    signal: Signal,
    pending: Option<u64>,
    inflight: Option<Ticket<O>>,
    last_start: Option<u64>,
    safe: bool,
}
impl<O> Default for Model<O> {
    fn default() -> Self {
        Self {
            enabled: false,
            owner: None,
            generation: 0,
            serial: 0,
            signal: Signal::default(),
            pending: None,
            inflight: None,
            last_start: None,
            safe: false,
        }
    }
}
impl<O: Clone + PartialEq> Model<O> {
    pub fn enable(&mut self, owner: O, signal: Signal, now: u64) {
        self.generation = self.generation.wrapping_add(1);
        self.enabled = true;
        self.owner = Some(owner);
        self.signal = signal;
        self.pending = Some(now);
        self.safe = false;
    }
    pub fn disable(&mut self) {
        self.generation = self.generation.wrapping_add(1);
        self.enabled = false;
        self.owner = None;
        self.pending = None;
        self.safe = false;
    }
    pub fn generation(&self) -> u64 {
        self.generation
    }
    pub fn busy(&self) -> bool {
        self.inflight.is_some()
    }
    pub fn pending(&self) -> bool {
        self.pending.is_some()
    }
    pub fn retry(&mut self, now: u64) {
        if self.enabled {
            self.generation = self.generation.wrapping_add(1);
            self.pending = Some(now);
        }
    }
    pub fn observe(&mut self, owner: Option<O>, signal: Signal, now: u64, safe: bool) {
        if !self.enabled {
            return;
        }
        if self.owner != owner {
            self.generation = self.generation.wrapping_add(1);
            self.owner = owner;
            self.pending = self.owner.as_ref().map(|_| now);
            self.signal = signal;
            self.safe = false;
        }
        if signal.change == u64::MAX || signal.gaps == u64::MAX {
            self.disable();
            return;
        }
        if signal.change != self.signal.change || signal.gaps != self.signal.gaps {
            self.pending = Some(now);
        }
        self.signal = signal;
        if self.safe && !safe {
            self.generation = self.generation.wrapping_add(1);
        }
        self.safe = safe;
    }
    pub fn claim(&mut self, now: u64) -> Option<Ticket<O>> {
        if !self.enabled || !self.safe || !self.signal.connected || self.inflight.is_some() {
            return None;
        }
        let since = self.pending?;
        if now.saturating_sub(since) < DEBOUNCE_MS
            || self
                .last_start
                .is_some_and(|t| now.saturating_sub(t) < MIN_INTERVAL_MS)
        {
            return None;
        }
        let owner = self.owner.clone()?;
        self.serial = self.serial.wrapping_add(1);
        let ticket = Ticket {
            owner,
            generation: self.generation,
            serial: self.serial,
            signal: self.signal,
        };
        self.inflight = Some(ticket.clone());
        self.pending = None;
        self.last_start = Some(now);
        Some(ticket)
    }
    /// Releasing a physical worker never erases a newer dirty watermark.
    /// Unsafe completion is discarded; resumption requires a fresh read.
    pub fn complete(
        &mut self,
        ticket: &Ticket<O>,
        owner: Option<&O>,
        safe: bool,
        now: u64,
    ) -> bool {
        if self.inflight.as_ref() != Some(ticket) {
            return false;
        }
        self.inflight = None;
        let same = self.enabled
            && self.owner.as_ref() == Some(&ticket.owner)
            && owner == Some(&ticket.owner);
        let accepted = same
            && self.generation == ticket.generation
            && safe
            && self.safe
            && self.signal.connected;
        if same && !accepted && self.pending.is_none() {
            self.pending = Some(now);
        }
        accepted
    }
    /// A successful own acceptance may change the source request without losing
    /// an event which arrived while that read was in flight.
    pub fn accepted_owner(&mut self, old: &O, new: O) {
        if self.owner.as_ref() == Some(old) {
            self.owner = Some(new);
        }
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn signal(n: u64) -> Signal {
        Signal {
            change: n,
            gaps: 0,
            connected: true,
        }
    }
    fn enabled() -> Model<u64> {
        let mut m = Model::default();
        m.enable(1, signal(0), 0);
        m.observe(Some(1), signal(0), 0, true);
        m
    }
    #[test]
    fn debounce_coalesces_and_one_worker_preserves_new_event() {
        let mut m = enabled();
        assert!(m.claim(499).is_none());
        let t = m.claim(500).unwrap();
        m.observe(Some(1), signal(2), 700, true);
        assert!(m.claim(5000).is_none());
        assert!(m.complete(&t, Some(&1), true, 800));
        assert!(m.pending());
        assert!(m.claim(2499).is_none());
        assert!(m.claim(2500).is_some());
    }
    #[test]
    fn failure_is_one_attempt_until_new_signal_or_retry() {
        let mut m = enabled();
        let t = m.claim(500).unwrap();
        assert!(m.complete(&t, Some(&1), true, 600));
        assert!(m.claim(9999).is_none());
        m.retry(9999);
        assert!(m.claim(10499).is_some());
    }
    #[test]
    fn blocked_during_read_requires_fresh_read_even_after_safe_return() {
        let mut m = enabled();
        let t = m.claim(500).unwrap();
        m.observe(Some(1), signal(0), 600, false);
        m.observe(Some(1), signal(0), 700, true);
        assert!(!m.complete(&t, Some(&1), true, 800));
        assert!(m.pending());
        assert!(m.claim(2500).is_some());
    }
    #[test]
    fn route_aba_and_off_on_do_not_accept_old_completion() {
        let mut m = enabled();
        let t = m.claim(500).unwrap();
        m.observe(Some(2), signal(0), 600, true);
        m.observe(Some(1), signal(0), 700, true);
        assert!(!m.complete(&t, Some(&1), true, 800));
        let mut m = enabled();
        let t = m.claim(500).unwrap();
        m.disable();
        m.enable(1, signal(0), 600);
        m.observe(Some(1), signal(0), 600, true);
        assert!(m.claim(9999).is_none());
        assert!(!m.complete(&t, Some(&1), true, 9999));
        assert!(m.claim(10500).is_some());
    }
    #[test]
    fn gaps_wait_for_reconnection_and_own_accept_keeps_dirty() {
        let mut m = enabled();
        let t = m.claim(500).unwrap();
        let gap = Signal {
            change: 0,
            gaps: 1,
            connected: false,
        };
        m.observe(Some(1), gap, 600, true);
        assert!(!m.complete(&t, Some(&1), true, 700));
        assert!(m.claim(5000).is_none());
        m.observe(
            Some(1),
            Signal {
                connected: true,
                ..gap
            },
            5000,
            true,
        );
        let t = m.claim(5000).unwrap();
        m.observe(
            Some(1),
            Signal {
                change: 1,
                connected: true,
                ..gap
            },
            5100,
            true,
        );
        assert!(m.complete(&t, Some(&1), true, 5200));
        m.accepted_owner(&1, 3);
        assert!(m.pending());
        assert!(m.claim(7000).is_some());
    }
    #[test]
    fn blocked_without_worker_keeps_event_then_failure_stops_retrying() {
        let mut m = enabled();
        m.observe(Some(1), signal(1), 100, false);
        assert!(m.claim(5000).is_none());
        m.observe(Some(1), signal(1), 5000, true);
        let t = m.claim(5000).unwrap();
        assert!(m.complete(&t, Some(&1), true, 5100));
        assert!(m.claim(10000).is_none());
    }
    #[test]
    fn saturation_disables_but_releases_physical_old_worker() {
        let mut m = enabled();
        let t = m.claim(500).unwrap();
        m.observe(Some(1), signal(u64::MAX), 600, true);
        assert!(!m.enabled);
        assert!(m.busy());
        assert!(!m.complete(&t, Some(&1), true, 700));
        assert!(!m.busy());
    }
}

/// Changed Rendered snapshots always reset their projection; only apply at the measured top.
/// Source metadata can retain the exact body/tasks and existing scroll handle.
pub(crate) fn may_replace(same_display_inputs: bool, rendered: bool, measured_top: bool) -> bool {
    measured_top || (same_display_inputs && !rendered)
}
#[cfg(test)]
mod application_tests {
    use super::*;
    #[test]
    fn rendered_metadata_change_does_not_reset_reading_position() {
        assert!(!may_replace(true, true, false));
        assert!(!may_replace(false, true, false));
        assert!(may_replace(true, true, true));
        assert!(may_replace(true, false, false));
        assert!(!may_replace(false, false, false));
        assert!(may_replace(false, false, true));
    }
}
