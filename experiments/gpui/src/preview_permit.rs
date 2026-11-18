//! One process-wide bound for graph and reader preview HTTP workers.
//! Move the permit into the worker, not its UI completion task: canceling a
//! preview must not release capacity while its blocking request is still running.
use std::sync::atomic::{AtomicUsize, Ordering};

const MAX_WORKERS: usize = 2;
static ACTIVE: AtomicUsize = AtomicUsize::new(0);

/// Deliberately neither Clone nor Copy. Drop releases exactly one worker slot.
pub(crate) struct Permit {
    active: &'static AtomicUsize,
}
impl Permit {
    /// Nonblocking admission. Exhaustion is an explicit error, not a queued retry.
    pub(crate) fn acquire() -> Option<Self> {
        Self::acquire_from(&ACTIVE)
    }

    fn acquire_from(active: &'static AtomicUsize) -> Option<Self> {
        active
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |count| {
                (count < MAX_WORKERS).then_some(count + 1)
            })
            .ok()
            .map(|_| Self { active })
    }
}
impl Drop for Permit {
    fn drop(&mut self) {
        self.active.fetch_sub(1, Ordering::AcqRel);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Each test has its own counter; parallel Rust tests cannot consume one
    // another's capacity or interfere with the application-global counter.
    #[test]
    fn two_workers_only_and_drop_releases_exactly_one_slot() {
        static COUNT: AtomicUsize = AtomicUsize::new(0);
        let first = Permit::acquire_from(&COUNT).unwrap();
        let second = Permit::acquire_from(&COUNT).unwrap();
        assert!(Permit::acquire_from(&COUNT).is_none());
        assert_eq!(COUNT.load(Ordering::Acquire), 2);
        drop(first);
        let replacement = Permit::acquire_from(&COUNT).unwrap();
        assert!(Permit::acquire_from(&COUNT).is_none());
        drop(second);
        drop(replacement);
        assert_eq!(COUNT.load(Ordering::Acquire), 0);
    }

    #[test]
    fn failure_and_unwind_release_worker_capacity() {
        static COUNT: AtomicUsize = AtomicUsize::new(0);
        let failed = (|| -> Result<(), ()> {
            let _permit = Permit::acquire_from(&COUNT).unwrap();
            Err(())
        })();
        assert!(failed.is_err());
        assert_eq!(COUNT.load(Ordering::Acquire), 0);
        let panicked = std::panic::catch_unwind(|| {
            let _permit = Permit::acquire_from(&COUNT).unwrap();
            panic!("simulated worker failure");
        });
        assert!(panicked.is_err());
        assert_eq!(COUNT.load(Ordering::Acquire), 0);
    }

    #[test]
    fn canceled_ui_does_not_release_running_worker() {
        static COUNT: AtomicUsize = AtomicUsize::new(0);
        let first = Permit::acquire_from(&COUNT).unwrap();
        let second = Permit::acquire_from(&COUNT).unwrap();
        let (finish, wait) = std::sync::mpsc::channel();
        let (completion, ui) = std::sync::mpsc::channel();
        let worker = std::thread::spawn(move || {
            let _permit = first;
            wait.recv().unwrap();
            let _ = completion.send(());
        });
        drop(ui); // UI cancellation does not own the HTTP worker's permit.
        assert!(Permit::acquire_from(&COUNT).is_none());
        finish.send(()).unwrap();
        worker.join().unwrap();
        let replacement = Permit::acquire_from(&COUNT).unwrap();
        assert!(Permit::acquire_from(&COUNT).is_none());
        drop(second);
        drop(replacement);
        assert_eq!(COUNT.load(Ordering::Acquire), 0);
    }
}
