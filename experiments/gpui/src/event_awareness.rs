//! Notification acknowledgement only. Never authorizes reload, navigation or writes.
use crate::live_events::Snapshot;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Ticket {
    launch: u64,
    change: u64,
    data: u64,
    gaps: u64,
}
#[derive(Clone, Copy)]
pub struct Notice {
    pub message: &'static str,
    pub dismiss: Option<Ticket>,
}
#[derive(Default)]
pub struct State {
    current: Option<(u64, Snapshot)>,
    acknowledged: Option<Ticket>,
}
impl State {
    pub fn observe(&mut self, current: Option<(u64, Snapshot)>) -> bool {
        if self.current == current {
            return false;
        }
        if self.current.as_ref().map(|(launch, _)| launch)
            != current.as_ref().map(|(launch, _)| launch)
        {
            self.acknowledged = None;
        }
        self.current = current;
        true
    }
    pub fn notice(&self) -> Option<Notice> {
        let (launch, snapshot) = self.current?;
        let ticket = Ticket {
            launch,
            change: snapshot.change,
            data: snapshot.data,
            gaps: snapshot.gaps,
        };
        if !snapshot.connected && snapshot.gaps > 0 {
            return Some(Notice {
                message: "Live updates are interrupted. Reload for current data.",
                dismiss: None,
            });
        }
        let previous = self.acknowledged.unwrap_or(Ticket {
            launch,
            change: 0,
            data: 0,
            gaps: 0,
        });
        let message = if ticket.gaps > previous.gaps {
            "Live observation had a gap; changes may have been missed. Reload for current data."
        } else if ticket.change > previous.change && ticket.data > previous.data {
            "Note and data changes observed. Reload to refresh the current page."
        } else if ticket.change > previous.change {
            "Note changes observed. Reload to refresh the current page."
        } else if ticket.data > previous.data {
            "Data changes observed. Reload to refresh supported content."
        } else {
            return None;
        };
        Some(Notice {
            message,
            dismiss: Some(ticket),
        })
    }
    /// Dismiss acknowledges the notice, not freshness. Captured counters prevent
    /// an event arriving between paint and click from being acknowledged unseen.
    pub fn dismiss(&mut self, ticket: Ticket) -> bool {
        let Some((launch, snapshot)) = self.current else {
            return false;
        };
        if ticket.launch != launch
            || !snapshot.connected
            || ticket.change > snapshot.change
            || ticket.data > snapshot.data
            || ticket.gaps > snapshot.gaps
        {
            return false;
        }
        let previous = self.acknowledged.unwrap_or(Ticket {
            launch,
            change: 0,
            data: 0,
            gaps: 0,
        });
        self.acknowledged = Some(Ticket {
            launch,
            change: previous.change.max(ticket.change),
            data: previous.data.max(ticket.data),
            gaps: previous.gaps.max(ticket.gaps),
        });
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn snapshot(change: u64, data: u64, gaps: u64, connected: bool) -> Snapshot {
        Snapshot {
            follow: 0,
            change,
            data,
            gaps,
            connected,
        }
    }
    #[test]
    fn explicit_dismiss_is_not_freshness_and_new_events_survive_click_race() {
        let mut state = State::default();
        state.observe(Some((1, snapshot(1, 0, 0, true))));
        let ticket = state.notice().unwrap().dismiss.unwrap();
        state.observe(Some((1, snapshot(2, 1, 0, true))));
        assert!(state.dismiss(ticket));
        assert!(state.notice().unwrap().message.starts_with("Note and data"));
        let next = state.notice().unwrap().dismiss.unwrap();
        assert!(state.dismiss(next));
        assert!(state.notice().is_none());
        assert!(!state.observe(Some((1, snapshot(2, 1, 0, true)))));
    }
    #[test]
    fn disconnect_stays_visible_and_reconnection_does_not_clear_gap() {
        let mut state = State::default();
        state.observe(Some((1, snapshot(0, 0, 0, false))));
        assert!(state.notice().is_none()); // initial connection is not a failure
        state.observe(Some((1, snapshot(1, 0, 0, true))));
        let ticket = state.notice().unwrap().dismiss.unwrap();
        state.observe(Some((1, snapshot(1, 0, 1, false))));
        assert!(state.notice().unwrap().dismiss.is_none());
        assert!(!state.dismiss(ticket));
        state.observe(Some((1, snapshot(1, 0, 1, true))));
        assert!(state.notice().unwrap().message.contains("gap"));
        assert!(state.dismiss(ticket)); // old click still cannot dismiss the new gap
        assert!(state.notice().is_some());
    }
    #[test]
    fn replaced_launch_rejects_old_controls_and_ordinary_snapshot_mode_is_empty() {
        let mut state = State::default();
        state.observe(Some((1, snapshot(1, 0, 0, true))));
        let ticket = state.notice().unwrap().dismiss.unwrap();
        state.observe(Some((2, snapshot(1, 0, 0, true))));
        assert!(!state.dismiss(ticket));
        assert!(state.notice().is_some());
        assert!(state.observe(None));
        assert!(state.notice().is_none());
        assert!(!state.dismiss(ticket));
    }
}

#[cfg(test)]
mod follow_is_not_change_tests {
    use super::*;
    #[test]
    fn follow_invalidations_never_create_change_notices() {
        let mut state = State::default();
        state.observe(Some((
            1,
            Snapshot {
                follow: 100,
                connected: true,
                ..Default::default()
            },
        )));
        assert!(state.notice().is_none());
    }
}
