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

#[path = "creation_store.rs"]
pub(crate) mod creation_store;
#[path = "recovery_store.rs"]
mod recovery_store;
pub use creation_store::{CreationOutcome, CreationRecord, CreationStore};

const STATE: &str = "track-native-tabs-v1.json";
const LOCK: &str = "track-native-tabs-v1.lock";
const LIMIT: u64 = 128 * 1024;
const OWNER: &str = "track-gpui-fixture-tabs";

/// Coarse host Markdown checkpoint. No source text or volatile request identity.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PositionGeometry {
    pub viewport_width: f64,
    pub viewport_height: f64,
    pub reader_width: f64,
    pub reader_height: f64,
    pub scale: f64,
}
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ReadingPosition {
    pub vault: String,
    pub note_id: i64,
    pub etag: String,
    pub body_bytes: u64,
    pub renderer: String,
    pub block_index: usize,
    pub geometry: PositionGeometry,
}
impl ReadingPosition {
    pub fn key(&self) -> TabKey {
        TabKey::Note {
            vault: self.vault.clone(),
            note_id: self.note_id,
        }
    }
    pub fn valid(&self) -> bool {
        let g = &self.geometry;
        self.note_id > 0
            && self.vault.len() <= 1024
            && !self.vault.contains('\0')
            && !self.etag.is_empty()
            && self.etag.len() <= 4096
            && !self.etag.contains('\0')
            && !self.renderer.is_empty()
            && self.renderer.len() <= 256
            && !self.renderer.contains('\0')
            && self.body_bytes <= 64 * 1024 * 1024
            && self.block_index < 4096
            && [
                g.viewport_width,
                g.viewport_height,
                g.reader_width,
                g.reader_height,
            ]
            .into_iter()
            .all(|v| v.is_finite() && v > 0.0 && v <= 65536.0)
            && g.scale.is_finite()
            && g.scale > 0.0
            && g.scale <= 16.0
    }
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Snapshot {
    owner: String,
    version: u32,
    binding: String,
    revision: u64,
    tabs: Vec<Entry>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    history: Option<HistoryEntry>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    working_scope: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    reading_positions: Option<Vec<ReadingPosition>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    recent_notes: Option<Vec<RecentEntry>>,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct RecentEntry {
    vault: String,
    note_id: i64,
    title: String,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct HistoryEntry {
    routes: Vec<Kind>,
    cursor: usize,
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
    Home {},
    Graph {},
    Calendar {},
    Tag { tag: String, scope: String },
    Note { vault: String, note_id: i64 },
}

fn encode_kind(key: &TabKey) -> Kind {
    match key {
        TabKey::Home => Kind::Home {},
        TabKey::Graph => Kind::Graph {},
        TabKey::Calendar => Kind::Calendar {},
        TabKey::Tag { tag, scope } => Kind::Tag {
            tag: tag.clone(),
            scope: scope.clone(),
        },
        TabKey::Note { vault, note_id } => Kind::Note {
            vault: vault.clone(),
            note_id: *note_id,
        },
    }
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
    pub restored_history: Option<crate::navigation::Snapshot>,
    pub restored_scope: Option<String>,
    pub reading_positions: Vec<ReadingPosition>,
    recent_notes: Option<Vec<RecentEntry>>,
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
            restored_history: None,
            restored_scope: None,
            reading_positions: vec![],
            recent_notes: None,
        };
        let original = store.read()?;
        let tabs = if let Some(bytes) = &original {
            let value: serde_json::Value = serde_json::from_slice(bytes).map_err(err)?;
            if value.get("version").and_then(|v| v.as_u64()) == Some(1)
                && value.get("history").is_some()
            {
                return Err(err("unexpected history in version 1"));
            }
            if value
                .get("version")
                .and_then(|v| v.as_u64())
                .is_some_and(|v| v < 3)
                && value.get("working_scope").is_some()
            {
                return Err(err("unexpected scope in legacy snapshot"));
            }
            if value
                .get("version")
                .and_then(|v| v.as_u64())
                .is_some_and(|v| v < 4)
                && value.get("reading_positions").is_some()
            {
                return Err(err("unexpected positions in legacy snapshot"));
            }
            if value
                .get("reading_positions")
                .is_some_and(|p| !p.is_array())
            {
                return Err(err("reading positions must be a list"));
            }
            let version = value.get("version").and_then(|v| v.as_u64());
            if (version.is_some_and(|v| v >= 6)
                && !value.get("recent_notes").is_some_and(|v| v.is_array()))
                || (version.is_some_and(|v| v < 6) && value.get("recent_notes").is_some())
            {
                return Err(err("invalid recent notes schema"));
            }
            let s: Snapshot = serde_json::from_slice(bytes).map_err(err)?;
            if s.owner != OWNER
                || !matches!(s.version, 1 | 2 | 3 | 4 | 5 | 6 | 7)
                || s.binding != store.binding
                || s.revision == 0
            {
                return Err(err("schema or fixture/registry binding mismatch"));
            }
            if s.version < 7
                && (s.tabs.iter().any(|t| matches!(t.kind, Kind::Calendar {}))
                    || s.history
                        .as_ref()
                        .is_some_and(|h| h.routes.iter().any(|r| matches!(r, Kind::Calendar {}))))
            {
                return Err(err("calendar route requires version 7"));
            }
            if s.version < 5
                && s.history
                    .as_ref()
                    .is_some_and(|h| h.routes.iter().any(|r| matches!(r, Kind::Tag { .. })))
            {
                return Err(err("tag page history requires version 5"));
            }
            store.restored_history = match (s.version, s.history) {
                (1, None) => None,
                (2 | 3 | 4 | 5 | 6 | 7, Some(history)) => Some(store.decode_history(history)?),
                _ => return Err(err("invalid history schema")),
            };
            store.restored_scope = match (s.version, s.working_scope) {
                (1 | 2, None) => None,
                (3 | 4 | 5 | 6 | 7, Some(scope)) => {
                    store.validate_scope(&scope)?;
                    Some(scope)
                }
                _ => return Err(err("invalid working scope schema")),
            };
            if let Some(entries) = &s.recent_notes {
                store.validate_recents(entries)?;
            }
            store.recent_notes = s.recent_notes;
            store.revision = s.revision;
            let tabs = store.decode(s.tabs)?;
            let positions = s.reading_positions.unwrap_or_default();
            store.validate_positions(&positions, &tabs)?;
            store.reading_positions = positions;
            tabs
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
    fn decode_kind(&self, kind: Kind) -> Result<TabKey, String> {
        match kind {
            Kind::Home {} => Ok(TabKey::Home),
            Kind::Graph {} => Ok(TabKey::Graph),
            Kind::Calendar {} => Ok(TabKey::Calendar),
            Kind::Tag { tag, scope } if TabKey::valid_tag_route(&tag, &scope) => {
                Ok(TabKey::Tag { tag, scope })
            }
            Kind::Note { vault, note_id }
                if note_id > 0
                    && vault.len() <= 1024
                    && !vault.contains('\0')
                    && self.registry.contains(&vault) =>
            {
                Ok(TabKey::Note { vault, note_id })
            }
            _ => Err(err("unknown vault or invalid note identity")),
        }
    }
    fn decode_history(&self, history: HistoryEntry) -> Result<crate::navigation::Snapshot, String> {
        if history.routes.is_empty()
            || history.routes.len() > crate::navigation::LIMIT
            || history.cursor >= history.routes.len()
        {
            return Err(err("invalid history bounds"));
        }
        Ok(crate::navigation::Snapshot {
            routes: history
                .routes
                .into_iter()
                .map(|r| self.decode_kind(r))
                .collect::<Result<_, _>>()?,
            cursor: history.cursor,
        })
    }
    #[cfg(test)]
    pub fn save_with_history(
        &mut self,
        tabs: &[Tab],
        history: &crate::navigation::Snapshot,
    ) -> Result<(), String> {
        self.save_snapshot(tabs, history, None, false)
    }
    fn validate_scope(&self, scope: &str) -> Result<(), String> {
        if scope.len() > 1024 || scope.contains('\0') || !self.registry.iter().any(|v| v == scope) {
            return Err(err("unknown or invalid working scope"));
        }
        Ok(())
    }
    pub fn save_with_scope(
        &mut self,
        tabs: &[Tab],
        history: &crate::navigation::Snapshot,
        scope: &str,
    ) -> Result<(), String> {
        self.save_snapshot(tabs, history, Some(scope), false)
    }
    pub fn restored_recents(&self) -> Vec<crate::recent_notes::Entry> {
        self.recent_notes
            .as_deref()
            .unwrap_or_default()
            .iter()
            .map(|e| crate::recent_notes::Entry {
                key: crate::live::NoteKey {
                    vault: e.vault.clone(),
                    note_id: e.note_id,
                },
                title: e.title.clone(),
            })
            .collect()
    }
    fn validate_recents(&self, entries: &[RecentEntry]) -> Result<(), String> {
        let mut seen = HashSet::new();
        if entries.len() > 100
            || entries.iter().any(|e| {
                e.note_id <= 0
                    || e.vault.len() > 1024
                    || e.vault.contains('\0')
                    || !self.registry.contains(&e.vault)
                    || e.title.len() > 2048
                    || e.title.chars().count() > 512
                    || e.title.contains('\0')
                    || e.title.trim().is_empty()
                    || !seen.insert((e.vault.clone(), e.note_id))
            })
        {
            return Err(err("invalid recent note metadata"));
        }
        Ok(())
    }
    pub fn save_with_recents(
        &mut self,
        tabs: &[Tab],
        history: &crate::navigation::Snapshot,
        scope: &str,
        positions: &[ReadingPosition],
        recents: &[crate::recent_notes::Entry],
    ) -> Result<(), String> {
        let entries = recents
            .iter()
            .map(|e| RecentEntry {
                vault: e.key.vault.clone(),
                note_id: e.key.note_id,
                title: e.title.clone(),
            })
            .collect::<Vec<_>>();
        self.validate_recents(&entries)?;
        let old = self.recent_notes.replace(entries);
        let result = self.save_with_positions(tabs, history, scope, positions);
        if result.is_err() {
            self.recent_notes = old;
        }
        result
    }
    pub fn save_with_positions(
        &mut self,
        tabs: &[Tab],
        history: &crate::navigation::Snapshot,
        scope: &str,
        positions: &[ReadingPosition],
    ) -> Result<(), String> {
        self.validate_positions(positions, tabs)?;
        let previous = std::mem::replace(&mut self.reading_positions, positions.to_vec());
        let result = self.save_with_scope(tabs, history, scope);
        if result.is_err() {
            self.reading_positions = previous;
        }
        result
    }
    fn validate_positions(
        &self,
        positions: &[ReadingPosition],
        tabs: &[Tab],
    ) -> Result<(), String> {
        let mut seen = HashSet::new();
        if positions.len() > 128
            || positions.iter().any(|p| {
                !p.valid()
                    || !self.registry.contains(&p.vault)
                    || !seen.insert(p.key())
                    || !tabs.iter().any(|tab| tab.key == p.key())
            })
        {
            return Err(err("invalid, duplicate or orphan reading position"));
        }
        Ok(())
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
                let key = self.decode_kind(entry.kind)?;
                if matches!(key, TabKey::Tag { .. }) {
                    return Err(err("tag page cannot be a persistent tab"));
                }
                if !seen.insert(key.clone()) {
                    return Err(err("duplicate tab identity"));
                }
                let title = match &key {
                    TabKey::Home => "Home".into(),
                    TabKey::Graph => "Graph".into(),
                    TabKey::Calendar => "Calendar".into(),
                    _ => entry.title,
                };
                Ok(Tab { key, title })
            })
            .collect()
    }
    #[cfg(test)]
    pub fn save(&mut self, tabs: &[Tab]) -> Result<(), String> {
        self.save_inner(tabs, false)
    }
    #[cfg(test)]
    fn save_inner(&mut self, tabs: &[Tab], interrupt: bool) -> Result<(), String> {
        self.save_snapshot(
            tabs,
            &crate::navigation::Snapshot::default(),
            None,
            interrupt,
        )
    }
    fn save_snapshot(
        &mut self,
        tabs: &[Tab],
        history: &crate::navigation::Snapshot,
        scope: Option<&str>,
        interrupt: bool,
    ) -> Result<(), String> {
        if let Some(scope) = scope {
            self.validate_scope(scope)?;
        }
        self.validate_storage()?;
        if self.read()? != self.original {
            return Err(err("session revision/content conflict"));
        }
        let entries: Vec<_> = tabs
            .iter()
            .map(|t| Entry {
                title: t.title.clone(),
                kind: encode_kind(&t.key),
            })
            .collect();
        // Validate exactly the same bounds and identity policy in both directions.
        let entries_json = serde_json::to_vec(&entries).map_err(err)?;
        self.decode(serde_json::from_slice(&entries_json).map_err(err)?)?;
        self.decode_history(HistoryEntry {
            routes: history.routes.iter().map(encode_kind).collect(),
            cursor: history.cursor,
        })?;
        let revision = self
            .revision
            .checked_add(1)
            .ok_or_else(|| err("revision overflow"))?;
        let positions: Vec<_> = self
            .reading_positions
            .iter()
            .filter(|p| tabs.iter().any(|t| t.key == p.key()))
            .cloned()
            .collect();
        self.validate_positions(&positions, tabs)?;
        let bytes = serde_json::to_vec(&Snapshot {
            owner: OWNER.into(),
            // Version 5 adds page-only tag history. Keep non-tag saves compatible.
            version: if tabs.iter().any(|t| t.key == TabKey::Calendar)
                || history.routes.iter().any(|r| *r == TabKey::Calendar)
            {
                7
            } else if self.recent_notes.is_some() {
                6
            } else if history
                .routes
                .iter()
                .any(|r| matches!(r, TabKey::Tag { .. }))
            {
                5
            } else {
                4
            },
            binding: self.binding.clone(),
            revision,
            tabs: entries,
            working_scope: Some(scope.unwrap_or(&self.registry[0]).to_owned()),
            reading_positions: Some(positions.clone()),
            recent_notes: if tabs.iter().any(|t| t.key == TabKey::Calendar)
                || history.routes.iter().any(|r| *r == TabKey::Calendar)
            {
                Some(self.recent_notes.clone().unwrap_or_default())
            } else {
                self.recent_notes.clone()
            },
            history: Some(HistoryEntry {
                routes: history.routes.iter().map(encode_kind).collect(),
                cursor: history.cursor,
            }),
        })
        .map_err(err)?;
        if bytes.len() as u64 > LIMIT {
            return Err(err("oversized state"));
        }
        self.replace_artifact(STATE, &bytes, revision, interrupt, || {
            self.validate_storage()?;
            if self.read()? != self.original {
                return Err(err("session revision/content conflict"));
            }
            Ok(())
        })?;
        self.original = Some(bytes);
        self.revision = revision;
        self.reading_positions = positions;
        Ok(())
    }
    fn validate_storage(&self) -> Result<(), String> {
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
        Ok(())
    }
    // One primitive for both bounded artifacts, under the same held session lock.
    // File and directory fsync are requested; power-loss guarantees remain platform-dependent.
    // Failure after rename is uncertain, not a rollback. Callers retain their old
    // compare token and must stop/reload/reconcile rather than blindly retry.
    fn replace_artifact(
        &self,
        target: &str,
        bytes: &[u8],
        revision: u64,
        interrupt: bool,
        before_commit: impl FnOnce() -> Result<(), String>,
    ) -> Result<(), String> {
        let name = format!(".{target}-{}-{revision}.tmp", std::process::id());
        let mut file = child(
            &self.dir,
            &name,
            libc::O_WRONLY | libc::O_CREAT | libc::O_EXCL,
        )?;
        let result = (|| {
            owned(&file, false, true)?;
            file.write_all(bytes).map_err(err)?;
            file.sync_all().map_err(err)?;
            if interrupt {
                return Err(err("simulated interruption before replacement"));
            }
            before_commit()?;
            if unsafe {
                libc::renameat(
                    self.dir.as_raw_fd(),
                    cname(&name).as_ptr(),
                    self.dir.as_raw_fd(),
                    cname(target).as_ptr(),
                )
            } != 0
            {
                return Err(err(std::io::Error::last_os_error()));
            }
            self.dir.sync_all().map_err(err)
        })();
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
    fn recent(vault: &str, id: i64) -> crate::recent_notes::Entry {
        crate::recent_notes::Entry {
            key: crate::live::NoteKey {
                vault: vault.into(),
                note_id: id,
            },
            title: "Saved".into(),
        }
    }
    #[test]
    fn calendar_schema7_roundtrip_and_legacy_smuggling_rejection() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        let entries = vec![Tab {
            key: TabKey::Calendar,
            title: "Calendar".into(),
        }];
        let mut history = crate::navigation::History::default();
        history.record(Some(TabKey::Calendar));
        store
            .save_with_recents(&entries, &history.snapshot(), "one", &[], &[])
            .unwrap();
        drop(store);
        let bytes = std::fs::read(f.state()).unwrap();
        let (store, restored) = f.open().unwrap();
        assert_eq!(restored, entries);
        assert_eq!(
            crate::navigation::History::restore_home(store.restored_history.clone().unwrap())
                .unwrap()
                .current(),
            Some(&TabKey::Home)
        );
        assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        drop(store);
        let value: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(value["version"], 7);
        assert!(value.get("month").is_none());
        assert!(value.get("selected_day").is_none());
        for version in 1..=6 {
            for keep_tab in [true, false] {
                let mut bad = value.clone();
                bad["version"] = version.into();
                if keep_tab {
                    bad["history"]["routes"] = serde_json::json!([{"type":"Home"}]);
                    bad["history"]["cursor"] = 0.into();
                } else {
                    bad["tabs"] = serde_json::json!([]);
                }
                let map = bad.as_object_mut().unwrap();
                if version < 6 {
                    map.remove("recent_notes");
                }
                if version < 4 {
                    map.remove("reading_positions");
                }
                if version < 3 {
                    map.remove("working_scope");
                }
                if version == 1 && !keep_tab {
                    continue;
                }
                if version == 1 {
                    map.remove("history");
                }
                let invalid = serde_json::to_vec(&bad).unwrap();
                f.replace(&invalid);
                assert!(f.open().is_err());
                assert_eq!(std::fs::read(f.state()).unwrap(), invalid);
            }
        }
    }
    #[test]
    fn recents_schema6_survives_closed_tabs_and_rejects_bad_metadata_without_replacement() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        let rows = vec![recent("one", 42), recent("two", 42)];
        store
            .save_with_recents(
                &[],
                &crate::navigation::Snapshot::default(),
                "one",
                &[],
                &rows,
            )
            .unwrap();
        drop(store);
        let bytes = std::fs::read(f.state()).unwrap();
        let (store, tabs) = f.open().unwrap();
        assert!(tabs.is_empty());
        assert_eq!(store.restored_recents(), rows);
        assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        drop(store);
        let value: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(value["version"], 6);
        for bad in [
            serde_json::json!([{"vault":"missing","note_id":42,"title":"X"}]),
            serde_json::json!([{"vault":"one","note_id":0,"title":"X"}]),
            serde_json::json!([{"vault":"one","note_id":-1,"title":"X"}]),
            serde_json::json!([{"vault":"one","note_id":42,"title":"X"},{"vault":"one","note_id":42,"title":"Y"}]),
            serde_json::json!([{"vault":"one","note_id":42,"title":"X".repeat(513)}]),
            serde_json::Value::Null,
        ] {
            let mut invalid = value.clone();
            invalid["recent_notes"] = bad;
            let invalid = serde_json::to_vec(&invalid).unwrap();
            f.replace(&invalid);
            assert!(f.open().is_err());
            assert_eq!(std::fs::read(f.state()).unwrap(), invalid);
        }
        for version in 1..=5 {
            let mut old = value.clone();
            old["version"] = version.into();
            assert!({
                f.replace(&serde_json::to_vec(&old).unwrap());
                f.open().is_err()
            });
            let map = old.as_object_mut().unwrap();
            map.remove("recent_notes");
            if version < 4 {
                map.remove("reading_positions");
            }
            if version < 3 {
                map.remove("working_scope");
            }
            if version < 2 {
                map.remove("history");
            }
            let old = serde_json::to_vec(&old).unwrap();
            f.replace(&old);
            let (store, _) = f.open().unwrap();
            assert!(store.restored_recents().is_empty());
            assert_eq!(std::fs::read(f.state()).unwrap(), old);
            drop(store);
        }
    }
    #[test]
    fn recents_limits_do_not_replace_prior_artifact() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        let history = crate::navigation::Snapshot::default();
        let rows = (1..=100).map(|id| recent("one", id)).collect::<Vec<_>>();
        store
            .save_with_recents(&[], &history, "one", &[], &rows)
            .unwrap();
        let before = std::fs::read(f.state()).unwrap();
        let mut extra = rows.clone();
        extra.push(recent("one", 101));
        assert!(
            store
                .save_with_recents(&[], &history, "one", &[], &extra)
                .is_err()
        );
        let large = rows
            .into_iter()
            .map(|mut e| {
                e.title = "🦀".repeat(512);
                e
            })
            .collect::<Vec<_>>();
        assert!(
            store
                .save_with_recents(&[], &history, "one", &[], &large)
                .is_err()
        );
        assert_eq!(std::fs::read(f.state()).unwrap(), before);
    }
    #[test]
    fn tag_history_v5_roundtrip_preserves_tabs_and_rejects_legacy_reinterpretation() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        let mut history = crate::navigation::History::default();
        let tag = TabKey::Tag {
            tag: "ος/日本語 a".into(),
            scope: "".into(),
        };
        history.record(Some(tag.clone()));
        store
            .save_with_history(&tabs(), &history.snapshot())
            .unwrap();
        drop(store);
        let original = std::fs::read(f.state()).unwrap();
        let (store, restored_tabs) = f.open().unwrap();
        assert_eq!(restored_tabs, tabs());
        assert_eq!(store.restored_history, Some(history.snapshot()));
        let safe =
            crate::navigation::History::restore_home(store.restored_history.clone().unwrap())
                .unwrap();
        assert_eq!(safe.current(), Some(&TabKey::Home));
        assert_eq!(safe.peek(false).unwrap().route, Some(tag));
        drop(store);
        let value: serde_json::Value = serde_json::from_slice(&original).unwrap();
        assert_eq!(value["version"], 5);
        for version in [2, 3, 4] {
            let mut old = value.clone();
            old["version"] = version.into();
            if version < 4 {
                old.as_object_mut().unwrap().remove("reading_positions");
            }
            if version < 3 {
                old.as_object_mut().unwrap().remove("working_scope");
            }
            let bytes = serde_json::to_vec(&old).unwrap();
            f.replace(&bytes);
            assert!(f.open().is_err());
            assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        }
        for invalid in [
            serde_json::json!({"type":"Tag","tag":"a","scope":"one"}),
            serde_json::json!({"type":"Tag","tag":"","scope":""}),
            serde_json::json!({"type":"Tag","tag":"a/","scope":""}),
            serde_json::json!({"type":"Tag","tag":"A","scope":""}),
            serde_json::json!({"type":"Tag","tag":"a".repeat(crate::tabs::TAG_MAX_BYTES + 1),"scope":""}),
            serde_json::json!({"type":"Unknown","tag":"a","scope":""}),
            serde_json::json!({"type":"Tag","tag":"a","scope":"","extra":true}),
        ] {
            let mut bad = value.clone();
            bad["history"]["routes"][1] = invalid;
            let bytes = serde_json::to_vec(&bad).unwrap();
            f.replace(&bytes);
            assert!(f.open().is_err());
            assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        }
        let mut bad = value;
        bad["tabs"][0]["kind"] = serde_json::json!({"type":"Tag","tag":"a","scope":""});
        f.replace(&serde_json::to_vec(&bad).unwrap());
        assert!(f.open().is_err());
    }

    #[test]
    fn tag_cannot_be_saved_as_tab_and_bad_history_leaves_snapshot_untouched() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        store.save(&tabs()).unwrap();
        let original = std::fs::read(f.state()).unwrap();
        let tag = TabKey::Tag {
            tag: "a".into(),
            scope: "".into(),
        };
        assert!(
            store
                .save(&[Tab {
                    key: tag,
                    title: "tag".into()
                }])
                .is_err()
        );
        for (tag, scope) in [("a", "one"), ("", ""), ("a/", ""), ("A", "")] {
            let invalid = crate::navigation::Snapshot {
                routes: vec![TabKey::Tag {
                    tag: tag.into(),
                    scope: scope.into(),
                }],
                cursor: 0,
            };
            assert!(store.save_with_history(&tabs(), &invalid).is_err());
        }
        assert_eq!(std::fs::read(f.state()).unwrap(), original);
    }

    #[test]
    fn scope_v3_roundtrip_and_invalid_snapshots_preserve_bytes() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        let history = crate::navigation::Snapshot::default();
        store.save_with_scope(&tabs(), &history, "two").unwrap();
        let before = std::fs::read(f.state()).unwrap();
        assert!(store.save_with_scope(&tabs(), &history, "unknown").is_err());
        assert!(
            store
                .save_with_scope(&tabs(), &history, &"x".repeat(1025))
                .is_err()
        );
        assert_eq!(std::fs::read(f.state()).unwrap(), before);
        drop(store);
        let (store, _) = f.open().unwrap();
        assert_eq!(store.restored_scope.as_deref(), Some("two"));
        assert_eq!(std::fs::read(f.state()).unwrap(), before);
        drop(store);
        let original: serde_json::Value = serde_json::from_slice(&before).unwrap();
        for scope in [
            serde_json::Value::Null,
            42.into(),
            "unknown".into(),
            "two\0".into(),
            "x".repeat(1025).into(),
        ] {
            let mut bad = original.clone();
            bad["working_scope"] = scope;
            let bytes = serde_json::to_vec(&bad).unwrap();
            f.replace(&bytes);
            assert!(f.open().is_err());
            assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        }
        let mut missing = original.clone();
        missing.as_object_mut().unwrap().remove("working_scope");
        f.replace(&serde_json::to_vec(&missing).unwrap());
        assert!(f.open().is_err());
        for version in [1, 2] {
            let mut legacy = original.clone();
            legacy["version"] = version.into();
            legacy.as_object_mut().unwrap().remove("reading_positions");
            if version == 1 {
                legacy.as_object_mut().unwrap().remove("history");
            }
            f.replace(&serde_json::to_vec(&legacy).unwrap());
            assert!(f.open().is_err()); // Legacy cannot smuggle a scope field.
            legacy.as_object_mut().unwrap().remove("working_scope");
            let bytes = serde_json::to_vec(&legacy).unwrap();
            f.replace(&bytes);
            let (mut store, entries) = f.open().unwrap();
            assert!(store.restored_scope.is_none());
            assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
            store.save_with_scope(&entries, &history, "one").unwrap();
            assert_eq!(
                serde_json::from_slice::<serde_json::Value>(&std::fs::read(f.state()).unwrap())
                    .unwrap()["version"],
                4
            );
        }
        let duplicate = String::from_utf8(before).unwrap().replacen(
            "\"working_scope\":",
            "\"working_scope\":\"one\",\"working_scope\":",
            1,
        );
        f.replace(duplicate.as_bytes());
        assert!(f.open().is_err());
        assert_eq!(std::fs::read(f.state()).unwrap(), duplicate.as_bytes());
    }

    #[test]
    fn history_v2_roundtrip_and_strict_legacy_migration() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        let mut history = crate::navigation::History::default();
        history.record(Some(TabKey::Note {
            vault: "two".into(),
            note_id: 7,
        }));
        store
            .save_with_history(&tabs(), &history.snapshot())
            .unwrap();
        drop(store);
        let before = std::fs::read(f.state()).unwrap();
        let (store, _) = f.open().unwrap();
        assert_eq!(store.restored_history, Some(history.snapshot()));
        assert_eq!(std::fs::read(f.state()).unwrap(), before);
        drop(store);
        let mut legacy: serde_json::Value = serde_json::from_slice(&before).unwrap();
        legacy["version"] = 1.into();
        legacy.as_object_mut().unwrap().remove("reading_positions");
        legacy.as_object_mut().unwrap().remove("working_scope");
        legacy.as_object_mut().unwrap().remove("history");
        let legacy = serde_json::to_vec(&legacy).unwrap();
        f.replace(&legacy);
        let (mut store, restored) = f.open().unwrap();
        assert!(store.restored_history.is_none());
        assert_eq!(std::fs::read(f.state()).unwrap(), legacy);
        store
            .save_with_history(&restored, &history.snapshot())
            .unwrap();
        let saved: serde_json::Value =
            serde_json::from_slice(&std::fs::read(f.state()).unwrap()).unwrap();
        assert_eq!(saved["version"], 4);
        assert_eq!(saved["history"]["routes"][1]["vault"], "two");
    }
    #[test]
    fn invalid_history_save_preserves_previous_snapshot() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        store.save(&tabs()).unwrap();
        let before = std::fs::read(f.state()).unwrap();
        for history in [
            crate::navigation::Snapshot {
                routes: vec![],
                cursor: 0,
            },
            crate::navigation::Snapshot {
                routes: vec![TabKey::Home],
                cursor: 1,
            },
            crate::navigation::Snapshot {
                routes: vec![TabKey::Home; 101],
                cursor: 0,
            },
            crate::navigation::Snapshot {
                routes: vec![TabKey::Note {
                    vault: "missing".into(),
                    note_id: 7,
                }],
                cursor: 0,
            },
        ] {
            assert!(store.save_with_history(&tabs(), &history).is_err());
            assert_eq!(std::fs::read(f.state()).unwrap(), before);
        }
    }
    #[test]
    fn invalid_history_schema_routes_and_cursor_preserve_artifact() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        store.save(&tabs()).unwrap();
        drop(store);
        let original: serde_json::Value =
            serde_json::from_slice(&std::fs::read(f.state()).unwrap()).unwrap();
        let duplicate = serde_json::to_string(&original)
            .unwrap()
            .replace("\"cursor\":0", "\"cursor\":0,\"cursor\":0");
        f.replace(duplicate.as_bytes());
        assert!(f.open().is_err());
        assert_eq!(std::fs::read(f.state()).unwrap(), duplicate.as_bytes());
        let invalid = [
            serde_json::json!(null),
            serde_json::json!({"routes":[],"cursor":0}),
            serde_json::json!({"routes":[{"type":"Home"}],"cursor":1}),
            serde_json::json!({"routes":[{"type":"Note","vault":"missing","note_id":7}],"cursor":0}),
            serde_json::json!({"routes":[{"type":"Note","vault":"one","note_id":0}],"cursor":0}),
            serde_json::json!({"routes":[{"type":"Home","body":"private"}],"cursor":0}),
            serde_json::json!({"routes":[{"type":"Home"}],"cursor":0,"draft":"private"}),
            serde_json::json!({"routes":vec![serde_json::json!({"type":"Home"});101],"cursor":0}),
        ];
        for history in invalid {
            let mut value = original.clone();
            value["history"] = history;
            let bytes = serde_json::to_vec(&value).unwrap();
            f.replace(&bytes);
            assert!(
                f.open().is_err(),
                "accepted {}",
                String::from_utf8_lossy(&bytes)
            );
            assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        }
        let mut value = original;
        value["version"] = 1.into();
        value["history"] = serde_json::Value::Null;
        let bytes = serde_json::to_vec(&value).unwrap();
        f.replace(&bytes);
        assert!(f.open().is_err());
        assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
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
            assert!(
                f.open().is_err(),
                "accepted {}",
                String::from_utf8_lossy(&bytes)
            );
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
    fn checkpoint(vault: &str) -> ReadingPosition {
        ReadingPosition {
            vault: vault.into(),
            note_id: 7,
            etag: "revision-A".into(),
            body_bytes: 42,
            renderer: "checkpoint-v1:clean-fixture-build".into(),
            block_index: 4095,
            geometry: PositionGeometry {
                viewport_width: 800.,
                viewport_height: 600.,
                reader_width: 700.,
                reader_height: 500.,
                scale: 2.,
            },
        }
    }
    #[test]
    fn positions_roundtrip_preserved_and_orphans_removed_without_startup_rewrite() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        let positions = vec![checkpoint("one"), checkpoint("two")];
        store
            .save_with_positions(
                &tabs(),
                &crate::navigation::Snapshot::default(),
                "one",
                &positions,
            )
            .unwrap();
        drop(store);
        let bytes = std::fs::read(f.state()).unwrap();
        let (mut store, entries) = f.open().unwrap();
        assert_eq!(store.reading_positions, positions);
        assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        store
            .save_with_scope(&entries, &crate::navigation::Snapshot::default(), "two")
            .unwrap();
        assert_eq!(store.reading_positions, positions);
        store
            .save_with_scope(
                &entries[1..],
                &crate::navigation::Snapshot::default(),
                "two",
            )
            .unwrap();
        assert_eq!(store.reading_positions, vec![checkpoint("two")]);
        drop(store);
        assert_eq!(
            f.open().unwrap().0.reading_positions,
            vec![checkpoint("two")]
        );
    }
    #[test]
    fn positions_invalid_records_preserve_artifact() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        store
            .save_with_positions(
                &tabs(),
                &crate::navigation::Snapshot::default(),
                "one",
                &[checkpoint("one")],
            )
            .unwrap();
        let bytes = std::fs::read(f.state()).unwrap();
        let mut bad = checkpoint("one");
        bad.geometry.scale = f64::NAN;
        assert!(
            store
                .save_with_positions(
                    &tabs(),
                    &crate::navigation::Snapshot::default(),
                    "one",
                    &[bad]
                )
                .is_err()
        );
        assert_eq!(std::fs::read(f.state()).unwrap(), bytes);
        drop(store);
        let value: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
        for (field, invalid) in [
            ("vault", serde_json::json!("unknown")),
            ("note_id", serde_json::json!(-1)),
            ("etag", serde_json::json!("")),
            ("renderer", serde_json::json!("")),
            ("block_index", serde_json::json!(4096)),
            ("body_bytes", serde_json::json!(67108865)),
            (
                "geometry",
                serde_json::json!({"viewport_width":0,"viewport_height":600,"reader_width":700,"reader_height":500,"scale":2}),
            ),
        ] {
            let mut bad = value.clone();
            bad["reading_positions"][0][field] = invalid;
            let bad = serde_json::to_vec(&bad).unwrap();
            f.replace(&bad);
            assert!(f.open().is_err(), "{field}");
            assert_eq!(std::fs::read(f.state()).unwrap(), bad);
        }
        for count in [2, 129] {
            let mut bad = value.clone();
            bad["reading_positions"] = serde_json::json!(vec![checkpoint("one"); count]);
            let bad = serde_json::to_vec(&bad).unwrap();
            f.replace(&bad);
            assert!(f.open().is_err());
            assert_eq!(std::fs::read(f.state()).unwrap(), bad);
        }
        for version in [1, 2, 3] {
            let mut legacy = value.clone();
            legacy["version"] = version.into();
            legacy.as_object_mut().unwrap().remove("reading_positions");
            if version < 3 {
                legacy.as_object_mut().unwrap().remove("working_scope");
            }
            if version == 1 {
                legacy.as_object_mut().unwrap().remove("history");
            }
            let legacy = serde_json::to_vec(&legacy).unwrap();
            f.replace(&legacy);
            assert!(f.open().unwrap().0.reading_positions.is_empty());
            assert_eq!(std::fs::read(f.state()).unwrap(), legacy);
        }
    }
    #[test]
    fn oversized_position_payload_is_not_truncated_or_written() {
        let f = Fixture::new();
        let (mut store, _) = f.open().unwrap();
        store.save(&tabs()).unwrap();
        let before = std::fs::read(f.state()).unwrap();
        let mut entries = vec![];
        let mut positions = vec![];
        for id in 1..=128 {
            let mut p = checkpoint("one");
            p.note_id = id;
            p.etag = "e".repeat(4096);
            entries.push(Tab {
                key: p.key(),
                title: "bounded".into(),
            });
            positions.push(p);
        }
        assert!(
            store
                .save_with_positions(
                    &entries,
                    &crate::navigation::Snapshot::default(),
                    "one",
                    &positions
                )
                .is_err()
        );
        assert_eq!(std::fs::read(f.state()).unwrap(), before);
        assert!(store.reading_positions.is_empty());
    }
}
