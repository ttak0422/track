//! Explicit disposable-fixture UI state only. No note content or server identity.
use crate::tabs::{Tab, TabKey};
use serde::{Deserialize, Serialize};
use std::{
    collections::HashSet,
    fs::File,
    hash::{Hash, Hasher},
    io::{Read, Write},
    os::unix::{
        ffi::OsStrExt,
        fs::MetadataExt,
        io::{AsRawFd, FromRawFd},
    },
    path::{Path, PathBuf},
};

const STATE: &str = "track-native-tabs-v1.json";
const LOCK: &str = "track-native-tabs-v1.lock";
const LIMIT: u64 = 128 * 1024;
const OWNER: &str = "track-gpui-fixture-tabs";

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Snapshot {
    owner: String,
    version: u32,
    binding: String,
    revision: u64,
    tabs: Vec<Entry>,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Entry {
    kind: Kind,
    title: String,
}
#[derive(Serialize, Deserialize)]
#[serde(tag = "type", deny_unknown_fields)]
enum Kind {
    Home,
    Graph,
    Note { vault: String, note_id: i64 },
}

pub struct SessionStore {
    dir: File,
    _lock: File,
    fixture: PathBuf,
    config: PathBuf,
    cache: PathBuf,
    registry: Vec<String>,
    binding: String,
    revision: u64,
    original: Option<Vec<u8>>,
}
fn err(e: impl std::fmt::Display) -> String {
    format!("Fixture tab persistence disabled: {e}")
}
fn cname(name: &str) -> std::ffi::CString {
    std::ffi::CString::new(name).unwrap()
}
fn child(dir: &File, name: &str, flags: i32) -> Result<File, String> {
    let fd = unsafe {
        libc::openat(
            dir.as_raw_fd(),
            cname(name).as_ptr(),
            flags | libc::O_NOFOLLOW | libc::O_CLOEXEC | libc::O_NONBLOCK,
            0o600,
        )
    };
    if fd < 0 {
        return Err(err(std::io::Error::last_os_error()));
    }
    Ok(unsafe { File::from_raw_fd(fd) })
}
fn owned(file: &File, directory: bool, private: bool) -> Result<(), String> {
    let m = file.metadata().map_err(err)?;
    if m.uid() != unsafe { libc::geteuid() }
        || (directory && !m.is_dir())
        || (!directory && (!m.is_file() || m.nlink() != 1))
        || m.mode() & if private { 0o077 } else { 0o022 } != 0
    {
        return Err(err("wrong type, ownership, links or permissions"));
    }
    Ok(())
}
fn path_file(path: &Path, directory: bool) -> Result<File, String> {
    let canonical = path.canonicalize().map_err(err)?;
    let roots = [std::env::temp_dir(), PathBuf::from("/tmp")];
    if canonical != path
        || !roots
            .iter()
            .filter_map(|p| p.canonicalize().ok())
            .any(|p| canonical != p && canonical.starts_with(p))
    {
        return Err(err("paths must be canonical and beneath a temporary root"));
    }
    let c = std::ffi::CString::new(path.as_os_str().as_bytes()).map_err(err)?;
    let fd = unsafe {
        libc::open(
            c.as_ptr(),
            libc::O_RDONLY
                | libc::O_NOFOLLOW
                | libc::O_CLOEXEC
                | libc::O_NONBLOCK
                | if directory { libc::O_DIRECTORY } else { 0 },
        )
    };
    if fd < 0 {
        return Err(err(std::io::Error::last_os_error()));
    }
    let f = unsafe { File::from_raw_fd(fd) };
    owned(&f, directory, false)?;
    Ok(f)
}
fn bounded(file: File, limit: u64) -> Result<Vec<u8>, String> {
    let mut bytes = Vec::new();
    file.take(limit + 1).read_to_end(&mut bytes).map_err(err)?;
    if bytes.len() as u64 > limit {
        return Err(err("oversized state or configuration"));
    }
    Ok(bytes)
}
fn binding(
    fixture: &Path,
    config: &Path,
    cache: &Path,
    registry: &[String],
) -> Result<String, String> {
    let root = path_file(fixture, true)?;
    let marker = child(&root, ".track-native-edit-fixture", libc::O_RDONLY)?;
    owned(&marker, false, false)?;
    if bounded(marker, 64)? != b"disposable-fixture-only\n" {
        return Err(err("missing exact disposable fixture marker"));
    }
    let conf = path_file(config, false)?;
    let dir = path_file(cache, true)?;
    let mut hash = std::collections::hash_map::DefaultHasher::new();
    // Binding is an identity/change detector, not a cryptographic authentication claim.
    for (p, f) in [(fixture, &root), (config, &conf), (cache, &dir)] {
        p.hash(&mut hash);
        let m = f.metadata().map_err(err)?;
        m.dev().hash(&mut hash);
        m.ino().hash(&mut hash);
    }
    bounded(conf, 1024 * 1024)?.hash(&mut hash);
    registry.hash(&mut hash);
    Ok(format!("{:016x}", hash.finish()))
}
impl SessionStore {
    pub fn open(
        fixture: &Path,
        config: &Path,
        cache: &Path,
        mut registry: Vec<String>,
    ) -> Result<(Self, Vec<Tab>), String> {
        // Session artifacts must never become vault sidecars or overwrite an
        // explicitly supplied configuration/lock/state path.
        if cache.starts_with(fixture) || fixture.starts_with(cache) || config.starts_with(cache) {
            return Err(err("fixture, cache and configuration locations overlap"));
        }
        registry.sort();
        registry.dedup();
        if registry.is_empty() || registry.len() > 256 || registry.iter().any(|v| v.len() > 1024) {
            return Err(err("invalid registry"));
        }
        let binding = binding(fixture, config, cache, &registry)?;
        let dir = path_file(cache, true)?;
        let lock = match child(&dir, LOCK, libc::O_RDWR | libc::O_CREAT | libc::O_EXCL) {
            Ok(mut f) => {
                f.write_all(OWNER.as_bytes()).map_err(err)?;
                f.sync_all().map_err(err)?;
                f
            }
            Err(_) => {
                let f = child(&dir, LOCK, libc::O_RDWR)?;
                owned(&f, false, true)?;
                if bounded(f.try_clone().map_err(err)?, 128)? != OWNER.as_bytes() {
                    return Err(err("unowned lock file"));
                }
                f
            }
        };
        owned(&lock, false, true)?;
        if unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) } != 0 {
            return Err(err("another session writer owns this fixture"));
        }
        let mut store = Self {
            dir,
            _lock: lock,
            fixture: fixture.into(),
            config: config.into(),
            cache: cache.into(),
            registry,
            binding,
            revision: 0,
            original: None,
        };
        let original = store.read()?;
        let tabs = if let Some(bytes) = &original {
            let s: Snapshot = serde_json::from_slice(bytes).map_err(err)?;
            if s.owner != OWNER || s.version != 1 || s.binding != store.binding || s.revision == 0 {
                return Err(err("schema or fixture/registry binding mismatch"));
            }
            store.revision = s.revision;
            store.decode(s.tabs)?
        } else {
            vec![]
        };
        store.original = original;
        Ok((store, tabs))
    }
    fn read(&self) -> Result<Option<Vec<u8>>, String> {
        let f = match child(&self.dir, STATE, libc::O_RDONLY) {
            Ok(f) => f,
            Err(e) => {
                // Check absence without following symlinks. All other failures preserve the entry.
                let mut st = std::mem::MaybeUninit::<libc::stat>::uninit();
                let r = unsafe {
                    libc::fstatat(
                        self.dir.as_raw_fd(),
                        cname(STATE).as_ptr(),
                        st.as_mut_ptr(),
                        libc::AT_SYMLINK_NOFOLLOW,
                    )
                };
                if r < 0 && std::io::Error::last_os_error().raw_os_error() == Some(libc::ENOENT) {
                    return Ok(None);
                }
                return Err(e);
            }
        };
        owned(&f, false, true)?;
        Ok(Some(bounded(f, LIMIT)?))
    }
    fn decode(&self, entries: Vec<Entry>) -> Result<Vec<Tab>, String> {
        if entries.len() > 128 {
            return Err(err("too many tabs"));
        }
        let mut seen = HashSet::new();
        entries
            .into_iter()
            .map(|entry| {
                if entry.title.len() > 2048 || entry.title.contains('\0') {
                    return Err(err("invalid title hint"));
                }
                let key = match entry.kind {
                    Kind::Home => TabKey::Home,
                    Kind::Graph => TabKey::Graph,
                    Kind::Note { vault, note_id } => {
                        if note_id <= 0 || !self.registry.contains(&vault) {
                            return Err(err("unknown vault or invalid note identity"));
                        }
                        TabKey::Note { vault, note_id }
                    }
                };
                if !seen.insert(key.clone()) {
                    return Err(err("duplicate tab identity"));
                }
                let title = match &key {
                    TabKey::Home => "Home".into(),
                    TabKey::Graph => "Graph".into(),
                    _ => entry.title,
                };
                Ok(Tab { key, title })
            })
            .collect()
    }
    pub fn save(&mut self, tabs: &[Tab]) -> Result<(), String> {
        self.save_inner(tabs, false)
    }
    fn save_inner(&mut self, tabs: &[Tab], interrupt: bool) -> Result<(), String> {
        let current = path_file(&self.cache, true)?;
        let a = current.metadata().map_err(err)?;
        let b = self.dir.metadata().map_err(err)?;
        if a.dev() != b.dev()
            || a.ino() != b.ino()
            || binding(&self.fixture, &self.config, &self.cache, &self.registry)? != self.binding
        {
            return Err(err("fixture changed"));
        }
        // The lock name must still identify our held inode; cooperating writers never
        // unlink it. Refuse a replaced lock instead of continuing under stale ownership.
        let named_lock = child(&self.dir, LOCK, libc::O_RDONLY)?;
        owned(&named_lock, false, true)?;
        let named = named_lock.metadata().map_err(err)?;
        let held = self._lock.metadata().map_err(err)?;
        if named.dev() != held.dev() || named.ino() != held.ino() {
            return Err(err("writer lock was replaced"));
        }
        if self.read()? != self.original {
            return Err(err("session revision/content conflict"));
        }
        let entries: Vec<_> = tabs
            .iter()
            .map(|t| Entry {
                title: t.title.clone(),
                kind: match &t.key {
                    TabKey::Home => Kind::Home,
                    TabKey::Graph => Kind::Graph,
                    TabKey::Note { vault, note_id } => Kind::Note {
                        vault: vault.clone(),
                        note_id: *note_id,
                    },
                },
            })
            .collect();
        // Validate exactly the same bounds and identity policy in both directions.
        let entries_json = serde_json::to_vec(&entries).map_err(err)?;
        self.decode(serde_json::from_slice(&entries_json).map_err(err)?)?;
        let revision = self
            .revision
            .checked_add(1)
            .ok_or_else(|| err("revision overflow"))?;
        let bytes = serde_json::to_vec(&Snapshot {
            owner: OWNER.into(),
            version: 1,
            binding: self.binding.clone(),
            revision,
            tabs: entries,
        })
        .map_err(err)?;
        if bytes.len() as u64 > LIMIT {
            return Err(err("oversized state"));
        }
        let name = format!(".track-native-tabs-{}-{}.tmp", std::process::id(), revision);
        let mut file = child(
            &self.dir,
            &name,
            libc::O_WRONLY | libc::O_CREAT | libc::O_EXCL,
        )?;
        let result = (|| {
            file.write_all(&bytes).map_err(err)?;
            file.sync_all().map_err(err)?;
            if interrupt {
                return Err(err("simulated interruption before replacement"));
            }
            if unsafe {
                libc::renameat(
                    self.dir.as_raw_fd(),
                    cname(&name).as_ptr(),
                    self.dir.as_raw_fd(),
                    cname(STATE).as_ptr(),
                )
            } != 0
            {
                return Err(err(std::io::Error::last_os_error()));
            }
            self.original = Some(bytes);
            self.revision = revision;
            self.dir.sync_all().map_err(err)
        })();
        // This name was exclusively created by us; never remove any other artifact.
        if result.is_err() {
            unsafe {
                libc::unlinkat(self.dir.as_raw_fd(), cname(&name).as_ptr(), 0);
            }
        }
        result
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::{PermissionsExt, symlink};
    struct Fixture {
        root: PathBuf,
    }
    impl Fixture {
        fn new() -> Self {
            static N: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
            let root = std::env::temp_dir().canonicalize().unwrap().join(format!(
                "track-session-test-{}-{}-{}",
                std::process::id(),
                std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap()
                    .as_nanos(),
                N.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
            ));
            std::fs::create_dir(&root).unwrap();
            for d in ["vault", "cache"] {
                std::fs::create_dir(root.join(d)).unwrap();
            }
            std::fs::write(
                root.join("vault/.track-native-edit-fixture"),
                "disposable-fixture-only\n",
            )
            .unwrap();
            std::fs::write(root.join("config"), "synthetic registry").unwrap();
            Self { root }
        }
        fn open(&self) -> Result<(SessionStore, Vec<Tab>), String> {
            SessionStore::open(
                &self.root.join("vault"),
                &self.root.join("config"),
                &self.root.join("cache"),
                vec!["one".into(), "two".into()],
            )
        }
        fn state(&self) -> PathBuf {
            self.root.join("cache").join(STATE)
        }
        fn replace(&self, bytes: &[u8]) {
            std::fs::write(self.state(), bytes).unwrap();
            std::fs::set_permissions(self.state(), std::fs::Permissions::from_mode(0o600)).unwrap();
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            std::fs::remove_dir_all(&self.root).unwrap();
        }
    }
    fn tabs() -> Vec<Tab> {
        vec![
            Tab {
                key: TabKey::Note {
                    vault: "one".into(),
                    note_id: 7,
                },
                title: "日本語".into(),
            },
            Tab {
                key: TabKey::Note {
                    vault: "two".into(),
                    note_id: 7,
                },
                title: "Other".into(),
            },
            Tab {
                key: TabKey::Graph,
                title: "Graph".into(),
            },
        ]
    }
    #[test]
    fn roundtrip_is_bounded_identity_only_and_startup_does_not_write() {
        let f = Fixture::new();
        let (mut s, initial) = f.open().unwrap();
        assert!(initial.is_empty());
        assert!(!f.state().exists());
        s.save(&tabs()).unwrap();
        let bytes = std::fs::read(f.state()).unwrap();
        assert_eq!(std::fs::metadata(f.state()).unwrap().mode() & 0o777, 0o600);
        drop(s);
        let (s, restored) = f.open().unwrap();
        assert_eq!(restored, tabs());
        assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        assert_eq!(s.revision, 1);
        let json = String::from_utf8(bytes).unwrap();
        for forbidden in ["body", "draft", "etag", "token"] {
            assert!(!json.contains(forbidden));
        }
    }
    #[test]
    fn exclusive_writer_and_interrupted_write_preserve_previous() {
        let f = Fixture::new();
        let (mut s, _) = f.open().unwrap();
        s.save(&tabs()).unwrap();
        let before = std::fs::read(f.state()).unwrap();
        assert!(f.open().is_err());
        assert!(s.save_inner(&[], true).is_err());
        assert_eq!(std::fs::read(f.state()).unwrap(), before);
        drop(s);
        assert!(f.open().is_ok());
    }
    #[test]
    fn corrupt_large_wrong_version_and_unknown_vault_are_preserved() {
        let f = Fixture::new();
        let (mut s, _) = f.open().unwrap();
        s.save(&tabs()).unwrap();
        drop(s);
        let valid = std::fs::read(f.state()).unwrap();
        let mut bad: serde_json::Value = serde_json::from_slice(&valid).unwrap();
        bad["version"] = 9.into();
        let version = serde_json::to_vec(&bad).unwrap();
        let mut bad: serde_json::Value = serde_json::from_slice(&valid).unwrap();
        bad["tabs"][0]["kind"]["vault"] = "unknown".into();
        let unknown = serde_json::to_vec(&bad).unwrap();
        for bytes in [
            b"{".to_vec(),
            vec![b' '; LIMIT as usize + 1],
            version,
            unknown,
        ] {
            f.replace(&bytes);
            assert!(f.open().is_err());
            assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        }
    }
    #[test]
    fn invalid_snapshot_and_revision_conflict_do_not_overwrite() {
        let f = Fixture::new();
        let (mut s, _) = f.open().unwrap();
        s.save(&tabs()).unwrap();
        let before = std::fs::read(f.state()).unwrap();
        for entries in [
            vec![tabs()[0].clone(); 129],
            vec![tabs()[0].clone(); 2],
            vec![Tab {
                key: TabKey::Note {
                    vault: "one".into(),
                    note_id: -1,
                },
                title: "bad".into(),
            }],
            vec![Tab {
                key: TabKey::Home,
                title: "x".repeat(2049),
            }],
        ] {
            assert!(s.save(&entries).is_err());
            assert_eq!(std::fs::read(f.state()).unwrap(), before);
        }
        f.replace(b"external change");
        assert!(s.save(&[]).is_err());
        assert_eq!(std::fs::read(f.state()).unwrap(), b"external change");
    }
    #[test]
    fn binding_marker_permissions_symlink_and_wrong_type_fail_closed() {
        let f = Fixture::new();
        let (mut s, _) = f.open().unwrap();
        s.save(&tabs()).unwrap();
        drop(s);
        let before = std::fs::read(f.state()).unwrap();
        std::fs::write(f.root.join("config"), "changed registry").unwrap();
        assert!(f.open().is_err());
        assert_eq!(std::fs::read(f.state()).unwrap(), before);
        std::fs::remove_file(f.state()).unwrap();
        symlink(f.root.join("config"), f.state()).unwrap();
        assert!(f.open().is_err());
        std::fs::remove_file(f.state()).unwrap();
        std::fs::create_dir(f.state()).unwrap();
        assert!(f.open().is_err());
        std::fs::remove_dir(f.state()).unwrap();
        std::fs::write(f.root.join("vault/.track-native-edit-fixture"), "wrong").unwrap();
        assert!(f.open().is_err());
        std::fs::write(
            f.root.join("vault/.track-native-edit-fixture"),
            "disposable-fixture-only\n",
        )
        .unwrap();
        std::fs::set_permissions(f.root.join("cache"), std::fs::Permissions::from_mode(0o777))
            .unwrap();
        assert!(f.open().is_err());
    }
    #[test]
    fn invalid_owner_kind_ids_and_duplicate_restore_preserve_original() {
        let f = Fixture::new();
        let (mut s, _) = f.open().unwrap();
        s.save(&tabs()).unwrap();
        drop(s);
        let valid: serde_json::Value =
            serde_json::from_slice(&std::fs::read(f.state()).unwrap()).unwrap();
        for (pointer, value) in [
            ("/owner", serde_json::json!("unrelated application")),
            ("/tabs/0/kind/type", serde_json::json!("Draft")),
            ("/tabs/0/kind/note_id", serde_json::json!(-1)),
            ("/tabs/0/kind/note_id", serde_json::json!(0)),
            ("/revision", serde_json::json!(0)),
            ("/tabs/1", valid["tabs"][0].clone()),
        ] {
            let mut changed = valid.clone();
            *changed.pointer_mut(pointer).unwrap() = value;
            let bytes = serde_json::to_vec(&changed).unwrap();
            f.replace(&bytes);
            assert!(f.open().is_err(), "accepted {pointer}");
            assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        }
    }

    #[test]
    fn fixture_registry_and_parent_alias_are_not_reinterpreted() {
        let f = Fixture::new();
        let (mut s, _) = f.open().unwrap();
        s.save(&tabs()).unwrap();
        drop(s);
        let bytes = std::fs::read(f.state()).unwrap();
        assert!(
            SessionStore::open(
                &f.root.join("vault"),
                &f.root.join("config"),
                &f.root.join("cache"),
                vec!["one".into()]
            )
            .is_err()
        );
        assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        let other = Fixture::new();
        other.replace(&bytes);
        assert!(other.open().is_err());
        assert_eq!(std::fs::read(other.state()).unwrap(), bytes);
        symlink(&f.root, other.root.join("alias")).unwrap();
        assert!(
            SessionStore::open(
                &other.root.join("alias/vault"),
                &f.root.join("config"),
                &f.root.join("cache"),
                vec!["one".into(), "two".into()]
            )
            .is_err()
        );
    }

    #[test]
    fn replaced_lock_and_zero_id_cannot_write() {
        let f = Fixture::new();
        let (mut s, _) = f.open().unwrap();
        s.save(&tabs()).unwrap();
        let bytes = std::fs::read(f.state()).unwrap();
        let mut invalid = tabs();
        invalid[0].key = TabKey::Note {
            vault: "one".into(),
            note_id: 0,
        };
        assert!(s.save(&invalid).is_err());
        let lock = f.root.join("cache").join(LOCK);
        std::fs::remove_file(&lock).unwrap();
        std::fs::write(&lock, OWNER).unwrap();
        std::fs::set_permissions(&lock, std::fs::Permissions::from_mode(0o600)).unwrap();
        assert!(s.save(&[]).is_err());
        assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
    }
    #[test]
    fn overlapping_locations_fail_before_creating_session_artifacts() {
        let f = Fixture::new();
        let vault = f.root.join("vault");
        let config = f.root.join("config");
        let cache = f.root.join("cache");
        std::fs::create_dir(vault.join("nested-cache")).unwrap();
        std::fs::write(cache.join("config"), "synthetic config").unwrap();
        for (fixture, config, cache) in [
            (vault.clone(), config.clone(), vault.clone()),
            (vault.clone(), config.clone(), vault.join("nested-cache")),
            (vault.clone(), config.clone(), f.root.clone()),
            (vault.clone(), cache.join("config"), cache.clone()),
        ] {
            assert!(SessionStore::open(&fixture, &config, &cache, vec!["one".into()]).is_err());
            assert!(!cache.join(LOCK).exists());
            assert!(!cache.join(STATE).exists());
        }
    }
}
