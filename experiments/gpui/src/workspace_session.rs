//! Explicit disposable-fixture tab/history metadata only. Never loads or saves note bodies.
use super::*;
use std::path::PathBuf;

pub struct SessionOptions {
    pub fixture: PathBuf,
    pub config: PathBuf,
    pub cache: PathBuf,
}
#[derive(Default)]
struct Pending {
    latest: Option<(u64, Vec<tabs::Tab>, navigation::Snapshot)>,
    completed: u64,
    error: Option<String>,
}
struct Writer {
    wake: Option<std::sync::mpsc::SyncSender<()>>,
    shared: std::sync::Arc<std::sync::Mutex<Pending>>,
    sent: u64,
    #[cfg(test)]
    thread: Option<std::thread::JoinHandle<()>>,
}
impl Writer {
    fn new(mut store: session_store::SessionStore) -> Self {
        let (wake, receiver) = std::sync::mpsc::sync_channel(1);
        let shared = std::sync::Arc::new(std::sync::Mutex::new(Pending::default()));
        let state = shared.clone();
        let thread = std::thread::spawn(move || {
            while receiver.recv().is_ok() {
                let snapshot = state.lock().unwrap().latest.take();
                if let Some((sequence, entries, history)) = snapshot {
                    let result = store.save_with_history(&entries, &history);
                    let mut state = state.lock().unwrap();
                    state.completed = sequence;
                    if let Err(error) = result {
                        state.error = Some(error);
                        break;
                    }
                }
            }
        });
        #[cfg(not(test))]
        let _ = thread;
        Self {
            wake: Some(wake),
            shared,
            sent: 0,
            #[cfg(test)]
            thread: Some(thread),
        }
    }
    fn enqueue(&mut self, entries: Vec<tabs::Tab>, history: navigation::Snapshot) {
        self.sent += 1;
        self.shared.lock().unwrap().latest = Some((self.sent, entries, history));
        // Full means a wake already exists; that wake consumes the latest snapshot.
        let _ = self.wake.as_ref().unwrap().try_send(());
    }
}
impl Drop for Writer {
    fn drop(&mut self) {
        self.wake.take();
        // UI never waits for disk on quit. Tests join to inspect deterministic artifacts.
        #[cfg(test)]
        if let Some(thread) = self.thread.take() {
            thread.join().unwrap();
        }
    }
}
#[derive(Default)]
pub struct SessionUi {
    pub options: Option<SessionOptions>,
    worker: Option<Writer>,
    last: Vec<tabs::Tab>,
    last_history: navigation::Snapshot,
    restored_history: Option<navigation::History>,
    pub warning: Option<String>,
    pub safe_home: bool,
    known_vaults: Vec<String>,
}
impl SessionUi {
    pub fn new(options: Option<SessionOptions>) -> Self {
        Self {
            safe_home: options.is_some(),
            options,
            ..Self::default()
        }
    }
    /// None is a strict zero-session-I/O path, including no environment/path probes.
    pub fn restore(&mut self, registry: Result<Vec<String>, String>) -> Option<tabs::Tabs> {
        let options = self.options.take()?;
        let result = registry.and_then(|known| {
            self.known_vaults = known.clone();
            session_store::SessionStore::open(
                &options.fixture,
                &options.config,
                &options.cache,
                known,
            )
        });
        match result {
            Ok((mut store, entries)) => {
                let history = store
                    .restored_history
                    .take()
                    .and_then(navigation::History::restore_home)
                    .unwrap_or_default();
                self.last_history = history.snapshot();
                self.restored_history = Some(history);
                let mut tabs = tabs::Tabs::restore(entries);
                // Home is the safe active route; relative MRU order of restored notes is retained.
                tabs.open_home();
                self.last = tabs.entries().to_vec();
                self.worker = Some(Writer::new(store));
                Some(tabs)
            }
            Err(error) => {
                self.warning = Some(format!("Tab persistence disabled: {error}"));
                None
            }
        }
    }
    /// Consume once; startup navigation races discard, never install, saved routes.
    pub fn take_restored_history(&mut self, startup_current: bool) -> Option<navigation::History> {
        self.restored_history.take().filter(|_| startup_current)
    }
    pub fn sync(&mut self, tabs: &tabs::Tabs, history: &navigation::History) {
        self.poll_warning();
        if self.worker.is_none() {
            return;
        }
        let history = history.snapshot();
        if self.last == tabs.entries() && self.last_history == history {
            return;
        }
        if self.worker.is_some()
            && (tabs.entries().len() > 128
                || tabs.entries().iter().any(|tab| {
                    tab.title.len() > 2048
                        || matches!(&tab.key, tabs::TabKey::Note {vault, ..} if vault.len() > 1024)
                }))
        {
            self.warning =
                Some("Tab persistence disabled: metadata exceeds bounded snapshot limits.".into());
            self.worker = None;
            return;
        }
        let Some(worker) = self.worker.as_mut() else {
            return;
        };
        // Bounded latest snapshot, not a queue of disk writes. No filesystem work here.
        worker.enqueue(tabs.entries().to_vec(), history.clone());
        self.last_history = history;
        self.last = tabs.entries().to_vec();
    }
    pub fn poll_warning(&mut self) {
        let error = self
            .worker
            .as_ref()
            .and_then(|worker| worker.shared.lock().unwrap().error.clone());
        if let Some(error) = error {
            self.warning = Some(format!("Tab persistence disabled: {error}"));
            self.worker = None;
        }
    }
    #[cfg(test)]
    fn flush(&self) {
        if let Some(worker) = &self.worker {
            let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
            while worker.shared.lock().unwrap().completed < worker.sent {
                assert!(std::time::Instant::now() < deadline);
                std::thread::sleep(std::time::Duration::from_millis(2));
            }
        }
    }
    pub fn registry_changed(&mut self, mut known: Vec<String>) {
        if !self.safe_home {
            return;
        }
        known.sort();
        known.dedup();
        if known != self.known_vaults {
            self.worker = None;
            self.known_vaults = known;
            self.warning = Some("Tab persistence disabled: vault registry changed; restart with matching fixture configuration.".into());
        }
    }
    pub fn permits(&self, vault: &str) -> bool {
        !self.safe_home || self.known_vaults.iter().any(|known| known == vault)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;
    struct Fixture(PathBuf);
    impl Fixture {
        fn new() -> Self {
            static NEXT: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
            let root = std::env::temp_dir().canonicalize().unwrap().join(format!(
                "track-tabs-ui-{}-{}",
                std::process::id(),
                NEXT.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
            ));
            std::fs::create_dir(&root).unwrap();
            for name in ["vault", "cache"] {
                std::fs::create_dir(root.join(name)).unwrap();
            }
            std::fs::set_permissions(root.join("cache"), std::fs::Permissions::from_mode(0o700))
                .unwrap();
            std::fs::write(
                root.join("vault/.track-native-edit-fixture"),
                "disposable-fixture-only\n",
            )
            .unwrap();
            std::fs::write(root.join("config"), "{}\n").unwrap();
            Self(root)
        }
        fn ui(&self) -> SessionUi {
            SessionUi::new(Some(SessionOptions {
                fixture: self.0.join("vault"),
                config: self.0.join("config"),
                cache: self.0.join("cache"),
            }))
        }
        fn bytes(&self) -> Vec<u8> {
            std::fs::read(self.0.join("cache/track-native-tabs-v1.json")).unwrap()
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    fn registry() -> Result<Vec<String>, String> {
        Ok(vec!["".into(), "work".into()])
    }
    #[test]
    fn disabled_has_no_paths_or_store_and_no_session_io() {
        let mut ui = SessionUi::new(None);
        assert!(ui.restore(Err("must not be consulted".into())).is_none());
        let mut tabs = tabs::Tabs::default();
        tabs.open_graph();
        ui.sync(&tabs, &navigation::History::default());
        ui.flush();
        assert!(ui.warning.is_none());
        assert!(ui.worker.is_none());
        assert!(!ui.safe_home);
    }
    #[test]
    fn restart_history_requires_explicit_back_and_cursor_only_change_is_saved() {
        let fixture = Fixture::new();
        let mut ui = fixture.ui();
        let mut tabs = ui.restore(registry()).unwrap();
        let mut history = ui.restored_history.take().unwrap();
        tabs.open_note("work", 7, "A");
        history.record(tabs.active().map(|t| t.key.clone()));
        tabs.open_graph();
        history.record(Some(tabs::TabKey::Graph));
        ui.sync(&tabs, &history);
        ui.flush();
        let saved = fixture.bytes();
        drop(ui);
        let mut restarted = fixture.ui();
        let tabs = restarted.restore(registry()).unwrap();
        let mut history = restarted.restored_history.take().unwrap();
        assert_eq!(tabs.active().unwrap().key, tabs::TabKey::Home);
        assert_eq!(history.snapshot().routes.last(), Some(&tabs::TabKey::Home));
        assert!(history.peek(true).is_none());
        restarted.sync(&tabs, &history);
        restarted.flush();
        assert_eq!(fixture.bytes(), saved); // Safe Home normalization does not rewrite startup.
        let back = history.peek(false).unwrap();
        assert_eq!(back.route, Some(tabs::TabKey::Graph));
        assert!(history.commit(&back));
        restarted.sync(&tabs, &history);
        restarted.flush();
        let moved = fixture.bytes();
        assert_ne!(moved, saved);
        let json: serde_json::Value = serde_json::from_slice(&moved).unwrap();
        assert_eq!(json["history"]["cursor"], history.snapshot().cursor);
        // The same tabs with a different accepted cursor must still persist.
        assert!(history.commit(&history.peek(true).unwrap()));
        restarted.sync(&tabs, &history);
        restarted.flush();
        assert_ne!(fixture.bytes(), moved);
    }
    #[test]
    fn startup_race_discards_saved_history_without_replacing_current_routes() {
        let mut ui = SessionUi::default();
        let mut saved = navigation::History::default();
        saved.record(Some(tabs::TabKey::Note {
            vault: "work".into(),
            note_id: 7,
        }));
        ui.restored_history = Some(saved);
        let mut current = navigation::History::default();
        current.record(Some(tabs::TabKey::Graph));
        let before = current.clone();
        if let Some(restored) = ui.take_restored_history(false) {
            current = restored;
        }
        assert_eq!(current, before);
        assert!(ui.take_restored_history(true).is_none());
    }
    #[test]
    fn startup_reads_without_writing_and_selects_safe_home() {
        let fixture = Fixture::new();
        let mut ui = fixture.ui();
        let mut tabs = ui.restore(registry()).unwrap();
        tabs.open_note("work", 7, "日本語");
        ui.sync(&tabs, &navigation::History::default());
        ui.flush();
        let bytes = fixture.bytes();
        drop(ui);
        let mut restarted = fixture.ui();
        let restored = restarted.restore(registry()).unwrap();
        assert_eq!(restored.active().unwrap().key, tabs::TabKey::Home);
        assert_eq!(restored.entries()[1].title, "日本語");
        restarted.sync(&restored, &navigation::History::default());
        restarted.flush();
        assert_eq!(fixture.bytes(), bytes);
        assert!(!restarted.permits("unknown"));
        restarted.registry_changed(vec!["".into()]);
        assert!(!restarted.permits("work"));
        assert!(restarted.worker.is_none());
    }
    #[test]
    fn bounded_worker_coalesces_and_reports_conflicts_without_retrying() {
        let fixture = Fixture::new();
        let mut ui = fixture.ui();
        let mut tabs = ui.restore(registry()).unwrap();
        let key = tabs.open_note("work", 7, "first");
        for n in 0..200 {
            tabs.update_title(&key, format!("title {n}"));
            ui.sync(&tabs, &navigation::History::default());
        }
        ui.flush();
        let saved: serde_json::Value = serde_json::from_slice(&fixture.bytes()).unwrap();
        assert_eq!(saved["tabs"][0]["title"], "title 199");
        std::fs::write(
            fixture.0.join("cache/track-native-tabs-v1.json"),
            b"corrupt external replacement",
        )
        .unwrap();
        tabs.open_graph();
        ui.sync(&tabs, &navigation::History::default());
        ui.flush();
        ui.poll_warning();
        assert!(ui.warning.as_deref().unwrap().contains("conflict"));
        assert!(ui.worker.is_none());
        tabs.open_home();
        ui.sync(&tabs, &navigation::History::default());
        assert_eq!(fixture.bytes(), b"corrupt external replacement");
    }
    #[test]
    fn cancel_pending_and_failed_save_preserve_snapshot_then_discard_allows_navigation() {
        let fixture = Fixture::new();
        let mut ui = fixture.ui();
        let mut tabs = ui.restore(registry()).unwrap();
        let mut history = ui.restored_history.take().unwrap();
        tabs.open_note("work", 7, "Original");
        history.record(tabs.active().map(|t| t.key.clone()));
        ui.sync(&tabs, &history);
        ui.flush();
        let before = fixture.bytes();
        let storage: live::Storage = serde_json::from_value(
            serde_json::json!({"phase":"ready","read_only":false,"verified":1,"generation":2}),
        )
        .unwrap();
        let source: live::NoteResponse = serde_json::from_value(serde_json::json!({"note":{"note_id":7,"vault":"work","title":"Original","path":"unused","file_kind":"note","body":"private baseline","etag":"private etag","read_only":false}})).unwrap();
        let mut edit = editing::EditSession::from_note(1, &storage, &source).unwrap();
        edit.replace_text("private unsaved draft".into()).unwrap();
        assert!(matches!(
            edit.choose_leave(editing::LeaveChoice::Cancel, 1, &storage)
                .unwrap(),
            editing::LeaveAction::Stay
        ));
        ui.sync(&tabs, &history);
        ui.flush();
        assert_eq!(fixture.bytes(), before);
        let editing::LeaveAction::Save(save) = edit
            .choose_leave(editing::LeaveChoice::Save, 1, &storage)
            .unwrap()
        else {
            panic!()
        };
        assert_eq!(edit.leave_prompt(), editing::LeavePrompt::WaitForSave);
        ui.sync(&tabs, &history);
        ui.flush();
        assert_eq!(fixture.bytes(), before);
        assert_eq!(
            edit.finish_save(save.attempt, 1, 2, Err(live::Error::Stale)),
            editing::SaveOutcome::Retained
        );
        ui.sync(&tabs, &history);
        ui.flush();
        assert_eq!(fixture.bytes(), before);
        assert!(matches!(
            edit.choose_leave(editing::LeaveChoice::Discard, 1, &storage)
                .unwrap(),
            editing::LeaveAction::Leave
        ));
        tabs.open_graph();
        history.record(Some(tabs::TabKey::Graph));
        ui.sync(&tabs, &history);
        ui.flush();
        assert_ne!(fixture.bytes(), before);
        assert!(
            !String::from_utf8(fixture.bytes())
                .unwrap()
                .contains("private")
        );
    }
}
