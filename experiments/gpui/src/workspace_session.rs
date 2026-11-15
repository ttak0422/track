//! Disposable-fixture UI metadata and explicitly enabled draft checkpoints.
//! One worker owns the protected store and serializes both artifact lifecycles.
use super::*;
use crate::draft_recovery::{
    LocalStatus, OperationKind, Record, RecoveryContext, RecoveryModel, RecoveryToken,
};
use std::path::PathBuf;

pub struct SessionOptions {
    pub fixture: PathBuf,
    pub config: PathBuf,
    pub cache: PathBuf,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum RecoveryStatus {
    Disabled,
    Loading,
    Ready(LocalStatus),
    Failed,
}
#[derive(Default)]
struct RecoveryShared {
    model: Option<RecoveryModel>,
    binding: Option<String>,
    high_water: u64,
    failed: bool,
    frozen: bool,
}
#[derive(Default)]
struct Pending {
    latest: Option<(
        u64,
        Vec<tabs::Tab>,
        navigation::Snapshot,
        String,
        Vec<session_store::ReadingPosition>,
    )>,
    completed: u64,
    error: Option<String>,
    recovery: RecoveryShared,
}
struct Writer {
    wake: Option<std::sync::mpsc::SyncSender<()>>,
    shared: std::sync::Arc<std::sync::Mutex<Pending>>,
    sent: u64,
    #[cfg(test)]
    thread: Option<std::thread::JoinHandle<()>>,
}
impl Writer {
    fn new(
        mut store: session_store::SessionStore,
        recovery_enabled: bool,
        registry: Vec<String>,
    ) -> Self {
        let (wake, receiver) = std::sync::mpsc::sync_channel(1);
        let shared = std::sync::Arc::new(std::sync::Mutex::new(Pending::default()));
        let state = shared.clone();
        let thread = std::thread::spawn(move || {
            // This branch is the only recovery-file entry point. Ordinary launches
            // do not load, stat, inspect or create the artifact.
            let mut recovery = if recovery_enabled {
                match store.load_recovery() {
                    Ok(token) => {
                        let binding = store.recovery_binding().to_owned();
                        let prior = token.record().cloned();
                        let model = RecoveryModel::new(binding.clone(), registry, prior.clone());
                        let mut shared = state.lock().unwrap();
                        match model {
                            Ok(model) => {
                                shared.recovery.high_water =
                                    prior.as_ref().map_or(0, |r| r.revision);
                                shared.recovery.binding = Some(binding);
                                shared.recovery.model = Some(model);
                            }
                            Err(_) => shared.recovery.failed = true,
                        }
                        Some(token)
                    }
                    Err(_) => {
                        state.lock().unwrap().recovery.failed = true;
                        None
                    }
                }
            } else {
                None
            };
            while receiver.recv().is_ok() {
                let snapshot = {
                    let mut shared = state.lock().unwrap();
                    if shared.error.is_none() {
                        shared.latest.take()
                    } else {
                        shared.latest = None;
                        None
                    }
                };
                if let Some((sequence, entries, history, scope, positions)) = snapshot {
                    let result = store.save_with_positions(&entries, &history, &scope, &positions);
                    let mut shared = state.lock().unwrap();
                    shared.completed = sequence;
                    if let Err(error) = result {
                        shared.error = Some(error);
                    }
                }
                // No per-keystroke thread and no filesystem operation holds the
                // model mutex. One active operation and one coalesced latest remain.
                loop {
                    let op = {
                        let mut shared = state.lock().unwrap();
                        if shared.recovery.frozen || shared.recovery.failed {
                            None
                        } else {
                            shared
                                .recovery
                                .model
                                .as_mut()
                                .and_then(RecoveryModel::next_operation)
                        }
                    };
                    let Some(op) = op else {
                        break;
                    };
                    let result = match recovery.as_mut() {
                        Some(token) => match op.kind {
                            OperationKind::Write(record) => {
                                // Model only yields writes after explicit recovery
                                // or from an initially absent artifact.
                                token.resolve();
                                store.checkpoint_recovery(token, record)
                            }
                            OperationKind::Clear => store.clear_recovery_if_present(token),
                        },
                        None => Err("Fixture draft persistence unavailable".into()),
                    };
                    if let Some(model) = state.lock().unwrap().recovery.model.as_mut() {
                        model.complete(op.token, result.is_ok());
                    }
                    // Post-rename/fsync uncertainty never retries automatically.
                    if result.is_err() {
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
    fn notify(&self) {
        if let Some(wake) = &self.wake {
            let _ = wake.try_send(());
        }
    }
    fn enqueue(
        &mut self,
        entries: Vec<tabs::Tab>,
        history: navigation::Snapshot,
        scope: String,
        positions: Vec<session_store::ReadingPosition>,
    ) {
        self.sent += 1;
        self.shared.lock().unwrap().latest = Some((self.sent, entries, history, scope, positions));
        self.notify();
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
    last_scope: String,
    positions: Vec<session_store::ReadingPosition>,
    restored_scope: Option<String>,
    restored_history: Option<navigation::History>,
    pub warning: Option<String>,
    pub safe_home: bool,
    known_vaults: Vec<String>,
    recovery_enabled: bool,
}
impl SessionUi {
    pub fn new(options: Option<SessionOptions>) -> Self {
        Self {
            safe_home: options.is_some(),
            options,
            ..Self::default()
        }
    }
    /// Must be called before restore; path/fixture authorization belongs to startup.
    pub fn enable_recovery(&mut self) {
        if self.worker.is_none() && self.options.is_some() {
            self.recovery_enabled = true;
        }
    }
    pub fn recovery_enabled(&self) -> bool {
        self.recovery_enabled
    }
    pub fn recovery_status(&self) -> RecoveryStatus {
        if !self.recovery_enabled {
            return RecoveryStatus::Disabled;
        }
        let Some(worker) = &self.worker else {
            return if self.options.is_some() {
                RecoveryStatus::Loading
            } else {
                RecoveryStatus::Failed
            };
        };
        let shared = worker.shared.lock().unwrap();
        let state = &shared.recovery;
        if state.failed || state.frozen {
            return RecoveryStatus::Failed;
        }
        match &state.model {
            Some(model) if model.status() == LocalStatus::Failed => RecoveryStatus::Failed,
            Some(model) => RecoveryStatus::Ready(model.status()),
            None => RecoveryStatus::Loading,
        }
    }
    pub fn recovery_ready(&self) -> bool {
        matches!(self.recovery_status(), RecoveryStatus::Ready(_))
    }
    pub fn recovery_record(&self) -> Option<Record> {
        self.worker
            .as_ref()?
            .shared
            .lock()
            .unwrap()
            .recovery
            .model
            .as_ref()?
            .latest()
            .cloned()
    }
    pub fn recovery_binding(&self) -> Option<String> {
        self.worker
            .as_ref()?
            .shared
            .lock()
            .unwrap()
            .recovery
            .binding
            .clone()
    }
    pub fn next_revision(&self) -> Result<u64, String> {
        let worker = self
            .worker
            .as_ref()
            .ok_or("Fixture draft persistence unavailable")?;
        let shared = worker.shared.lock().unwrap();
        if shared.recovery.frozen || shared.recovery.failed || shared.recovery.model.is_none() {
            return Err("Fixture draft persistence unavailable".into());
        }
        shared
            .recovery
            .high_water
            .checked_add(1)
            .ok_or_else(|| "Fixture draft revision exhausted".into())
    }
    pub fn checkpoint(&mut self, record: Record) -> Result<(), String> {
        let worker = self
            .worker
            .as_ref()
            .ok_or("Fixture draft persistence unavailable")?;
        {
            let mut shared = worker.shared.lock().unwrap();
            if shared.recovery.frozen || shared.recovery.failed {
                return Err("Fixture draft persistence unavailable".into());
            }
            let revision = record.revision;
            shared
                .recovery
                .model
                .as_mut()
                .ok_or("Fixture draft persistence unavailable")?
                .checkpoint(record)?;
            shared.recovery.high_water = revision;
        }
        worker.notify();
        Ok(())
    }
    pub fn offer_recovery(&self, context: &RecoveryContext) -> Option<RecoveryToken> {
        let shared = self.worker.as_ref()?.shared.lock().unwrap();
        if shared.recovery.frozen || shared.recovery.failed {
            return None;
        }
        shared.recovery.model.as_ref()?.offer_recovery(context)
    }
    pub fn recover(&mut self, token: &RecoveryToken, context: &RecoveryContext) -> Option<Record> {
        let mut shared = self.worker.as_ref()?.shared.lock().unwrap();
        if shared.recovery.frozen || shared.recovery.failed {
            return None;
        }
        shared.recovery.model.as_mut()?.recover(token, context)
    }
    pub fn discard(&mut self, confirmed: &Record) -> bool {
        self.clear_recovery(confirmed, false)
    }
    pub fn acknowledged(&mut self, exact: &Record) -> bool {
        self.clear_recovery(exact, true)
    }
    fn clear_recovery(&mut self, record: &Record, saved: bool) -> bool {
        let Some(worker) = &self.worker else {
            return false;
        };
        let accepted = {
            let mut shared = worker.shared.lock().unwrap();
            if shared.recovery.frozen || shared.recovery.failed {
                return false;
            }
            let Some(model) = shared.recovery.model.as_mut() else {
                return false;
            };
            if model.status() == LocalStatus::Failed {
                return false;
            }
            if saved {
                model.acknowledge_saved(record)
            } else {
                model.discard(record)
            }
        };
        if accepted {
            worker.notify();
        }
        accepted
    }
    fn session_available(&self) -> bool {
        self.worker
            .as_ref()
            .is_some_and(|w| w.shared.lock().unwrap().error.is_none())
    }
    /// Metadata failures must not destroy the draft worker or its pending bytes.
    fn disable_session(&mut self) {
        if self.recovery_enabled {
            if let Some(worker) = &self.worker {
                let mut shared = worker.shared.lock().unwrap();
                shared.latest = None;
                if shared.error.is_none() {
                    shared.error = Some("Fixture metadata unavailable".into());
                }
            }
        } else {
            self.worker = None;
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
                self.positions = store.reading_positions.clone();
                self.restored_scope = store.restored_scope.take();
                self.last_scope = self.restored_scope.clone().unwrap_or_default();
                self.last_history = history.snapshot();
                self.restored_history = Some(history);
                let mut tabs = tabs::Tabs::restore(entries);
                // Home is the safe active route; relative MRU order of restored notes is retained.
                tabs.open_home();
                self.last = tabs.entries().to_vec();
                self.worker = Some(Writer::new(
                    store,
                    self.recovery_enabled,
                    self.known_vaults.clone(),
                ));
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
        if !startup_current {
            self.positions.clear();
        }
        self.restored_history.take().filter(|_| startup_current)
    }
    /// Canonical launch-name aliases mean the empty working scope. Normalize the
    /// in-memory baseline too, so startup never rewrites a valid saved snapshot.
    pub fn normalize_scope_alias(&mut self, launch_name: &str) {
        if self.restored_scope.as_deref() == Some(launch_name) {
            self.restored_scope = Some(String::new());
            self.last_scope.clear();
        }
    }
    pub fn take_restored_scope(&mut self, startup_current: bool) -> Option<String> {
        if !startup_current {
            self.positions.clear();
        }
        self.restored_scope.take().filter(|_| startup_current)
    }
    #[cfg(test)]
    pub fn sync(&mut self, tabs: &tabs::Tabs, history: &navigation::History) {
        self.sync_with_scope(tabs, history, "");
    }
    pub fn sync_with_scope(
        &mut self,
        tabs: &tabs::Tabs,
        history: &navigation::History,
        scope: &str,
    ) {
        self.poll_warning();
        if !self.session_available() {
            return;
        }
        if scope.len() > 1024
            || scope.contains('\0')
            || !self.known_vaults.iter().any(|v| v == scope)
        {
            self.warning =
                Some("Tab persistence disabled: unknown or invalid working scope.".into());
            self.disable_session();
            return;
        }
        let history = history.snapshot();
        if self.last == tabs.entries() && self.last_history == history && self.last_scope == scope {
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
            self.disable_session();
            return;
        }
        let Some(worker) = self.worker.as_mut() else {
            return;
        };
        // Bounded latest snapshot, not a queue of disk writes. No filesystem work here.
        self.positions
            .retain(|p| tabs.entries().iter().any(|tab| tab.key == p.key()));
        worker.enqueue(
            tabs.entries().to_vec(),
            history.clone(),
            scope.to_owned(),
            self.positions.clone(),
        );
        self.last_scope = scope.to_owned();
        self.last_history = history;
        self.last = tabs.entries().to_vec();
    }
    pub fn positions_enabled(&self) -> bool {
        self.safe_home && self.session_available()
    }
    pub fn reading_position(&self, key: &tabs::TabKey) -> Option<&session_store::ReadingPosition> {
        self.positions.iter().find(|p| &p.key() == key)
    }
    /// Returns a queued sequence, not a durability claim. Call completion after polling errors.
    pub fn save_reading_position(
        &mut self,
        position: session_store::ReadingPosition,
        tabs: &tabs::Tabs,
        history: &navigation::History,
        scope: &str,
    ) -> Result<u64, String> {
        self.poll_warning();
        if !self.positions_enabled() {
            return Err("Fixture position persistence unavailable".into());
        }
        if !position.valid()
            || !self.known_vaults.contains(&position.vault)
            || !tabs.entries().iter().any(|tab| tab.key == position.key())
        {
            self.warning = Some("Tab persistence disabled: invalid reading position".into());
            self.disable_session();
            return Err(self.warning.clone().unwrap());
        }
        self.sync_with_scope(tabs, history, scope);
        if !self.session_available() {
            return Err("Fixture position persistence unavailable".into());
        }
        let Some(worker) = self.worker.as_mut() else {
            return Err("Fixture position persistence unavailable".into());
        };
        self.positions.retain(|p| p.key() != position.key());
        self.positions.push(position);
        worker.enqueue(
            self.last.clone(),
            self.last_history.clone(),
            self.last_scope.clone(),
            self.positions.clone(),
        );
        Ok(worker.sent)
    }
    pub fn position_save_completed(&self, sequence: u64) -> bool {
        self.worker.as_ref().is_some_and(|w| {
            let pending = w.shared.lock().unwrap();
            pending.error.is_none() && pending.completed >= sequence
        })
    }
    pub fn poll_warning(&mut self) {
        let error = self
            .worker
            .as_ref()
            .and_then(|worker| worker.shared.lock().unwrap().error.clone());
        if let Some(error) = error {
            self.warning = Some(format!("Tab persistence disabled: {error}"));
            self.disable_session();
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
            if let Some(worker) = &self.worker {
                worker.shared.lock().unwrap().recovery.frozen = true;
            }
            self.disable_session();
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
    fn canonical_launch_alias_normalizes_without_rewriting_startup() {
        let fixture = Fixture::new();
        let mut ui = fixture.ui();
        let tabs = ui.restore(registry()).unwrap();
        let history = ui.take_restored_history(true).unwrap();
        ui.sync_with_scope(&tabs, &history, "work");
        ui.flush();
        let before = fixture.bytes();
        drop(ui);
        let mut ui = fixture.ui();
        let tabs = ui.restore(registry()).unwrap();
        let history = ui.take_restored_history(true).unwrap();
        ui.normalize_scope_alias("work");
        assert_eq!(ui.take_restored_scope(true).as_deref(), Some(""));
        ui.sync_with_scope(&tabs, &history, "");
        ui.flush();
        assert_eq!(fixture.bytes(), before);
    }

    #[test]
    fn scope_only_save_restores_without_startup_write_or_route_change() {
        let fixture = Fixture::new();
        let mut ui = fixture.ui();
        let tabs = ui.restore(registry()).unwrap();
        let history = ui.take_restored_history(true).unwrap();
        ui.sync_with_scope(&tabs, &history, "work");
        ui.flush();
        let saved = fixture.bytes();
        assert_eq!(
            serde_json::from_slice::<serde_json::Value>(&saved).unwrap()["working_scope"],
            "work"
        );
        drop(ui);
        let mut ui = fixture.ui();
        let tabs = ui.restore(registry()).unwrap();
        let history = ui.take_restored_history(true).unwrap();
        assert_eq!(tabs.active().unwrap().key, tabs::TabKey::Home);
        assert_eq!(ui.take_restored_scope(true).as_deref(), Some("work"));
        assert!(ui.take_restored_scope(true).is_none());
        ui.sync_with_scope(&tabs, &history, "work");
        ui.flush();
        assert_eq!(fixture.bytes(), saved);
        ui.sync_with_scope(&tabs, &history, "");
        ui.flush();
        assert_ne!(fixture.bytes(), saved);
        let before = fixture.bytes();
        ui.sync_with_scope(&tabs, &history, "unregistered");
        assert!(ui.warning.is_some());
        assert!(ui.worker.is_none());
        assert_eq!(fixture.bytes(), before);
    }

    #[test]
    fn newer_startup_selection_discards_saved_scope_and_preserves_current() {
        let fixture = Fixture::new();
        let mut ui = fixture.ui();
        let tabs = ui.restore(registry()).unwrap();
        let history = ui.take_restored_history(true).unwrap();
        ui.sync_with_scope(&tabs, &history, "work");
        ui.flush();
        drop(ui);
        let mut ui = fixture.ui();
        let tabs = ui.restore(registry()).unwrap();
        assert!(ui.take_restored_scope(false).is_none());
        assert!(ui.take_restored_scope(true).is_none());
        // Accepted launch-scope choice wins even if saved tabs remain the same.
        ui.sync_with_scope(&tabs, &history, "");
        ui.flush();
        assert_eq!(
            serde_json::from_slice::<serde_json::Value>(&fixture.bytes()).unwrap()["working_scope"],
            ""
        );
    }

    #[test]
    fn disabled_has_no_paths_or_store_and_no_session_io() {
        let mut ui = SessionUi::new(None);
        assert!(ui.restore(Err("must not be consulted".into())).is_none());
        let mut tabs = tabs::Tabs::default();
        tabs.open_graph();
        ui.sync(&tabs, &navigation::History::default());
        ui.sync_with_scope(&tabs, &navigation::History::default(), "arbitrary");
        assert!(ui.take_restored_scope(true).is_none());
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
    fn ready_startup_registry_keeps_session_and_recovery_available() {
        let fixture = Fixture::new();
        let mut ui = fixture.ui();
        ui.enable_recovery();
        let mut initial = serde_json::json!({
            "active": {"name": "work", "path": "/fixture"},
            "vaults": [{"name": "work", "path": "/fixture", "active": true}],
            "unavailable": [{"error": "initializing"}], "storage_state": "initializing"
        });
        let ready = serde_json::json!({
            "active": {"name": "work", "path": "/fixture"},
            "vaults": [{"name": "work", "path": "/fixture", "active": true}],
            "unavailable": []
        });
        let registry = crate::workspace_live::startup_registry(
            Some("ready"),
            &mut initial,
            || false,
            || Ok(ready),
        );
        let tabs = ui.restore(registry).unwrap();
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
        while !ui.recovery_ready() {
            assert!(std::time::Instant::now() < deadline);
            assert!(!matches!(ui.recovery_status(), RecoveryStatus::Failed));
            std::thread::sleep(std::time::Duration::from_millis(2));
        }
        ui.registry_changed(crate::shell::session_registry(&initial).unwrap());
        ui.sync(&tabs, &navigation::History::default());
        ui.flush();
        assert!(ui.session_available());
        assert!(ui.recovery_ready());
        assert!(ui.warning.is_none());
        drop(ui);
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
    #[test]
    fn explicit_position_only_queue_preserves_history_scope_and_startup_race_discards() {
        let f = Fixture::new();
        let mut ui = f.ui();
        let mut tabs = ui.restore(registry()).unwrap();
        let mut history = ui.take_restored_history(true).unwrap();
        tabs.open_note("work", 7, "A");
        history.record(tabs.active().map(|t| t.key.clone()));
        ui.sync_with_scope(&tabs, &history, "work");
        ui.flush();
        let before = f.bytes();
        let position = session_store::ReadingPosition {
            vault: "work".into(),
            note_id: 7,
            etag: "revision".into(),
            body_bytes: 12,
            renderer: "checkpoint-v1:clean-test".into(),
            block_index: 0,
            geometry: session_store::PositionGeometry {
                viewport_width: 800.,
                viewport_height: 600.,
                reader_width: 700.,
                reader_height: 500.,
                scale: 1.,
            },
        };
        let sequence = ui
            .save_reading_position(position.clone(), &tabs, &history, "work")
            .unwrap();
        ui.flush();
        assert!(ui.position_save_completed(sequence));
        assert_ne!(f.bytes(), before);
        tabs.open_graph();
        history.record(Some(tabs::TabKey::Graph));
        ui.sync_with_scope(&tabs, &history, "");
        ui.flush();
        drop(ui);
        let before = f.bytes();
        let mut ui = f.ui();
        let _ = ui.restore(registry()).unwrap();
        assert_eq!(ui.reading_position(&position.key()), Some(&position));
        assert_eq!(f.bytes(), before);
        ui.take_restored_history(false);
        assert!(ui.reading_position(&position.key()).is_none());
        assert_eq!(f.bytes(), before);
        let mut normal = SessionUi::new(None);
        assert!(!normal.positions_enabled());
        assert!(
            normal
                .save_reading_position(position, &tabs, &history, "work")
                .is_err()
        );
        assert!(normal.positions.is_empty());
        assert!(normal.worker.is_none());
    }
    fn wait_recovery(ui: &SessionUi, expected: RecoveryStatus) {
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
        while ui.recovery_status() != expected {
            assert!(
                std::time::Instant::now() < deadline,
                "recovery worker did not reach expected state"
            );
            std::thread::sleep(std::time::Duration::from_millis(2));
        }
    }
    fn draft(ui: &SessionUi, revision: u64) -> Record {
        Record {
            schema: 1,
            binding: ui.recovery_binding().unwrap(),
            scope: "work".into(),
            vault: "work".into(),
            note_id: 7,
            kind: "note".into(),
            base_etag: "base".into(),
            body: "日本語 e\u{301}\n".into(),
            revision,
            title: Some("fixture".into()),
        }
    }
    #[test]
    fn enabled_worker_checkpoints_coalesces_restarts_without_install_or_write() {
        let f = Fixture::new();
        let path = f.0.join("cache/draft-recovery.json");
        let mut ui = f.ui();
        ui.enable_recovery();
        let tabs = ui.restore(registry()).unwrap();
        assert_eq!(tabs.active().unwrap().key, tabs::TabKey::Home);
        wait_recovery(&ui, RecoveryStatus::Ready(LocalStatus::Empty));
        assert!(!path.exists());
        let mut latest = draft(&ui, 1);
        for revision in 1..=30 {
            latest.revision = revision;
            latest.body = format!("日本語 e\u{301}\n{revision}");
            ui.checkpoint(latest.clone()).unwrap();
        }
        wait_recovery(&ui, RecoveryStatus::Ready(LocalStatus::SavedLocally));
        assert_eq!(ui.next_revision().unwrap(), 31);
        let before = std::fs::read(&path).unwrap();
        assert!(Record::decode(&before, &latest.binding, &registry().unwrap()).unwrap() == latest);
        assert!(!f.0.join("cache/track-native-tabs-v1.json").exists());
        drop(ui);
        let mut restarted = f.ui();
        restarted.enable_recovery();
        let tabs = restarted.restore(registry()).unwrap();
        wait_recovery(&restarted, RecoveryStatus::Ready(LocalStatus::Available));
        assert_eq!(tabs.active().unwrap().key, tabs::TabKey::Home);
        assert!(restarted.recovery_record().unwrap() == latest);
        assert_eq!(std::fs::read(&path).unwrap(), before);
        let mut newer = latest.clone();
        newer.revision += 1;
        assert!(restarted.checkpoint(newer).is_err());
        let context = RecoveryContext {
            binding: latest.binding.clone(),
            scope: latest.scope.clone(),
            vault: latest.vault.clone(),
            note_id: latest.note_id,
            kind: latest.kind.clone(),
            etag: latest.base_etag.clone(),
            request: 1,
            launch: 2,
            storage: 3,
            editing_ready: true,
            storage_ready: true,
            draft_busy: false,
        };
        let offer = restarted.offer_recovery(&context).unwrap();
        assert!(restarted.recover(&offer, &context).unwrap() == latest);
        assert_eq!(std::fs::read(&path).unwrap(), before);
        assert!(restarted.acknowledged(&latest));
        wait_recovery(&restarted, RecoveryStatus::Ready(LocalStatus::Empty));
        assert!(!path.exists());
        assert_eq!(restarted.next_revision().unwrap(), 31);
    }
    #[test]
    fn metadata_failure_keeps_recovery_worker_but_registry_change_freezes_it() {
        let f = Fixture::new();
        let mut ui = f.ui();
        ui.enable_recovery();
        let mut tabs = ui.restore(registry()).unwrap();
        wait_recovery(&ui, RecoveryStatus::Ready(LocalStatus::Empty));
        tabs.open_graph();
        ui.sync(&tabs, &navigation::History::default());
        ui.flush();
        std::fs::write(
            f.0.join("cache/track-native-tabs-v1.json"),
            b"foreign metadata",
        )
        .unwrap();
        tabs.open_home();
        ui.sync(&tabs, &navigation::History::default());
        ui.flush();
        ui.poll_warning();
        assert!(ui.warning.is_some());
        assert!(ui.worker.is_some());
        let first = draft(&ui, 1);
        ui.checkpoint(first.clone()).unwrap();
        wait_recovery(&ui, RecoveryStatus::Ready(LocalStatus::SavedLocally));
        assert!(ui.recovery_record().unwrap() == first);
        let before = std::fs::read(f.0.join("cache/draft-recovery.json")).unwrap();
        ui.registry_changed(vec!["".into()]);
        assert_eq!(ui.recovery_status(), RecoveryStatus::Failed);
        let mut next = first;
        next.revision = 2;
        assert!(ui.checkpoint(next).is_err());
        assert_eq!(
            std::fs::read(f.0.join("cache/draft-recovery.json")).unwrap(),
            before
        );
    }
    #[test]
    fn recovery_cas_failure_retains_latest_and_never_retries() {
        let f = Fixture::new();
        let mut ui = f.ui();
        ui.enable_recovery();
        ui.restore(registry()).unwrap();
        wait_recovery(&ui, RecoveryStatus::Ready(LocalStatus::Empty));
        let mut record = draft(&ui, 1);
        ui.checkpoint(record.clone()).unwrap();
        wait_recovery(&ui, RecoveryStatus::Ready(LocalStatus::SavedLocally));
        let path = f.0.join("cache/draft-recovery.json");
        std::fs::write(&path, b"foreign artifact").unwrap();
        record.revision = 2;
        ui.checkpoint(record.clone()).unwrap();
        wait_recovery(&ui, RecoveryStatus::Failed);
        assert!(ui.recovery_record().unwrap() == record);
        record.revision = 3;
        ui.checkpoint(record.clone()).unwrap();
        assert_eq!(ui.recovery_status(), RecoveryStatus::Failed);
        drop(ui); // drains wake, but must not retry an uncertain operation
        assert_eq!(std::fs::read(&path).unwrap(), b"foreign artifact");
        let mut restart = f.ui();
        restart.enable_recovery();
        restart.restore(registry()).unwrap();
        wait_recovery(&restart, RecoveryStatus::Failed);
        assert!(restart.recovery_record().is_none());
    }
    #[test]
    fn disabled_worker_never_inspects_recovery_artifact() {
        let f = Fixture::new();
        let path = f.0.join("cache/draft-recovery.json");
        std::os::unix::fs::symlink(f.0.join("config"), &path).unwrap();
        let mut ui = f.ui();
        let mut tabs = ui.restore(registry()).unwrap();
        tabs.open_graph();
        ui.sync(&tabs, &navigation::History::default());
        ui.flush();
        assert_eq!(ui.recovery_status(), RecoveryStatus::Disabled);
        assert!(ui.recovery_binding().is_none());
        assert!(ui.recovery_record().is_none());
        assert!(path.symlink_metadata().unwrap().file_type().is_symlink());
        assert_eq!(std::fs::read(f.0.join("config")).unwrap(), b"{}\n");
    }
}
