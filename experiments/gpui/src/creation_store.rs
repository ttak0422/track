//! One durable creation intent, separate from draft/tab artifacts and locks.
//! Uses the same descriptor-relative, no-follow, CAS and atomic replace helpers.
//! Pending means "may have been dispatched", including a crash before POST.
use super::*;

const RECEIPT: &str = "track-native-create-v1.json";
const RECEIPT_LOCK: &str = "track-native-create-v1.lock";
const RECEIPT_OWNER: &str = "track-gpui-fixture-create";
const RECEIPT_LIMIT: u64 = 64 * 1024;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "state", deny_unknown_fields)]
pub enum CreationOutcome {
    Pending,
    Created { note_id: i64, title: String },
    Rejected { status: u16 },
    NotSent,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct CreationRecord {
    owner: String,
    schema: u32,
    binding: String,
    pub revision: u64,
    pub title: String,
    pub outcome: CreationOutcome,
}
impl CreationRecord {
    fn validate(&self, expected: &str) -> Result<(), String> {
        if self.owner != RECEIPT_OWNER
            || self.schema != 1
            || self.binding != expected
            || self.revision == 0
            || !valid_title(&self.title)
        {
            return Err(receipt_error("invalid or foreign receipt"));
        }
        match &self.outcome {
            CreationOutcome::Created { note_id, title }
                if *note_id <= 0 || *note_id > 9_007_199_254_740_991 || title != &self.title =>
            {
                return Err(receipt_error("invalid created identity"));
            }
            CreationOutcome::Rejected { status }
                if !matches!(status, 400 | 403 | 404 | 405 | 409) =>
            {
                return Err(receipt_error("invalid definitive rejection"));
            }
            _ => {}
        }
        Ok(())
    }
}
fn valid_title(title: &str) -> bool {
    !title.is_empty() && title.len() <= 8192 && title.trim() == title && !title.contains('\0')
}
fn receipt_error(reason: impl std::fmt::Display) -> String {
    format!("Creation receipt unavailable; no automatic retry: {reason}")
}
fn receipt_binding(fixture: &Path, config: &Path, cache: &Path) -> Result<String, String> {
    // Empty launch wire scope only; registry changes are unrelated to this receipt.
    let base = binding(fixture, config, cache, &[String::new()])?;
    let root = path_file(fixture, true)?;
    let marker = child(&root, ".track-native-edit-fixture", libc::O_RDONLY)?;
    owned(&marker, false, false)?;
    let meta = marker.metadata().map_err(receipt_error)?;
    Ok(format!("{base}-{:x}-{:x}", meta.dev(), meta.ino()))
}

pub struct CreationStore {
    // The inner store supplies only protected descriptors and replace_artifact.
    // It never loads or writes tab/draft state and owns a DIFFERENT lock name.
    inner: SessionStore,
    binding: String,
    original: Option<(Vec<u8>, u64, u64)>,
    record: Option<CreationRecord>,
    frozen: bool,
    #[cfg(test)]
    fail_after_replace: bool,
}
impl CreationStore {
    pub fn open(fixture: &Path, config: &Path, cache: &Path) -> Result<Self, String> {
        if cache.starts_with(fixture) || fixture.starts_with(cache) || config.starts_with(cache) {
            return Err(receipt_error("fixture, cache and configuration overlap"));
        }
        let identity = receipt_binding(fixture, config, cache)?;
        let dir = path_file(cache, true)?;
        let lock = match child(
            &dir,
            RECEIPT_LOCK,
            libc::O_RDWR | libc::O_CREAT | libc::O_EXCL,
        ) {
            Ok(mut lock) => {
                owned(&lock, false, true)?;
                lock.write_all(RECEIPT_OWNER.as_bytes())
                    .map_err(receipt_error)?;
                lock.sync_all().map_err(receipt_error)?;
                dir.sync_all().map_err(receipt_error)?;
                lock
            }
            Err(_) => {
                let lock = child(&dir, RECEIPT_LOCK, libc::O_RDWR)?;
                owned(&lock, false, true)?;
                if bounded(lock.try_clone().map_err(receipt_error)?, 128)?
                    != RECEIPT_OWNER.as_bytes()
                {
                    return Err(receipt_error("unowned receipt lock"));
                }
                lock
            }
        };
        if unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) } != 0 {
            return Err(receipt_error("another creation owner holds this fixture"));
        }
        let inner = SessionStore {
            dir,
            _lock: lock,
            fixture: fixture.into(),
            config: config.into(),
            cache: cache.into(),
            registry: vec![String::new()],
            binding: identity.clone(),
            revision: 0,
            original: None,
            restored_history: None,
            restored_scope: None,
            reading_positions: vec![],
            recent_notes: None,
        };
        let mut store = Self {
            inner,
            binding: identity,
            original: None,
            record: None,
            frozen: false,
            #[cfg(test)]
            fail_after_replace: false,
        };
        store.validate_storage()?;
        store.original = store.read_bytes()?;
        store.record = store
            .original
            .as_ref()
            .map(|(bytes, _, _)| {
                let record: CreationRecord =
                    serde_json::from_slice(bytes).map_err(receipt_error)?;
                record.validate(&store.binding)?;
                Ok::<_, String>(record)
            })
            .transpose()?;
        Ok(store)
    }
    pub fn snapshot(&self) -> Option<CreationRecord> {
        self.record.clone()
    }
    /// Checked UI/startup observation, including exact validated absence.
    pub fn inspect(&self) -> Result<Option<CreationRecord>, String> {
        self.check_original()?;
        Ok(self.record.clone())
    }
    fn validate_storage(&self) -> Result<(), String> {
        let current = path_file(&self.inner.cache, true)?;
        let a = current.metadata().map_err(receipt_error)?;
        let b = self.inner.dir.metadata().map_err(receipt_error)?;
        if a.dev() != b.dev()
            || a.ino() != b.ino()
            || receipt_binding(&self.inner.fixture, &self.inner.config, &self.inner.cache)?
                != self.binding
        {
            return Err(receipt_error("fixture binding changed"));
        }
        let named = child(&self.inner.dir, RECEIPT_LOCK, libc::O_RDONLY)?;
        owned(&named, false, true)?;
        if bounded(named.try_clone().map_err(receipt_error)?, 128)? != RECEIPT_OWNER.as_bytes() {
            return Err(receipt_error("creation lock content changed"));
        }
        let a = named.metadata().map_err(receipt_error)?;
        let b = self.inner._lock.metadata().map_err(receipt_error)?;
        if a.dev() != b.dev() || a.ino() != b.ino() {
            return Err(receipt_error("creation lock replaced"));
        }
        Ok(())
    }
    fn read_bytes(&self) -> Result<Option<(Vec<u8>, u64, u64)>, String> {
        let file = match child(&self.inner.dir, RECEIPT, libc::O_RDONLY) {
            Ok(file) => file,
            Err(error) => {
                let mut st = std::mem::MaybeUninit::<libc::stat>::uninit();
                let result = unsafe {
                    libc::fstatat(
                        self.inner.dir.as_raw_fd(),
                        cname(RECEIPT).as_ptr(),
                        st.as_mut_ptr(),
                        libc::AT_SYMLINK_NOFOLLOW,
                    )
                };
                if result < 0
                    && std::io::Error::last_os_error().raw_os_error() == Some(libc::ENOENT)
                {
                    return Ok(None);
                }
                return Err(receipt_error(error));
            }
        };
        owned(&file, false, true)?;
        let meta = file.metadata().map_err(receipt_error)?;
        if meta.len() > RECEIPT_LIMIT {
            return Err(receipt_error("oversized receipt"));
        }
        Ok(Some((
            bounded(file, RECEIPT_LIMIT)?,
            meta.dev(),
            meta.ino(),
        )))
    }
    fn check_original(&self) -> Result<(), String> {
        if self.frozen {
            return Err(receipt_error("previous durable write outcome uncertain"));
        }
        self.validate_storage()?;
        if self.read_bytes()? != self.original {
            return Err(receipt_error("receipt changed externally"));
        }
        Ok(())
    }
    fn write_record(
        &mut self,
        record: CreationRecord,
        interrupt: bool,
    ) -> Result<CreationRecord, String> {
        self.check_original()?;
        record.validate(&self.binding)?;
        let bytes = serde_json::to_vec(&record).map_err(receipt_error)?;
        if bytes.len() as u64 > RECEIPT_LIMIT {
            return Err(receipt_error("oversized receipt"));
        }
        let result =
            self.inner
                .replace_artifact(RECEIPT, &bytes, record.revision, interrupt, || {
                    self.check_original()
                });
        #[cfg(test)]
        let result = if result.is_ok() && self.fail_after_replace {
            Err(receipt_error("simulated lost durability acknowledgement"))
        } else {
            result
        };
        if let Err(error) = result {
            self.frozen = true;
            return Err(receipt_error(error));
        }
        let original = match self.read_bytes() {
            Ok(Some(current)) if current.0 == bytes => Some(current),
            _ => {
                self.frozen = true;
                return Err(receipt_error("cannot verify committed receipt"));
            }
        };
        if let Err(error) = self.validate_storage() {
            self.frozen = true;
            return Err(error);
        }
        self.original = original;
        self.record = Some(record.clone());
        Ok(record)
    }
    /// Must succeed durably before the transport is allowed to dispatch a POST.
    /// A crash after this step, even before POST, recovers as unresolved Pending.
    pub fn begin(&mut self, title: &str) -> Result<CreationRecord, String> {
        self.check_original()?;
        let title = title.trim();
        if !valid_title(title) {
            return Err(receipt_error("invalid or oversized title"));
        }
        if self
            .record
            .as_ref()
            .is_some_and(|r| r.outcome == CreationOutcome::Pending)
        {
            return Err(receipt_error("unresolved creation intent retained"));
        }
        let revision = self
            .record
            .as_ref()
            .map_or(0, |r| r.revision)
            .checked_add(1)
            .ok_or_else(|| receipt_error("revision exhausted"))?;
        self.write_record(
            CreationRecord {
                owner: RECEIPT_OWNER.into(),
                schema: 1,
                binding: self.binding.clone(),
                revision,
                title: title.into(),
                outcome: CreationOutcome::Pending,
            },
            false,
        )
    }
    /// Only exact, current Pending can acquire a definitive transport outcome.
    /// Failed persistence retains Pending and never permits another POST.
    pub fn finish(
        &mut self,
        exact: &CreationRecord,
        outcome: CreationOutcome,
    ) -> Result<CreationRecord, String> {
        self.check_original()?;
        if self.record.as_ref() != Some(exact)
            || exact.outcome != CreationOutcome::Pending
            || outcome == CreationOutcome::Pending
        {
            return Err(receipt_error("stale or invalid completion"));
        }
        let mut record = exact.clone();
        record.revision = record
            .revision
            .checked_add(1)
            .ok_or_else(|| receipt_error("revision exhausted"))?;
        record.outcome = outcome;
        self.write_record(record, false)
    }
    /// An exit/dismissal acknowledgement may retain evidence, never delete it.
    /// Pending is valid here only after the caller's mutation worker has finished.
    pub fn verify_retained(&self, exact: &CreationRecord) -> Result<(), String> {
        self.check_original()?;
        if exact.outcome != CreationOutcome::Pending || self.record.as_ref() != Some(exact) {
            return Err(receipt_error("stale or non-pending retained receipt"));
        }
        let file = child(&self.inner.dir, RECEIPT, libc::O_RDONLY)?;
        owned(&file, false, true)?;
        file.sync_all().map_err(receipt_error)?;
        self.inner.dir.sync_all().map_err(receipt_error)?;
        self.check_original()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::{PermissionsExt, symlink};
    static NEXT_FIXTURE: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
    struct Fixture(PathBuf);
    impl Fixture {
        fn new() -> Self {
            Self::at_time(
                std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap()
                    .as_nanos(),
            )
        }
        fn at_time(timestamp: u128) -> Self {
            let root = std::env::temp_dir().canonicalize().unwrap().join(format!(
                "track-create-receipt-{}-{}-{}",
                std::process::id(),
                NEXT_FIXTURE.fetch_add(1, std::sync::atomic::Ordering::Relaxed),
                timestamp
            ));
            std::fs::create_dir(&root).unwrap();
            for name in ["vault", "cache"] {
                std::fs::create_dir(root.join(name)).unwrap();
            }
            std::fs::write(
                root.join("vault/.track-native-edit-fixture"),
                "disposable-fixture-only\n",
            )
            .unwrap();
            std::fs::write(root.join("config"), "fixture only").unwrap();
            Self(root)
        }
        fn open(&self) -> Result<CreationStore, String> {
            CreationStore::open(
                &self.0.join("vault"),
                &self.0.join("config"),
                &self.0.join("cache"),
            )
        }
        fn path(&self) -> PathBuf {
            self.0.join("cache").join(RECEIPT)
        }
        fn replace(&self, bytes: &[u8]) {
            std::fs::write(self.path(), bytes).unwrap();
            std::fs::set_permissions(self.path(), std::fs::Permissions::from_mode(0o600)).unwrap();
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            std::fs::remove_dir_all(&self.0).unwrap();
        }
    }
    #[test]
    fn fixture_roots_are_distinct_when_clock_timestamps_match() {
        let first = Fixture::at_time(7);
        let second = Fixture::at_time(7);
        assert_ne!(first.0, second.0);
        assert!(first.open().is_ok());
        assert!(second.open().is_ok());
    }

    #[test]
    fn crash_after_intent_before_post_is_unresolved_on_restart() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        let pending = store.begin(" 雪 ").unwrap();
        assert_eq!(pending.title, "雪");
        assert!(store.begin("duplicate").is_err());
        store.verify_retained(&pending).unwrap();
        drop(store);
        let mut restarted = fixture.open().unwrap();
        assert_eq!(restarted.snapshot(), Some(pending.clone()));
        assert!(restarted.begin("retry").is_err());
        restarted.verify_retained(&pending).unwrap();
    }
    #[test]
    fn explicit_definitive_completion_allows_new_attempt_not_stale_completion() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        for outcome in [
            CreationOutcome::NotSent,
            CreationOutcome::Rejected { status: 409 },
            CreationOutcome::Created {
                note_id: 42,
                title: "Alpha".into(),
            },
        ] {
            let pending = store.begin("Alpha").unwrap();
            let completed = store.finish(&pending, outcome).unwrap();
            assert!(store.finish(&pending, CreationOutcome::NotSent).is_err());
            assert!(store.verify_retained(&pending).is_err());
            assert_eq!(store.inspect().unwrap(), Some(completed));
        }
        drop(store);
        assert!(matches!(
            fixture.open().unwrap().snapshot().unwrap().outcome,
            CreationOutcome::Created { note_id: 42, .. }
        ));
    }
    #[test]
    fn interrupted_write_never_changes_original_and_freezes_instance() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        let pending = store.begin("Alpha").unwrap();
        let mut completed = pending.clone();
        completed.revision += 1;
        completed.outcome = CreationOutcome::NotSent;
        assert!(store.write_record(completed, true).is_err());
        assert!(store.verify_retained(&pending).is_err());
        assert!(store.begin("retry").is_err());
        drop(store);
        assert_eq!(fixture.open().unwrap().snapshot(), Some(pending));
    }
    #[test]
    fn lost_commit_acknowledgement_recovers_disk_record_without_retry() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        store.fail_after_replace = true;
        assert!(store.begin("Alpha").is_err()); // transport MUST NOT dispatch
        assert!(store.begin("Alpha").is_err());
        drop(store);
        let mut restarted = fixture.open().unwrap();
        assert_eq!(
            restarted.snapshot().unwrap().outcome,
            CreationOutcome::Pending
        );
        assert!(restarted.begin("Alpha").is_err());
    }
    #[test]
    fn subprocess_exit_after_durable_intent_recovers_pending_without_replay() {
        const ROOT_ENV: &str = "TRACK_CREATION_RECEIPT_EXIT_TEST_ROOT";
        if let Some(root) = std::env::var_os(ROOT_ENV) {
            let root = PathBuf::from(root);
            let mut store = CreationStore::open(
                &root.join("vault"),
                &root.join("config"),
                &root.join("cache"),
            )
            .unwrap();
            let pending = store.begin("Crash-window title").unwrap();
            assert_eq!(pending.outcome, CreationOutcome::Pending);
            // Exit immediately after durable begin, without running any Rust
            // destructors and without dispatching a POST or finishing the record.
            std::process::exit(0);
        }
        let fixture = Fixture::new();
        let status = std::process::Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                "session_store::creation_store::tests::subprocess_exit_after_durable_intent_recovers_pending_without_replay",
                "--nocapture",
            ])
            .env(ROOT_ENV, &fixture.0)
            .status()
            .unwrap();
        assert!(status.success());
        let before = std::fs::read(fixture.path()).unwrap();
        let mut restarted = fixture.open().unwrap();
        let pending = restarted.inspect().unwrap().unwrap();
        assert_eq!(pending.title, "Crash-window title");
        assert_eq!(pending.revision, 1);
        assert_eq!(pending.outcome, CreationOutcome::Pending);
        assert!(restarted.begin("Crash-window title").is_err());
        assert!(restarted.begin("Different title").is_err());
        restarted.verify_retained(&pending).unwrap();
        assert_eq!(std::fs::read(fixture.path()).unwrap(), before);
        assert!(!fixture.0.join("cache").join(STATE).exists());
        assert_eq!(
            std::fs::read_dir(fixture.0.join("vault")).unwrap().count(),
            1
        );
    }
    #[test]
    fn lost_created_checkpoint_ack_freezes_owner_but_restart_reads_created_receipt() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        let pending = store.begin("Alpha").unwrap();
        store.fail_after_replace = true;
        let created = CreationOutcome::Created {
            note_id: 42,
            title: "Alpha".into(),
        };
        assert!(store.finish(&pending, created.clone()).is_err());
        assert!(store.inspect().is_err());
        assert!(store.verify_retained(&pending).is_err());
        assert!(store.begin("Alpha").is_err());
        assert_eq!(store.snapshot(), Some(pending.clone()));
        let disk_after_finish = std::fs::read(fixture.path()).unwrap();
        drop(store);

        let restarted = fixture.open().unwrap();
        let receipt = restarted.inspect().unwrap().unwrap();
        assert_eq!(receipt.title, pending.title);
        assert_eq!(receipt.revision, pending.revision + 1);
        assert_eq!(receipt.outcome, created);
        // Loading only observes the already committed checkpoint. It does not
        // dispatch a creation or manufacture another attempt/revision.
        assert_eq!(restarted.inspect().unwrap(), Some(receipt));
        assert_eq!(std::fs::read(fixture.path()).unwrap(), disk_after_finish);
        assert_eq!(
            std::fs::read_dir(fixture.0.join("vault")).unwrap().count(),
            1
        );
    }
    #[test]
    fn exclusive_creation_lock_coexists_with_tab_writer() {
        let fixture = Fixture::new();
        let store = fixture.open().unwrap();
        assert!(fixture.open().is_err());
        let tabs = SessionStore::open(
            &fixture.0.join("vault"),
            &fixture.0.join("config"),
            &fixture.0.join("cache"),
            vec![String::new()],
        )
        .unwrap();
        assert!(store.snapshot().is_none());
        drop(tabs);
        drop(store);
        assert!(fixture.open().is_ok());
    }
    #[test]
    fn cas_rejects_same_bytes_replaced_inode_and_external_mutation() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        let pending = store.begin("Alpha").unwrap();
        let bytes = std::fs::read(fixture.path()).unwrap();
        std::fs::rename(fixture.path(), fixture.path().with_extension("old")).unwrap();
        fixture.replace(&bytes);
        assert!(store.verify_retained(&pending).is_err());
        assert!(store.finish(&pending, CreationOutcome::NotSent).is_err());
    }
    #[test]
    fn malformed_foreign_oversized_and_linked_receipts_fail_closed() {
        for kind in 0..6 {
            let fixture = Fixture::new();
            let mut store = fixture.open().unwrap();
            store.begin("Alpha").unwrap();
            drop(store);
            let bytes = std::fs::read(fixture.path()).unwrap();
            match kind {
                0 => fixture.replace(b"not json"),
                1 => fixture.replace(&vec![b'x'; RECEIPT_LIMIT as usize + 1]),
                2 => {
                    let mut value: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
                    value["binding"] = "foreign".into();
                    fixture.replace(&serde_json::to_vec(&value).unwrap());
                }
                3 => {
                    std::fs::rename(fixture.path(), fixture.path().with_extension("target"))
                        .unwrap();
                    symlink(fixture.path().with_extension("target"), fixture.path()).unwrap();
                }
                4 => {
                    std::fs::hard_link(fixture.path(), fixture.path().with_extension("hard"))
                        .unwrap();
                }
                _ => {
                    std::fs::set_permissions(
                        fixture.path(),
                        std::fs::Permissions::from_mode(0o644),
                    )
                    .unwrap();
                }
            }
            assert!(fixture.open().is_err());
        }
    }
    #[test]
    fn identity_changes_and_replaced_lock_prevent_post_intent() {
        for kind in 0..3 {
            let fixture = Fixture::new();
            let mut store = fixture.open().unwrap();
            match kind {
                0 => std::fs::write(fixture.0.join("config"), "changed").unwrap(),
                1 => {
                    let marker = fixture.0.join("vault/.track-native-edit-fixture");
                    std::fs::rename(&marker, marker.with_extension("old")).unwrap();
                    std::fs::write(&marker, "disposable-fixture-only\n").unwrap();
                }
                _ => {
                    let lock = fixture.0.join("cache").join(RECEIPT_LOCK);
                    std::fs::rename(&lock, lock.with_extension("old")).unwrap();
                    std::fs::write(&lock, RECEIPT_OWNER).unwrap();
                    std::fs::set_permissions(&lock, std::fs::Permissions::from_mode(0o600))
                        .unwrap();
                }
            }
            assert!(store.begin("Alpha").is_err());
            assert!(!fixture.path().exists());
        }
    }
    #[test]
    fn absent_snapshot_is_checked_and_intent_storage_failure_never_claims_success() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        assert_eq!(store.inspect().unwrap(), None);
        let temp = fixture
            .0
            .join("cache")
            .join(format!(".{RECEIPT}-{}-1.tmp", std::process::id()));
        std::fs::write(&temp, "foreign temporary entry").unwrap();
        assert!(store.begin("Alpha").is_err());
        assert!(!fixture.path().exists());
        assert!(store.inspect().is_err());
        assert_eq!(
            std::fs::read_to_string(temp).unwrap(),
            "foreign temporary entry"
        );
    }
    #[test]
    fn dropped_completion_keeps_original_pending_on_restart() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        let pending = store.begin("Alpha").unwrap();
        // A POST may have committed here. A lost response must never call finish.
        drop(store);
        let mut restarted = fixture.open().unwrap();
        assert_eq!(restarted.inspect().unwrap(), Some(pending.clone()));
        restarted.verify_retained(&pending).unwrap();
        assert!(restarted.begin("different title").is_err());
        drop(restarted);
        assert_eq!(fixture.open().unwrap().inspect().unwrap(), Some(pending));
    }
    #[test]
    fn invalid_titles_and_outcomes_do_not_replace_pending_evidence() {
        let fixture = Fixture::new();
        let mut store = fixture.open().unwrap();
        for title in [" ".to_owned(), "bad\0title".into(), "x".repeat(8193)] {
            assert!(store.begin(&title).is_err());
        }
        let pending = store.begin("Alpha").unwrap();
        for outcome in [
            CreationOutcome::Pending,
            CreationOutcome::Rejected { status: 503 },
            CreationOutcome::Created {
                note_id: 0,
                title: "Alpha".into(),
            },
            CreationOutcome::Created {
                note_id: 42,
                title: "Other".into(),
            },
        ] {
            assert!(store.finish(&pending, outcome).is_err());
        }
        assert_eq!(store.snapshot(), Some(pending.clone()));
        store.verify_retained(&pending).unwrap();
    }
}
