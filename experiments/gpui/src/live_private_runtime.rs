//! Inactive ownership helper for one newly created private runtime directory.
//! The caller must independently choose/authorize the parent and prove separation
//! from every vault, configuration and ordinary cache. This is not that proof.
//! All filesystem checks are point-in-time, not hostile same-user race defense.
//! In particular, mkdir/open and identity-check/unlink are not one atomic action.
use std::ffi::{CStr, CString};
use std::fmt;
use std::fs::File;
use std::io::{self, Read};
use std::os::fd::{AsRawFd, FromRawFd};
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};

const ATTEMPTS: usize = 8;
#[derive(Clone, Copy, PartialEq, Eq)]
struct Identity {
    device: u64,
    inode: u64,
    uid: u32,
    mode: u32,
}
impl Identity {
    fn file(file: &File) -> io::Result<Self> {
        let m = file.metadata()?;
        Ok(Self {
            device: m.dev(),
            inode: m.ino(),
            uid: m.uid(),
            mode: m.mode(),
        })
    }
    fn entry(parent: &File, name: &CStr) -> io::Result<Self> {
        let mut stat: libc::stat = unsafe { std::mem::zeroed() };
        if unsafe {
            libc::fstatat(
                parent.as_raw_fd(),
                name.as_ptr(),
                &mut stat,
                libc::AT_SYMLINK_NOFOLLOW,
            )
        } != 0
        {
            return Err(io::Error::last_os_error());
        }
        Ok(Self {
            device: stat.st_dev as u64,
            inode: stat.st_ino as u64,
            uid: stat.st_uid,
            mode: stat.st_mode as u32,
        })
    }
    fn owned_directory(self) -> bool {
        self.mode & libc::S_IFMT as u32 == libc::S_IFDIR as u32
            && self.uid == unsafe { libc::geteuid() }
    }
    fn parent_ok(self) -> bool {
        self.owned_directory() && self.mode & 0o022 == 0
    }
    fn private_ok(self) -> bool {
        self.owned_directory() && self.mode & 0o7777 == 0o700
    }
}
fn rejected() -> io::Error {
    io::Error::other("Private runtime identity could not be verified")
}
fn open_parent(path: &Path) -> io::Result<File> {
    let text = path.to_str().ok_or_else(rejected)?;
    if !text.starts_with('/')
        || text.len() > 4096
        || text.chars().any(char::is_control)
        || (text != "/"
            && text[1..]
                .split('/')
                .any(|p| p.is_empty() || p == "." || p == ".."))
        || std::fs::canonicalize(path)?.as_os_str() != path.as_os_str()
    {
        return Err(rejected());
    }
    let raw = CString::new(text).map_err(|_| rejected())?;
    let fd = unsafe {
        libc::open(
            raw.as_ptr(),
            libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if fd < 0 {
        return Err(io::Error::last_os_error());
    }
    // Every successful open transfers the descriptor into a closing owner.
    let file = unsafe { File::from_raw_fd(fd) };
    if !Identity::file(&file)?.parent_ok() {
        return Err(rejected());
    }
    Ok(file)
}
fn open_child(parent: &File, name: &CStr) -> io::Result<File> {
    let fd = unsafe {
        libc::openat(
            parent.as_raw_fd(),
            name.as_ptr(),
            libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if fd < 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(unsafe { File::from_raw_fd(fd) })
    }
}
fn random_name() -> io::Result<String> {
    let mut bytes = [0; 16];
    File::open("/dev/urandom")?.read_exact(&mut bytes)?;
    Ok(format!(
        "track-private-{}",
        bytes.iter().map(|b| format!("{b:02x}")).collect::<String>()
    ))
}

pub(super) struct CreateError {
    source: io::Error,
    retained_path: Option<PathBuf>,
}
impl CreateError {
    fn before(source: io::Error) -> Self {
        Self {
            source,
            retained_path: None,
        }
    }
    /// Diagnostic spelling only, not retained ownership or permission to remove
    /// the current occupant. Failed post-creation checks require separate review.
    pub(super) fn retained_path(&self) -> Option<&Path> {
        self.retained_path.as_deref()
    }
}
impl fmt::Debug for CreateError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("PrivateRuntimeCreateError")
            .field("retained", &self.retained_path.is_some())
            .finish()
    }
}
impl fmt::Display for CreateError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(if self.retained_path.is_some() {
            "Private runtime creation failed; created entry was retained for review"
        } else {
            "Private runtime creation failed before creating an entry"
        })
    }
}
impl std::error::Error for CreateError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        Some(&self.source)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum CleanupOutcome {
    Removed,
    AlreadyRemoved,
    RetainedNonEmpty,
    /// No removal attempted: identity/path checks failed. The entry may have
    /// moved or disappeared; this does not claim a usable runtime still exists.
    RefusedUnverified,
}

