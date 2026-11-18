//! Explicit disposable-fixture checkpoints. Access is serialized by a mutable
//! SessionStore borrow and its existing exclusive cache lock. No independent writer.
#![allow(dead_code)]
use super::*;
use crate::draft_recovery::{ARTIFACT_LIMIT, Record};
const RECOVERY: &str = "draft-recovery.json";

/// An opaque compare-and-swap token. A loaded record remains unresolved until the
/// owner explicitly recovers/discards it; merely loading never grants replacement.
pub struct RecoveryCheckpoint {
    binding: String,
    original: Option<(Vec<u8>, u64, u64)>,
    record: Option<Record>,
    resolved: bool,
    high_water: u64,
}
impl RecoveryCheckpoint {
    pub fn record(&self) -> Option<&Record> {
        self.record.as_ref()
    }
    /// Call only after the model has accepted an explicit same-source recovery.
    pub fn resolve(&mut self) {
        self.resolved = true;
    }
}
fn recovery_error() -> String {
    "Fixture draft persistence unavailable; recovery retained".into()
}
impl SessionStore {
    pub fn recovery_binding(&self) -> &str {
        &self.binding
    }
    fn read_recovery_bytes(&self) -> Result<Option<(Vec<u8>, u64, u64)>, String> {
        let f = match child(&self.dir, RECOVERY, libc::O_RDONLY) {
            Ok(f) => f,
            Err(_) => {
                let mut st = std::mem::MaybeUninit::<libc::stat>::uninit();
                let rc = unsafe {
                    libc::fstatat(
                        self.dir.as_raw_fd(),
                        cname(RECOVERY).as_ptr(),
                        st.as_mut_ptr(),
                        libc::AT_SYMLINK_NOFOLLOW,
                    )
                };
                if rc < 0 && std::io::Error::last_os_error().raw_os_error() == Some(libc::ENOENT) {
                    return Ok(None);
                }
                return Err(recovery_error());
            }
        };
        owned(&f, false, true).map_err(|_| recovery_error())?;
        let meta = f.metadata().map_err(|_| recovery_error())?;
        if meta.len() > ARTIFACT_LIMIT as u64 {
            return Err(recovery_error());
        }
        let bytes = bounded(f, ARTIFACT_LIMIT as u64).map_err(|_| recovery_error())?;
        Ok(Some((bytes, meta.dev(), meta.ino())))
    }
    pub fn load_recovery(&mut self) -> Result<RecoveryCheckpoint, String> {
        self.validate_storage().map_err(|_| recovery_error())?;
        let original = self.read_recovery_bytes()?;
        let record = original
            .as_ref()
            .map(|(bytes, _, _)| {
                let record: Record = serde_json::from_slice(bytes).map_err(|_| recovery_error())?;
                record
                    .validate(&self.binding, &self.registry)
                    .map_err(|_| recovery_error())?;
                Ok::<_, String>(record)
            })
            .transpose()?;
        let high_water = record.as_ref().map_or(0, |r| r.revision);
        Ok(RecoveryCheckpoint {
            high_water,
            binding: self.binding.clone(),
            resolved: record.is_none(),
            original,
            record,
        })
    }
    fn check_recovery(&self, checkpoint: &RecoveryCheckpoint) -> Result<(), String> {
        self.validate_storage().map_err(|_| recovery_error())?;
        if checkpoint.binding != self.binding || self.read_recovery_bytes()? != checkpoint.original
        {
            return Err(recovery_error());
        }
        Ok(())
    }
    pub fn checkpoint_recovery(
        &mut self,
        checkpoint: &mut RecoveryCheckpoint,
        record: Record,
    ) -> Result<(), String> {
        self.checkpoint_recovery_inner(checkpoint, record, false)
    }
    fn checkpoint_recovery_inner(
        &mut self,
        checkpoint: &mut RecoveryCheckpoint,
        record: Record,
        interrupt: bool,
    ) -> Result<(), String> {
        self.check_recovery(checkpoint)?;
        if !checkpoint.resolved || record.revision <= checkpoint.high_water {
            return Err(recovery_error());
        }
        record
            .validate(&self.binding, &self.registry)
            .map_err(|_| recovery_error())?;
        if let Some(previous) = &checkpoint.record {
            if record.revision <= previous.revision
                || record.scope != previous.scope
                || record.vault != previous.vault
                || record.note_id != previous.note_id
                || record.kind != previous.kind
            {
                return Err(recovery_error());
            }
        }
        let bytes = serde_json::to_vec(&record).map_err(|_| recovery_error())?;
        if bytes.len() > ARTIFACT_LIMIT {
            return Err(recovery_error());
        }
        self.replace_artifact(RECOVERY, &bytes, record.revision, interrupt, || {
            self.check_recovery(checkpoint)
        })
        .map_err(|_| recovery_error())?;
        checkpoint.original = self.read_recovery_bytes()?;
        checkpoint.high_water = record.revision;
        checkpoint.record = Some(record);
        Ok(())
    }
    /// Called only for an ordered model Clear. A canceled not-yet-written draft
    /// still checks protected storage and exact absence without an unlink.
    pub fn clear_recovery_if_present(
        &mut self,
        checkpoint: &mut RecoveryCheckpoint,
    ) -> Result<(), String> {
        if let Some(exact) = checkpoint.record().cloned() {
            self.clear_recovery(checkpoint, &exact)
        } else {
            self.check_recovery(checkpoint)
        }
    }
    /// Exact model-acknowledged clear or explicit confirmed discard only. The
    /// revision/source token excludes a newer checkpoint; queued jobs must also
    /// be invalidated by the model before this operation is dispatched. Directory
    /// sync failure after unlink is uncertain: preserve the old token and require
    /// explicit reload/reconciliation, never report successful durable removal.
    pub fn clear_recovery(
        &mut self,
        checkpoint: &mut RecoveryCheckpoint,
        exact: &Record,
    ) -> Result<(), String> {
        self.check_recovery(checkpoint)?;
        if checkpoint.record.as_ref() != Some(exact) {
            return Err(recovery_error());
        }
        if unsafe { libc::unlinkat(self.dir.as_raw_fd(), cname(RECOVERY).as_ptr(), 0) } != 0 {
            return Err(recovery_error());
        }
        self.dir.sync_all().map_err(|_| recovery_error())?;
        checkpoint.original = None;
        checkpoint.record = None;
        checkpoint.resolved = true;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::{PermissionsExt, symlink};
    struct Fixture(PathBuf);
    impl Fixture {
        fn new() -> Self {
            let root = std::env::temp_dir().canonicalize().unwrap().join(format!(
                "track-recovery-{}-{}",
                std::process::id(),
                std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap()
                    .as_nanos()
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
            std::fs::write(root.join("config"), "synthetic registry").unwrap();
            Self(root)
        }
        fn open(&self) -> SessionStore {
            open(&self.0)
        }
        fn path(&self) -> PathBuf {
            self.0.join("cache").join(RECOVERY)
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
    fn open(root: &Path) -> SessionStore {
        SessionStore::open(
            &root.join("vault"),
            &root.join("config"),
            &root.join("cache"),
            vec!["one".into()],
        )
        .unwrap()
        .0
    }
    fn record(s: &SessionStore) -> Record {
        Record {
            schema: 1,
            binding: s.binding.clone(),
            scope: "one".into(),
            vault: "one".into(),
            note_id: 7,
            kind: "note".into(),
            base_etag: "base".into(),
            body: "日本語 e\u{301}\n\n".into(),
            revision: 1,
            title: Some("fixture".into()),
        }
    }
    #[test]
    fn dropping_store_releases_lock_while_forked_child_is_before_exec() {
        let status = std::process::Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                "session_store::recovery_store::tests::forked_lock_helper",
                "--test-threads=1",
                "--nocapture",
            ])
            .env("TRACK_RECOVERY_FORK_HELPER", "1")
            .status()
            .unwrap();
        assert!(status.success());
    }

    #[test]
    fn forked_lock_helper() {
        if std::env::var_os("TRACK_RECOVERY_FORK_HELPER").is_none() {
            return;
        }
        let f = Fixture::new();
        let s = f.open();
        let mut release = [0; 2];
        // SAFETY: release is writable storage for exactly two file descriptors.
        assert_eq!(unsafe { libc::pipe(release.as_mut_ptr()) }, 0);
        // Model another test spawning a process between fork and exec. CLOEXEC
        // does not close inherited lock descriptors until exec actually runs.
        // SAFETY: the child uses only async-signal-safe libc calls and _exit;
        // it never accesses Rust state or runs inherited destructors.
        let pid = unsafe { libc::fork() };
        assert!(pid >= 0);
        if pid == 0 {
            // SAFETY: both pipe descriptors are inherited and valid; byte is
            // writable for the one-byte read. All calls are async-signal-safe,
            // and _exit prevents inherited Rust destructors from running.
            unsafe {
                libc::close(release[1]);
                let mut byte = 0u8;
                libc::read(release[0], (&mut byte as *mut u8).cast(), 1);
                libc::_exit(0);
            }
        }
        // SAFETY: this is the parent's valid read descriptor, closed only here.
        unsafe { libc::close(release[0]) };
        // A live owner must still exclude a second writer.
        let while_owned = SessionStore::open(
            &f.0.join("vault"),
            &f.0.join("config"),
            &f.0.join("cache"),
            vec!["one".into()],
        );
        drop(s);
        let reopened = SessionStore::open(
            &f.0.join("vault"),
            &f.0.join("config"),
            &f.0.join("cache"),
            vec!["one".into()],
        );
        // Always release/reap the child before assertions, including failures.
        // SAFETY: this is the parent's valid write descriptor, closed only here.
        unsafe { libc::close(release[1]) };
        let mut status = 0;
        // SAFETY: pid identifies our unreaped child; status is writable storage.
        assert_eq!(unsafe { libc::waitpid(pid, &mut status, 0) }, pid);
        assert_eq!(status, 0);
        assert!(while_owned.is_err());
        assert!(reopened.is_ok(), "{}", reopened.err().unwrap());
    }
    #[test]
    fn durable_roundtrip_unresolved_interruption_and_exact_clear() {
        let f = Fixture::new();
        let mut s = f.open();
        let mut token = s.load_recovery().unwrap();
        assert!(!f.path().exists());
        let first = record(&s);
        s.checkpoint_recovery(&mut token, first.clone()).unwrap();
        assert_eq!(std::fs::metadata(f.path()).unwrap().mode() & 0o777, 0o600);
        assert!(!f.0.join("cache").join(STATE).exists());
        let bytes = std::fs::read(f.path()).unwrap();
        drop(s);
        let mut s = f.open();
        let mut token = s.load_recovery().unwrap();
        assert!(token.record() == Some(&first));
        let mut second = first.clone();
        second.revision = 2;
        second.body.clear();
        assert!(s.checkpoint_recovery(&mut token, second.clone()).is_err());
        token.resolve();
        assert!(
            s.checkpoint_recovery_inner(&mut token, second.clone(), true)
                .is_err()
        );
        assert_eq!(std::fs::read(f.path()).unwrap(), bytes);
        s.checkpoint_recovery(&mut token, second.clone()).unwrap();
        assert!(s.clear_recovery(&mut token, &first).is_err());
        s.clear_recovery(&mut token, &second).unwrap();
        assert!(!f.path().exists());
        assert!(s.checkpoint_recovery(&mut token, second).is_err());
        assert!(!f.path().exists());
        assert_eq!(std::fs::read_dir(f.0.join("vault")).unwrap().count(), 1);
    }
    #[test]
    fn invalid_foreign_oversize_symlink_and_replaced_inode_are_preserved() {
        let f = Fixture::new();
        let mut s = f.open();
        let mut token = s.load_recovery().unwrap();
        let r = record(&s);
        s.checkpoint_recovery(&mut token, r.clone()).unwrap();
        let good = std::fs::read(f.path()).unwrap();
        let mut unknown = r.clone();
        unknown.schema = 99;
        for bytes in [
            b"{".to_vec(),
            vec![0xff],
            vec![b' '; ARTIFACT_LIMIT + 1],
            serde_json::to_vec(&unknown).unwrap(),
        ] {
            f.replace(&bytes);
            assert!(s.load_recovery().is_err());
            assert_eq!(std::fs::read(f.path()).unwrap(), bytes);
        }
        std::fs::remove_file(f.path()).unwrap();
        symlink(f.0.join("config"), f.path()).unwrap();
        assert!(s.load_recovery().is_err());
        std::fs::remove_file(f.path()).unwrap();
        f.replace(&good);
        let mut token = s.load_recovery().unwrap();
        token.resolve();
        let alternate = f.path().with_extension("replacement");
        std::fs::write(&alternate, &good).unwrap();
        std::fs::set_permissions(&alternate, std::fs::Permissions::from_mode(0o600)).unwrap();
        std::fs::rename(alternate, f.path()).unwrap();
        assert!(s.clear_recovery(&mut token, &r).is_err());
        assert_eq!(std::fs::read(f.path()).unwrap(), good);
        assert!(
            SessionStore::open(
                &f.0.join("vault"),
                &f.0.join("config"),
                &f.0.join("cache"),
                vec!["one".into()]
            )
            .is_err()
        );
    }
    #[test]
    fn session_without_recovery_calls_ignores_unrecognized_artifact() {
        let f = Fixture::new();
        f.replace(b"invalid recovery must remain untouched");
        let mut s = f.open();
        s.save(&[]).unwrap();
        assert_eq!(
            std::fs::read(f.path()).unwrap(),
            b"invalid recovery must remain untouched"
        );
    }
    #[test]
    fn recovery_refuses_binding_and_permission_changes() {
        let f = Fixture::new();
        let mut s = f.open();
        let mut token = s.load_recovery().unwrap();
        let r = record(&s);
        s.checkpoint_recovery(&mut token, r.clone()).unwrap();
        let before = std::fs::read(f.path()).unwrap();
        std::fs::set_permissions(f.path(), std::fs::Permissions::from_mode(0o644)).unwrap();
        assert!(s.load_recovery().is_err());
        std::fs::set_permissions(f.path(), std::fs::Permissions::from_mode(0o600)).unwrap();
        std::fs::write(f.0.join("config"), "changed binding").unwrap();
        assert!(s.clear_recovery(&mut token, &r).is_err());
        assert_eq!(std::fs::read(f.path()).unwrap(), before);
    }
    #[test]
    fn subprocess_checkpoint_helper() {
        let Some(root) = std::env::var_os("TRACK_RECOVERY_TEST_ROOT") else {
            return;
        };
        let mut s = open(Path::new(&root));
        let mut token = s.load_recovery().unwrap();
        if std::env::var_os("TRACK_RECOVERY_TEST_READ").is_some() {
            assert!(token.record() == Some(&record(&s)));
        } else {
            let r = record(&s);
            s.checkpoint_recovery(&mut token, r).unwrap();
            // Exit without Rust destructors, after the durable operation returned.
            std::process::exit(0);
        }
    }
    #[test]
    fn separate_process_restart_has_no_note_or_session_mutation() {
        let f = Fixture::new();
        for read in [false, true] {
            let mut cmd = std::process::Command::new(std::env::current_exe().unwrap());
            cmd.args([
                "--exact",
                "session_store::recovery_store::tests::subprocess_checkpoint_helper",
                "--nocapture",
            ])
            .env("TRACK_RECOVERY_TEST_ROOT", &f.0);
            if read {
                cmd.env("TRACK_RECOVERY_TEST_READ", "1");
            } else {
                cmd.env_remove("TRACK_RECOVERY_TEST_READ");
            }
            assert!(cmd.status().unwrap().success());
        }
        assert!(!f.0.join("cache").join(STATE).exists());
        assert_eq!(std::fs::read_dir(f.0.join("vault")).unwrap().count(), 1);
    }
}
