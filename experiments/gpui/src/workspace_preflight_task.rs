//! Test-only asynchronous preview correlation, never an access capability.
use crate::workspace_read_request::{Decision, EventStamp, RequestDialog, WorkspaceRequest};
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
#[derive(Clone)]
pub struct Ticket {
    identity: Arc<()>,
    stamp: EventStamp,
    request: WorkspaceRequest,
    canceled: Arc<AtomicBool>,
}
impl Ticket {
    pub fn canceled(&self) -> &Arc<AtomicBool> {
        &self.canceled
    }
    fn matches(&self, other: &Self) -> bool {
        Arc::ptr_eq(&self.identity, &other.identity)
    }
    fn current(&self, dialog: &RequestDialog) -> bool {
        !self.canceled.load(Ordering::Acquire)
            && self.stamp == dialog.stamp()
            && self.request == *dialog.request()
            && dialog.decision() == Decision::PreviewConfirmed
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Phase {
    Idle,
    Running,
    Completed,
    Cancelled,
}
pub struct PreviewFlight {
    ticket: Option<Ticket>,
    phase: Phase,
    used: bool,
}
impl PreviewFlight {
    pub fn new() -> Self {
        Self {
            ticket: None,
            phase: Phase::Idle,
            used: false,
        }
    }
    pub fn phase(&self) -> Phase {
        self.phase
    }
    /// Pure UI correlation only. No filesystem checks or process work here.
    pub fn start(&mut self, dialog: &RequestDialog) -> Option<Ticket> {
        if self.used || dialog.decision() != Decision::PreviewConfirmed {
            return None;
        }
        self.used = true;
        let ticket = Ticket {
            identity: Arc::new(()),
            stamp: dialog.stamp(),
            request: dialog.request().clone(),
            canceled: Arc::new(AtomicBool::new(false)),
        };
        self.ticket = Some(ticket.clone());
        self.phase = Phase::Running;
        Some(ticket)
    }
    pub fn cancel(&mut self) {
        if let Some(ticket) = &self.ticket {
            ticket.canceled.store(true, Ordering::Release);
        }
        self.used = true;
        self.phase = Phase::Cancelled;
    }
    pub fn accept(&mut self, ticket: &Ticket, dialog: &RequestDialog) -> bool {
        if self.phase != Phase::Running
            || !self.ticket.as_ref().is_some_and(|own| own.matches(ticket))
        {
            return false;
        }
        if !ticket.current(dialog) {
            self.cancel();
            return false;
        }
        self.phase = Phase::Completed;
        true
    }
}
impl Drop for PreviewFlight {
    fn drop(&mut self) {
        self.cancel();
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::workspace_read_request::Event;
    fn dialog() -> RequestDialog {
        let mut d = RequestDialog::new(
            WorkspaceRequest::new("".into(), "/fixture/active".into(), vec![]).unwrap(),
        );
        d.apply(&d.stamp(), Event::ExplicitConfirm);
        d
    }
    #[test]
    fn one_worker_and_only_matching_current_completion() {
        let d = dialog();
        let mut flight = PreviewFlight::new();
        let t = flight.start(&d).unwrap();
        assert!(flight.start(&d).is_none());
        let mut other = PreviewFlight::new();
        let wrong = other.start(&d).unwrap();
        assert!(!flight.accept(&wrong, &d));
        assert!(flight.accept(&t, &d));
        assert!(!flight.accept(&t, &d));
        assert_eq!(flight.phase(), Phase::Completed);
    }
    #[test]
    fn deterministic_inflight_cancel_replace_drop_discard_old_results() {
        // A bounded channel handshake, not sleeps or scheduler assumptions.
        for scenario in 0..4 {
            let mut d = dialog();
            let mut flight = Some(PreviewFlight::new());
            let t = flight.as_mut().unwrap().start(&d).unwrap();
            let worker = t.clone();
            let (ready_tx, ready_rx) = std::sync::mpsc::channel();
            let (go_tx, go_rx) = std::sync::mpsc::channel();
            let task = std::thread::spawn(move || {
                ready_tx.send(()).unwrap();
                go_rx
                    .recv_timeout(std::time::Duration::from_secs(3))
                    .unwrap();
                worker
            });
            ready_rx
                .recv_timeout(std::time::Duration::from_secs(3))
                .unwrap();
            match scenario {
                0 => flight.as_mut().unwrap().cancel(),
                1 => {
                    flight.as_mut().unwrap().cancel();
                    d.replace(
                        WorkspaceRequest::new("".into(), "/fixture/replaced".into(), vec![])
                            .unwrap(),
                    )
                    .unwrap();
                }
                2 => {
                    drop(flight.take());
                }
                _ => {
                    d = dialog();
                }
            }
            go_tx.send(()).unwrap();
            let completed = task.join().unwrap();
            if let Some(mut flight) = flight {
                assert!(!flight.accept(&completed, &d));
                assert!(flight.start(&d).is_none());
            } else {
                assert!(completed.canceled.load(Ordering::Acquire));
            }
        }
    }
    #[test]
    fn cancellation_after_worker_completion_before_ui_delivery_discards_result() {
        let d = dialog();
        let mut flight = PreviewFlight::new();
        let ticket = flight.start(&d).unwrap();
        let finished = std::thread::spawn(move || ticket).join().unwrap();
        flight.cancel();
        assert!(!flight.accept(&finished, &d));
        assert_eq!(flight.phase(), Phase::Cancelled);
    }
    #[test]
    fn pending_or_cancel_before_start_never_starts() {
        let d = RequestDialog::new(
            WorkspaceRequest::new("".into(), "/fixture/active".into(), vec![]).unwrap(),
        );
        let mut f = PreviewFlight::new();
        assert!(f.start(&d).is_none());
        f.cancel();
        assert!(f.start(&dialog()).is_none());
    }
}