pub(super) struct PrivateRuntime {
    parent_path: PathBuf,
    path: PathBuf,
    name: CString,
    parent: File,
    child: File,
    parent_identity: Identity,
    child_identity: Identity,
    removed: bool,
}
impl fmt::Debug for PrivateRuntime {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("PrivateRuntime { values: [redacted] }")
    }
}
impl PrivateRuntime {
    /// Uses only the explicit existing parent. It never adopts an existing
    /// runtime, changes existing permissions, creates parents or scans vaults.
    pub(super) fn create(parent: &Path) -> Result<Self, CreateError> {
        Self::create_with_names(parent, random_name)
    }
    fn create_with_names(
        parent_path: &Path,
        mut next: impl FnMut() -> io::Result<String>,
    ) -> Result<Self, CreateError> {
        let parent = open_parent(parent_path).map_err(CreateError::before)?;
        let parent_identity = Identity::file(&parent).map_err(CreateError::before)?;
        for _ in 0..ATTEMPTS {
            let name = next().map_err(CreateError::before)?;
            if !name.strip_prefix("track-private-").is_some_and(|suffix| {
                suffix.len() == 32
                    && suffix
                        .bytes()
                        .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
            }) {
                return Err(CreateError::before(rejected()));
            }
            let name = CString::new(name).map_err(|_| CreateError::before(rejected()))?;
            let current = open_parent(parent_path)
                .and_then(|f| Identity::file(&f))
                .map_err(CreateError::before)?;
            if current != parent_identity
                || Identity::file(&parent).map_err(CreateError::before)? != parent_identity
            {
                return Err(CreateError::before(rejected()));
            }
            if unsafe { libc::mkdirat(parent.as_raw_fd(), name.as_ptr(), 0o700) } != 0 {
                let error = io::Error::last_os_error();
                if error.kind() == io::ErrorKind::AlreadyExists {
                    continue;
                }
                return Err(CreateError::before(error));
            }
            let path = parent_path.join(name.to_str().expect("generated ASCII name"));
            let finish = || -> io::Result<(File, Identity)> {
                let first = Identity::entry(&parent, &name)?;
                if !first.private_ok() {
                    return Err(rejected());
                }
                let child = open_child(&parent, &name)?;
                if Identity::file(&child)? != first
                    || Identity::entry(&parent, &name)? != first
                    || Identity::file(&parent)? != parent_identity
                    || Identity::file(&open_parent(parent_path)?)? != parent_identity
                {
                    return Err(rejected());
                }
                Ok((child, first))
            };
            // If creation succeeded but later verification fails, never guess
            // which occupant can be removed. Report the created spelling and
            // retain it. A restrictive umask is not repaired with chmod.
            let (child, child_identity) = finish().map_err(|source| CreateError {
                source,
                retained_path: Some(path.clone()),
            })?;
            return Ok(Self {
                parent_path: parent_path.to_owned(),
                path,
                name,
                parent,
                child,
                parent_identity,
                child_identity,
                removed: false,
            });
        }
        Err(CreateError::before(io::Error::new(
            io::ErrorKind::AlreadyExists,
            "Private runtime name collision limit reached",
        )))
    }
    /// Test-only correlation with the creator-held descriptor identity.
    /// This neither re-stats a supplied pathname nor grants launch authority.
    #[cfg(test)]
    pub(super) fn matches_observed_identity(&self, observed: &str) -> bool {
        observed
            == format!(
                "{}:{}",
                self.child_identity.device, self.child_identity.inode
            )
    }
    pub(super) fn path(&self) -> &Path {
        &self.path
    }
    pub(super) fn verify(&self) -> io::Result<()> {
        if self.removed
            || Identity::file(&self.parent)? != self.parent_identity
            || Identity::file(&open_parent(&self.parent_path)?)? != self.parent_identity
            || Identity::file(&self.child)? != self.child_identity
            || Identity::entry(&self.parent, &self.name)? != self.child_identity
        {
            return Err(rejected());
        }
        Ok(())
    }
    /// Empty-only cleanup. There is deliberately no recursive deletion and no
    /// Drop cleanup: populated runtime caches require a separate reviewed policy.
    /// The caller must first stop all users of this root. Same-user replacement
    /// between verification and unlink cannot be excluded by this portable API.
    pub(super) fn cleanup_empty(&mut self) -> io::Result<CleanupOutcome> {
        if self.removed {
            return Ok(CleanupOutcome::AlreadyRemoved);
        }
        if self.verify().is_err() {
            return Ok(CleanupOutcome::RefusedUnverified);
        }
        if unsafe {
            libc::unlinkat(
                self.parent.as_raw_fd(),
                self.name.as_ptr(),
                libc::AT_REMOVEDIR,
            )
        } == 0
        {
            self.removed = true;
            return Ok(CleanupOutcome::Removed);
        }
        let error = io::Error::last_os_error();
        match error.raw_os_error() {
            Some(libc::ENOTEMPTY) | Some(libc::EEXIST) => Ok(CleanupOutcome::RetainedNonEmpty),
            Some(libc::ENOENT) => Ok(CleanupOutcome::RefusedUnverified),
            _ => Err(error),
        }
    }
}
// Drop closes the retained Files only. It never removes a path.

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::{PermissionsExt, symlink};
    use std::sync::atomic::{AtomicU64, Ordering};
    static NEXT: AtomicU64 = AtomicU64::new(0);
    struct Fixture(PathBuf);
    impl Fixture {
        fn new() -> Self {
            let root = std::env::temp_dir().canonicalize().unwrap().join(format!(
                "track-private-owner-{}-{}",
                std::process::id(),
                NEXT.fetch_add(1, Ordering::Relaxed)
            ));
            std::fs::create_dir(&root).unwrap();
            std::fs::set_permissions(&root, std::fs::Permissions::from_mode(0o700)).unwrap();
            std::fs::create_dir(root.join("parent")).unwrap();
            std::fs::set_permissions(root.join("parent"), std::fs::Permissions::from_mode(0o700))
                .unwrap();
            Self(root)
        }
        fn parent(&self) -> PathBuf {
            self.0.join("parent")
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    #[test]
    fn creates_exclusive_private_roots_and_removes_only_empty_owned_entry() {
        let f = Fixture::new();
        std::fs::write(f.parent().join("unrelated"), b"untouched").unwrap();
        let mut first = PrivateRuntime::create(&f.parent()).unwrap();
        let second = PrivateRuntime::create(&f.parent()).unwrap();
        assert_ne!(first.path(), second.path());
        assert_eq!(
            std::fs::metadata(first.path()).unwrap().mode() & 0o7777,
            0o700
        );
        first.verify().unwrap();
        assert_eq!(
            format!("{first:?}"),
            "PrivateRuntime { values: [redacted] }"
        );
        let first_path = first.path().to_owned();
        assert_eq!(first.cleanup_empty().unwrap(), CleanupOutcome::Removed);
        std::fs::create_dir(&first_path).unwrap();
        assert_eq!(
            first.cleanup_empty().unwrap(),
            CleanupOutcome::AlreadyRemoved
        );
        assert!(first_path.is_dir());
        let retained = second.path().to_owned();
        drop(second);
        assert!(retained.is_dir(), "Drop must not remove the root");
        assert_eq!(
            std::fs::read(f.parent().join("unrelated")).unwrap(),
            b"untouched"
        );
    }
    #[test]
    fn unknown_contents_are_retained_until_caller_explicitly_removes_them() {
        let f = Fixture::new();
        let mut runtime = PrivateRuntime::create(&f.parent()).unwrap();
        let unknown = runtime.path().join("unknown");
        std::fs::write(&unknown, b"preserve").unwrap();
        assert_eq!(
            runtime.cleanup_empty().unwrap(),
            CleanupOutcome::RetainedNonEmpty
        );
        assert_eq!(std::fs::read(&unknown).unwrap(), b"preserve");
        runtime.verify().unwrap();
        std::fs::remove_file(unknown).unwrap();
        assert_eq!(runtime.cleanup_empty().unwrap(), CleanupOutcome::Removed);
    }
    #[test]
    fn child_and_parent_replacements_are_refused_without_deleting_either_tree() {
        for parent_replaced in [false, true] {
            for link in [false, true] {
                let f = Fixture::new();
                let mut runtime = PrivateRuntime::create(&f.parent()).unwrap();
                let original = if parent_replaced {
                    f.parent()
                } else {
                    runtime.path().to_owned()
                };
                let moved = f.0.join("moved");
                std::fs::rename(&original, &moved).unwrap();
                if link {
                    symlink(&moved, &original).unwrap();
                } else {
                    std::fs::create_dir(&original).unwrap();
                    std::fs::set_permissions(&original, std::fs::Permissions::from_mode(0o700))
                        .unwrap();
                    std::fs::write(original.join("replacement"), b"keep").unwrap();
                }
                assert!(runtime.verify().is_err());
                assert_eq!(
                    runtime.cleanup_empty().unwrap(),
                    CleanupOutcome::RefusedUnverified
                );
                assert!(std::fs::symlink_metadata(&original).is_ok());
                assert!(moved.is_dir());
                if !link {
                    assert_eq!(
                        std::fs::read(original.join("replacement")).unwrap(),
                        b"keep"
                    );
                }
            }
        }
    }
    #[test]
    fn unsafe_parent_and_changed_child_permissions_are_not_repaired() {
        let f = Fixture::new();
        let alias = f.0.join("alias");
        symlink(f.parent(), &alias).unwrap();
        for path in [alias, f.parent().join("."), PathBuf::from("relative")] {
            assert!(PrivateRuntime::create(&path).is_err());
        }
        std::fs::set_permissions(f.parent(), std::fs::Permissions::from_mode(0o777)).unwrap();
        assert!(PrivateRuntime::create(&f.parent()).is_err());
        assert_eq!(std::fs::metadata(f.parent()).unwrap().mode() & 0o777, 0o777);
        std::fs::set_permissions(f.parent(), std::fs::Permissions::from_mode(0o700)).unwrap();
        let mut runtime = PrivateRuntime::create(&f.parent()).unwrap();
        std::fs::set_permissions(runtime.path(), std::fs::Permissions::from_mode(0o755)).unwrap();
        assert_eq!(
            runtime.cleanup_empty().unwrap(),
            CleanupOutcome::RefusedUnverified
        );
        assert_eq!(
            std::fs::metadata(runtime.path()).unwrap().mode() & 0o777,
            0o755
        );
    }
    #[test]
    fn owned_nonwritable_public_parent_is_not_chmodded() {
        let f = Fixture::new();
        std::fs::set_permissions(f.parent(), std::fs::Permissions::from_mode(0o755)).unwrap();
        let mut runtime = PrivateRuntime::create(&f.parent()).unwrap();
        assert_eq!(std::fs::metadata(f.parent()).unwrap().mode() & 0o777, 0o755);
        assert_eq!(
            std::fs::metadata(runtime.path()).unwrap().mode() & 0o777,
            0o700
        );
        runtime.verify().unwrap();
        assert_eq!(runtime.cleanup_empty().unwrap(), CleanupOutcome::Removed);
    }
    #[test]
    fn restrictive_umask_reports_the_created_entry_without_repair_or_removal() {
        const TEST: &str = "live::private_runtime::tests::restrictive_umask_reports_the_created_entry_without_repair_or_removal";
        const MARKER: &str = "TRACK_TEST_PRIVATE_RUNTIME_UMASK_CHILD";
        let args: Vec<String> = std::env::args().collect();
        let child_mode = std::env::var(MARKER).as_deref() == Ok("1")
            && args.iter().any(|arg| arg == TEST)
            && args.iter().any(|arg| arg == "--exact")
            && args.iter().any(|arg| arg == "--test-threads=1");
        if child_mode {
            // This exact single-test subprocess is the only place changing
            // process-global umask. Normal parallel test processes never do.
            let f = Fixture::new();
            struct Mask(libc::mode_t);
            impl Drop for Mask {
                fn drop(&mut self) {
                    unsafe {
                        libc::umask(self.0);
                    }
                }
            }
            let previous = Mask(unsafe { libc::umask(0o777) });
            let error = PrivateRuntime::create(&f.parent()).unwrap_err();
            drop(previous);
            let retained = error
                .retained_path()
                .expect("successful mkdir must be reported");
            assert_eq!(retained.parent(), Some(f.parent().as_path()));
            let meta = std::fs::symlink_metadata(retained).unwrap();
            assert!(meta.is_dir());
            assert_eq!(meta.mode() & 0o7777, 0);
            assert_eq!(std::fs::read_dir(f.parent()).unwrap().count(), 1);
            assert!(!format!("{error:?}").contains(retained.to_str().unwrap()));
            // Explicit test-fixture cleanup only, after proving the helper did
            // not chmod or remove this entry. Permit its fixture guard to read it.
            std::fs::set_permissions(retained, std::fs::Permissions::from_mode(0o700)).unwrap();
            return;
        }
        use std::process::{Child, Command, Stdio};
        use std::time::{Duration, Instant};
        struct ChildGuard {
            child: Child,
            reaped: bool,
        }
        impl Drop for ChildGuard {
            fn drop(&mut self) {
                if self.reaped {
                    return;
                }
                let _ = self.child.kill();
                let end = Instant::now() + Duration::from_secs(2);
                while Instant::now() < end {
                    match self.child.try_wait() {
                        Ok(Some(_)) => return,
                        Err(_) => return,
                        Ok(None) => std::thread::sleep(Duration::from_millis(5)),
                    }
                }
            }
        }
        let child = Command::new(std::env::current_exe().unwrap())
            .args(["--exact", TEST, "--test-threads=1"])
            .env(MARKER, "1")
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap();
        let mut owner = ChildGuard {
            child,
            reaped: false,
        };
        let end = Instant::now() + Duration::from_secs(10);
        loop {
            assert!(Instant::now() < end, "isolated umask test timed out");
            if let Some(status) = owner.child.try_wait().unwrap() {
                owner.reaped = true;
                assert!(status.success(), "isolated umask test failed");
                break;
            }
            std::thread::sleep(Duration::from_millis(5));
        }
    }
    #[test]
    fn collisions_never_adopt_or_change_an_existing_entry() {
        let f = Fixture::new();
        let name = format!("track-private-{}", "a".repeat(32));
        let existing = f.parent().join(&name);
        std::fs::create_dir(&existing).unwrap();
        std::fs::set_permissions(&existing, std::fs::Permissions::from_mode(0o755)).unwrap();
        std::fs::write(existing.join("sentinel"), b"untouched").unwrap();
        let error =
            PrivateRuntime::create_with_names(&f.parent(), || Ok(name.clone())).unwrap_err();
        assert!(error.retained_path().is_none());
        assert_eq!(std::fs::metadata(&existing).unwrap().mode() & 0o777, 0o755);
        assert_eq!(
            std::fs::read(existing.join("sentinel")).unwrap(),
            b"untouched"
        );
    }
}
