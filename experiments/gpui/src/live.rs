//! Owned, read-only loopback API. Every operation (including Drop) belongs on a
//! background worker, never the GPUI event thread. No requests mutate note data.
use serde::{Deserialize, de::DeserializeOwned};
use serde_json::Value;
use std::{
    collections::{BTreeMap, VecDeque},
    fmt,
    io::Read,
    net::TcpListener,
    path::PathBuf,
    process::{Child, Command, Stdio},
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, AtomicU64, Ordering},
    },
    thread,
    time::{Duration, Instant},
};

const BODY_LIMIT: u64 = 16 * 1024 * 1024;
const LOG_LIMIT: usize = 16 * 1024;
static NEXT_LAUNCH: AtomicU64 = AtomicU64::new(1);
type Fields = BTreeMap<String, Value>;

#[derive(Debug)]
pub enum Error {
    Config(String),
    Io(std::io::Error),
    Transport(String),
    Http { status: u16, body: String },
    Json(String),
    Shape(String),
    Oversized,
    Startup(String),
    Exited(String),
    Stale,
    Shutdown(String),
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for Error {}
impl From<std::io::Error> for Error {
    fn from(e: std::io::Error) -> Self {
        Self::Io(e)
    }
}

#[derive(Clone, Debug)]
pub enum Vault {
    Path(PathBuf),
    Registered(String),
}
#[derive(Clone, Debug)]
pub struct LaunchConfig {
    pub executable: PathBuf,
    pub vault: Vault,
    pub config: Option<PathBuf>,
    pub cache: Option<PathBuf>,
    pub port: u16,
    pub startup_timeout: Duration,
    pub request_timeout: Duration,
}
impl LaunchConfig {
    pub fn new(executable: PathBuf, vault: Vault, port: u16) -> Self {
        Self {
            executable,
            vault,
            port,
            config: None,
            cache: None,
            startup_timeout: Duration::from_secs(20),
            request_timeout: Duration::from_secs(3),
        }
    }
    fn validate(&self) -> Result<(), Error> {
        for (name, path) in [
            ("executable", Some(&self.executable)),
            ("config", self.config.as_ref()),
            ("cache", self.cache.as_ref()),
            ("vault", match &self.vault {
                Vault::Path(p) => Some(p),
                _ => None,
            }),
        ] {
            if path.is_some_and(|p| !p.is_absolute()) {
                return Err(Error::Config(format!("{name} must be absolute")));
            }
        }
        if matches!(&self.vault, Vault::Registered(n) if n.trim().is_empty()) {
            return Err(Error::Config("vault name is empty".into()));
        }
        if self.port == 0
            || self.port == u16::MAX
            || self.startup_timeout.is_zero()
            || self.request_timeout.is_zero()
        {
            return Err(Error::Config("invalid port or timeout".into()));
        }
        Ok(())
    }
}

fn nullable_vec<'de, D: serde::Deserializer<'de>, T: Deserialize<'de>>(
    d: D,
) -> Result<Vec<T>, D::Error> {
    Ok(Option::<Vec<T>>::deserialize(d)?.unwrap_or_default())
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct NoteKey {
    pub vault: String,
    pub note_id: i64,
}
#[derive(Clone, Debug, Deserialize)]
pub struct Note {
    pub note_id: i64,
    pub title: String,
    pub file_kind: String,
    pub path: String,
    #[serde(default)]
    pub vault: String,
    #[serde(default, deserialize_with = "nullable_vec")]
    pub tags: Vec<String>,
    #[serde(default, deserialize_with = "nullable_vec")]
    pub days: Vec<String>,
    #[serde(default, deserialize_with = "nullable_vec")]
    pub flags: Vec<String>,
    #[serde(flatten)]
    pub additional: Fields,
}
impl Note {
    pub fn key(&self) -> NoteKey {
        NoteKey {
            vault: self.vault.clone(),
            note_id: self.note_id,
        }
    }
}
#[derive(Clone, Debug, Deserialize)]
pub struct Notes {
    pub notes: Vec<Note>,
    #[serde(flatten)]
    pub diagnostics: Fields,
}
#[derive(Clone, Debug, Deserialize)]
pub struct Search {
    pub results: Vec<Note>,
    #[serde(flatten)]
    pub diagnostics: Fields,
}
#[derive(Clone, Debug, Deserialize)]
pub struct NoteBody {
    #[serde(flatten)]
    pub note: Note,
    pub body: String,
    pub etag: String,
    #[serde(default)]
    pub read_only: bool,
    #[serde(default)]
    pub props: Value,
}
#[derive(Clone, Debug, Deserialize)]
pub struct NoteResponse {
    pub note: NoteBody,
    #[serde(flatten)]
    pub related_and_diagnostics: Fields,
}
#[derive(Clone, Debug, Deserialize)]
pub struct Storage {
    pub phase: String,
    pub read_only: bool,
    pub verified: u64,
    pub generation: u64,
    #[serde(default)]
    pub reason: Option<String>,
    #[serde(default)]
    pub home_note_id: Option<String>,
    #[serde(flatten)]
    pub additional: Fields,
}

#[derive(Clone)]
pub struct ReadClient {
    http: reqwest::blocking::Client,
    origin: String,
    alive: Arc<AtomicBool>,
    pub launch_generation: u64,
}
impl ReadClient {
    fn get<T: DeserializeOwned>(&self, path: &str, query: &[(&str, String)]) -> Result<T, Error> {
        self.get_with_timeout(path, query, None)
    }
    fn get_with_timeout<T: DeserializeOwned>(
        &self,
        path: &str,
        query: &[(&str, String)],
        timeout: Option<Duration>,
    ) -> Result<T, Error> {
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        let mut request = self
            .http
            .get(format!("{}{path}", self.origin))
            .query(query)
            .header("Cache-Control", "no-cache, no-store");
        if let Some(timeout) = timeout {
            request = request.timeout(timeout);
        }
        let mut response = request
            .send()
            .map_err(|e| Error::Transport(e.to_string()))?;
        let status = response.status().as_u16();
        let mut bytes = Vec::new();
        (&mut response)
            .take(BODY_LIMIT + 1)
            .read_to_end(&mut bytes)
            .map_err(|e| Error::Transport(e.to_string()))?;
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        if bytes.len() as u64 > BODY_LIMIT {
            return Err(Error::Oversized);
        }
        if status != 200 {
            return Err(Error::Http {
                status,
                body: String::from_utf8_lossy(&bytes).into_owned(),
            });
        }
        let value: Value =
            serde_json::from_slice(&bytes).map_err(|e| Error::Json(e.to_string()))?;
        serde_json::from_value(value).map_err(|e| Error::Shape(e.to_string()))
    }
    pub fn vaults(&self) -> Result<Value, Error> {
        self.vaults_with_timeout(None)
    }
    fn vaults_with_timeout(&self, timeout: Option<Duration>) -> Result<Value, Error> {
        let v: Value = self.get_with_timeout("/api/vaults", &[], timeout)?;
        if !v.get("active").is_some_and(Value::is_object)
            || !v.get("vaults").is_some_and(Value::is_array)
        {
            return Err(Error::Shape(
                "vaults requires active object and vaults array".into(),
            ));
        }
        Ok(v)
    }
    pub fn storage(&self) -> Result<Storage, Error> {
        self.get("/api/storage", &[])
    }
    pub fn notes(&self, vault: &str) -> Result<Notes, Error> {
        let mut data: Notes = self.get("/api/notes", &vault_query(vault))?;
        for note in &mut data.notes {
            if note.vault.is_empty() {
                note.vault = vault.into();
            }
        }
        Ok(data)
    }
    /// Empty vault label deliberately means workspace-wide search, NOT active-vault-only.
    pub fn search(&self, query: &str, vault: &str) -> Result<Search, Error> {
        let mut args = vault_query(vault);
        args.extend([("q", query.into()), ("limit", "50".into())]);
        let mut data: Search = self.get("/api/search", &args)?;
        for note in &mut data.results {
            if note.vault.is_empty() {
                note.vault = vault.into();
            }
        }
        Ok(data)
    }
    pub fn local_graph(&self, key: &NoteKey) -> Result<(crate::graph::Graph, Fields), Error> {
        let mut args = vault_query(&key.vault);
        args.push(("id", key.note_id.to_string()));
        let value: Value = self.get("/api/graph/local", &args)?;
        if value
            .get("vault")
            .and_then(Value::as_str)
            .is_some_and(|vault| vault != key.vault)
        {
            return Err(Error::Shape("graph vault identity mismatch".into()));
        }
        let response: crate::graph::GraphResponse =
            serde_json::from_value(value.clone()).map_err(|e| Error::Shape(e.to_string()))?;
        if response.graph.center_id != key.note_id {
            return Err(Error::Shape("graph center identity mismatch".into()));
        }
        let mut diagnostics: Fields =
            serde_json::from_value(value).map_err(|e| Error::Shape(e.to_string()))?;
        diagnostics.remove("graph");
        Ok((response.graph, diagnostics))
    }

    pub fn note(&self, key: &NoteKey) -> Result<NoteResponse, Error> {
        let mut args = vault_query(&key.vault);
        args.push(("id", key.note_id.to_string()));
        let mut data: NoteResponse = self.get("/api/note", &args)?;
        if data.note.note.note_id != key.note_id {
            return Err(Error::Shape("note identity mismatch".into()));
        }
        if data.note.note.vault.is_empty() {
            data.note.note.vault = key.vault.clone();
        }
        if data.note.note.vault != key.vault && !key.vault.is_empty() {
            return Err(Error::Shape("vault identity mismatch".into()));
        }
        Ok(data)
    }
}
fn vault_query(vault: &str) -> Vec<(&'static str, String)> {
    if vault.is_empty() {
        vec![]
    } else {
        vec![("vault", vault.into())]
    }
}

/// Ticket includes navigation plus server/storage epochs. Compare before publishing worker results.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RequestTicket {
    pub launch: u64,
    pub navigation: u64,
    pub storage: u64,
}

pub struct OwnedServer {
    child: Option<Child>,
    client: ReadClient,
    logs: Arc<Mutex<VecDeque<u8>>>,
    pub initial_vaults: Value,
    pub initial_storage: Option<Storage>,
    pub storage_error: Option<String>,
}
impl OwnedServer {
    pub fn start(config: LaunchConfig) -> Result<Self, Error> {
        Self::start_cancelable(config, &AtomicBool::new(false))
    }
    fn start_cancelable(config: LaunchConfig, canceled: &AtomicBool) -> Result<Self, Error> {
        config.validate()?;
        if canceled.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        // Preflight both listeners; still require the child token after release to defeat TOCTOU adoption.
        let first = TcpListener::bind(("127.0.0.1", config.port))?;
        let second = TcpListener::bind(("127.0.0.1", config.port + 1))?;
        let mut entropy = [0u8; 32];
        std::fs::File::open("/dev/urandom")?.read_exact(&mut entropy)?;
        let token = entropy
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect::<String>();
        let http = reqwest::blocking::Client::builder()
            .no_proxy()
            .redirect(reqwest::redirect::Policy::none())
            .timeout(config.request_timeout.min(config.startup_timeout))
            .build()
            .map_err(|e| Error::Transport(e.to_string()))?;
        let alive = Arc::new(AtomicBool::new(true));
        let client = ReadClient {
            http,
            origin: format!("http://127.0.0.1:{}", config.port),
            alive,
            launch_generation: NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed),
        };
        let mut cmd = Command::new(&config.executable);
        cmd.env_remove("TRACK_VAULT")
            .env_remove("TRACK_CONFIG")
            .env_remove("TRACK_CACHE_DIR");
        match &config.vault {
            Vault::Path(p) => {
                cmd.env("TRACK_VAULT", p);
            }
            Vault::Registered(n) => {
                cmd.args(["--vault", n]);
            }
        }
        if let Some(p) = &config.config {
            cmd.env("TRACK_CONFIG", p);
        }
        if let Some(p) = &config.cache {
            cmd.env("TRACK_CACHE_DIR", p);
        }
        cmd.args(["web", "--addr", &format!("127.0.0.1:{}", config.port)])
            .env(
                "TRACK_WEB_DESKTOP_PARENT_PID",
                std::process::id().to_string(),
            )
            .env("TRACK_WEB_DESKTOP_READY_TOKEN", &token)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        drop((first, second));
        let mut child = cmd.spawn()?;
        let logs = Arc::new(Mutex::new(VecDeque::new()));
        let ready = Arc::new(AtomicBool::new(false));
        drain(
            child.stdout.take().unwrap(),
            logs.clone(),
            None,
            ready.clone(),
        );
        drain(
            child.stderr.take().unwrap(),
            logs.clone(),
            Some(format!("TRACK_DESKTOP_READY:{token}")),
            ready.clone(),
        );
        let mut owner = Self {
            child: Some(child),
            client,
            logs,
            initial_vaults: Value::Null,
            initial_storage: None,
            storage_error: None,
        };
        let start = Instant::now();
        let mut last = String::new();
        while start.elapsed() < config.startup_timeout {
            if canceled.load(Ordering::Acquire) {
                owner.shutdown()?;
                return Err(Error::Stale);
            }
            owner.check_running()?;
            if ready.load(Ordering::Acquire) {
                match owner.client.vaults_with_timeout(Some(
                    config
                        .startup_timeout
                        .saturating_sub(start.elapsed())
                        .max(Duration::from_millis(1))
                        .min(config.request_timeout)
                        .min(Duration::from_secs(3)),
                )) {
                    Ok(v) => {
                        owner.check_running()?;
                        owner.initial_vaults = v;
                        match owner.client.get_with_timeout(
                            "/api/storage",
                            &[],
                            Some(
                                config
                                    .startup_timeout
                                    .saturating_sub(start.elapsed())
                                    .max(Duration::from_millis(1))
                                    .min(config.request_timeout)
                                    .min(Duration::from_secs(3)),
                            ),
                        ) {
                            Ok(s) => owner.initial_storage = Some(s),
                            Err(e) => owner.storage_error = Some(e.to_string()),
                        }
                        owner.check_running()?;
                        if canceled.load(Ordering::Acquire) {
                            owner.shutdown()?;
                            return Err(Error::Stale);
                        }
                        return Ok(owner);
                    }
                    Err(e) => last = e.to_string(),
                }
            }
            thread::sleep(Duration::from_millis(20));
        }
        owner.shutdown()?;
        Err(Error::Startup(format!(
            "readiness deadline exceeded; {last}\n{}",
            owner.log_tail()
        )))
    }
    pub fn client(&self) -> ReadClient {
        self.client.clone()
    }
    pub fn log_tail(&self) -> String {
        String::from_utf8_lossy(
            &self
                .logs
                .lock()
                .unwrap()
                .iter()
                .copied()
                .collect::<Vec<_>>(),
        )
        .into_owned()
    }
    pub fn check_running(&mut self) -> Result<(), Error> {
        let Some(child) = &mut self.child else {
            return Err(Error::Stale);
        };
        if let Some(status) = child.try_wait()? {
            self.client.alive.store(false, Ordering::Release);
            self.child.take();
            return Err(Error::Exited(format!("{status}: {}", self.log_tail())));
        }
        Ok(())
    }
    /// Returns success only once the exact owned child was reaped. Do not restart after an error.
    pub fn shutdown(&mut self) -> Result<(), Error> {
        self.client.alive.store(false, Ordering::Release);
        let Some(child) = &mut self.child else {
            return Ok(());
        };
        if child.try_wait()?.is_some() {
            self.child.take();
            return Ok(());
        }
        let pid = child.id();
        if pid <= 1 || pid > i32::MAX as u32 {
            return Err(Error::Shutdown("unsafe child pid".into()));
        }
        // A child not reaped by this handle retains its PID; never signal by port/name.
        unsafe {
            libc::kill(pid as i32, libc::SIGTERM);
        }
        let deadline = Instant::now() + Duration::from_millis(700);
        while Instant::now() < deadline {
            if child.try_wait()?.is_some() {
                self.child.take();
                return Ok(());
            }
            thread::sleep(Duration::from_millis(10));
        }
        child.kill()?;
        let deadline = Instant::now() + Duration::from_secs(2);
        while Instant::now() < deadline {
            if child.try_wait()?.is_some() {
                self.child.take();
                return Ok(());
            }
            thread::sleep(Duration::from_millis(10));
        }
        Err(Error::Shutdown(
            "owned child has not reaped; replacement prohibited".into(),
        ))
    }
}
impl Drop for OwnedServer {
    fn drop(&mut self) {
        let _ = self.shutdown();
        // On pathological uninterruptible kernel IO, retain ownership in a reaper rather than block UI forever.
        if let Some(mut child) = self.child.take() {
            thread::spawn(move || {
                let _ = child.kill();
                let _ = child.wait();
            });
        }
    }
}

/// Start on a background task; clone/drop anywhere. The final handle drop only
/// disconnects a channel. Its dedicated owner thread performs bounded cleanup.
#[derive(Clone)]
pub struct Session {
    client: ReadClient,
    pub initial_vaults: Value,
    pub initial_storage: Option<Storage>,
    pub storage_error: Option<String>,
    status: Arc<Mutex<Option<String>>>,
    shutdown_done: Arc<AtomicBool>,
    _lifetime: std::sync::mpsc::Sender<()>,
}
impl Session {
    pub fn start(config: LaunchConfig) -> Result<Self, Error> {
        Self::start_cancelable(config, &AtomicBool::new(false))
    }
    fn start_cancelable(config: LaunchConfig, canceled: &AtomicBool) -> Result<Self, Error> {
        let server = OwnedServer::start_cancelable(config, canceled)?;
        Ok(Self::from_owned(server))
    }
    fn from_owned(mut server: OwnedServer) -> Self {
        let (tx, rx) = std::sync::mpsc::channel();
        let status = Arc::new(Mutex::new(None));
        let shutdown_done = Arc::new(AtomicBool::new(false));
        let session = Self {
            client: server.client(),
            initial_vaults: server.initial_vaults.clone(),
            initial_storage: server.initial_storage.clone(),
            storage_error: server.storage_error.clone(),
            status: status.clone(),
            shutdown_done: shutdown_done.clone(),
            _lifetime: tx,
        };
        thread::spawn(move || {
            loop {
                match rx.recv_timeout(Duration::from_millis(50)) {
                    Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                        if let Err(error) = server.check_running() {
                            *status.lock().unwrap() = Some(error.to_string());
                            break;
                        }
                    }
                    _ => break,
                }
            }
            match server.shutdown() {
                Ok(()) => shutdown_done.store(true, Ordering::Release),
                Err(error) => *status.lock().unwrap() = Some(error.to_string()),
            }
        });
        session
    }
    pub fn client(&self) -> ReadClient {
        self.client.clone()
    }
    pub fn failure(&self) -> Option<String> {
        self.status.lock().unwrap().clone()
    }
    /// Background-only barrier for retries. Never start a replacement if this fails.
    pub fn shutdown(&self) -> Result<(), Error> {
        self.client.alive.store(false, Ordering::Release);
        let _ = self._lifetime.send(());
        let deadline = Instant::now() + Duration::from_secs(4);
        while Instant::now() < deadline {
            if self.shutdown_done.load(Ordering::Acquire) {
                return Ok(());
            }
            thread::sleep(Duration::from_millis(10));
        }
        Err(Error::Shutdown(self.failure().unwrap_or_else(|| {
            "owned process cleanup not confirmed".into()
        })))
    }
}

/// Shared startup/quit ownership exists before the worker creates a child.
/// Its mutex is never held across process startup, HTTP, or shutdown.
#[derive(Default)]
pub struct LifecycleHandle {
    canceled: AtomicBool,
    state: Mutex<LifecycleState>,
}
#[derive(Default)]
struct LifecycleState {
    starting: bool,
    session: Option<Session>,
    failure: Option<String>,
    cleanup_unconfirmed: bool,
}
impl LifecycleHandle {
    pub fn begin_start(&self) -> Result<(), Error> {
        let mut state = self.state.lock().unwrap();
        if state.starting || state.session.is_some() || self.canceled.load(Ordering::Acquire) {
            return Err(Error::Config(
                "server lifecycle already started or closing".into(),
            ));
        }
        state.starting = true;
        Ok(())
    }
    /// Call on a worker after begin_start. Tracks ownership even before readiness.
    pub fn start(&self, config: LaunchConfig) -> Result<Session, Error> {
        let result = Session::start_cancelable(config, &self.canceled);
        let mut state = self.state.lock().unwrap();
        state.starting = false;
        match &result {
            Ok(session) => state.session = Some(session.clone()),
            Err(error) => {
                state.failure = Some(error.to_string());
                state.cleanup_unconfirmed = matches!(error, Error::Shutdown(_));
            }
        }
        result
    }
    pub fn failure(&self) -> Option<String> {
        let state = self.state.lock().unwrap();
        state
            .failure
            .clone()
            .or_else(|| state.session.as_ref().and_then(Session::failure))
    }
    pub fn is_closing(&self) -> bool {
        self.canceled.load(Ordering::Acquire)
    }
    /// Background-only quit barrier. Cancellation interrupts startup before another probe.
    pub fn shutdown(&self) -> Result<(), Error> {
        self.canceled.store(true, Ordering::Release);
        let deadline = Instant::now() + Duration::from_secs(10);
        loop {
            let (starting, session) = {
                let state = self.state.lock().unwrap();
                if state.cleanup_unconfirmed {
                    return Err(Error::Shutdown(state.failure.clone().unwrap_or_default()));
                }
                (state.starting, state.session.clone())
            };
            if let Some(session) = session {
                return session.shutdown().inspect_err(|error| {
                    self.state.lock().unwrap().failure = Some(error.to_string());
                });
            }
            if !starting {
                return Ok(());
            }
            if Instant::now() >= deadline {
                let message =
                    "startup cancellation cleanup is not confirmed; application remains open"
                        .to_owned();
                self.state.lock().unwrap().failure = Some(message.clone());
                return Err(Error::Shutdown(message));
            }
            thread::sleep(Duration::from_millis(10));
        }
    }
}

fn drain(
    mut pipe: impl Read + Send + 'static,
    logs: Arc<Mutex<VecDeque<u8>>>,
    marker: Option<String>,
    ready: Arc<AtomicBool>,
) {
    thread::spawn(move || {
        let mut chunk = [0u8; 4096];
        let mut line = Vec::new();
        let mut overlong = false;
        while let Ok(n) = pipe.read(&mut chunk) {
            if n == 0 {
                break;
            }
            for &byte in &chunk[..n] {
                if byte == b'\n' {
                    if !overlong {
                        let text = String::from_utf8_lossy(&line);
                        if marker
                            .as_ref()
                            .is_some_and(|m| text.trim_end_matches('\r') == m)
                        {
                            ready.store(true, Ordering::Release);
                        } else if !text.starts_with("TRACK_DESKTOP_READY:") {
                            let mut tail = logs.lock().unwrap();
                            for &b in line.iter().chain(std::iter::once(&b'\n')) {
                                if tail.len() == LOG_LIMIT {
                                    tail.pop_front();
                                }
                                tail.push_back(b);
                            }
                        }
                    }
                    line.clear();
                    overlong = false;
                } else if line.len() < LOG_LIMIT {
                    line.push(byte);
                } else {
                    overlong = true;
                }
            }
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    fn monitored_fixture(seconds: &str) -> (Session, u32) {
        let child = Command::new("/bin/sleep").arg(seconds).spawn().unwrap();
        let pid = child.id();
        let server = OwnedServer {
            child: Some(child),
            client: ReadClient { http: reqwest::blocking::Client::new(), origin: "http://127.0.0.1:1".into(), alive: Arc::new(AtomicBool::new(true)), launch_generation: 1 },
            logs: Arc::new(Mutex::new(VecDeque::new())), initial_vaults: serde_json::json!({"active":{},"vaults":[]}),
            initial_storage: Some(serde_json::from_value(serde_json::json!({"phase":"initializing","read_only":true,"verified":0,"generation":1})).unwrap()), storage_error: None,
        };
        (Session::from_owned(server), pid)
    }
    #[test]
    fn quit_after_readiness_does_not_wait_for_storage_initialization() {
        let (session, pid) = monitored_fixture("30");
        let owner = LifecycleHandle::default();
        owner.state.lock().unwrap().session = Some(session.clone());
        assert_eq!(
            session.initial_storage.as_ref().unwrap().phase,
            "initializing"
        );
        owner.shutdown().unwrap();
        let mut status = 0;
        assert_eq!(
            unsafe { libc::waitpid(pid as i32, &mut status, libc::WNOHANG) },
            -1
        );
        assert_eq!(
            std::io::Error::last_os_error().raw_os_error(),
            Some(libc::ECHILD)
        );
        assert!(matches!(session.client().notes(""), Err(Error::Stale)));
    }
    #[test]
    fn idle_exit_reaches_lifecycle_observer_without_http_requests() {
        let (session, _) = monitored_fixture("0.05");
        let owner = LifecycleHandle::default();
        owner.state.lock().unwrap().session = Some(session.clone());
        let deadline = Instant::now() + Duration::from_secs(2);
        while owner.failure().is_none() && Instant::now() < deadline {
            thread::sleep(Duration::from_millis(10));
        }
        assert!(owner.failure().unwrap().contains("Exited"));
        assert!(matches!(session.client().notes(""), Err(Error::Stale)));
        owner.shutdown().unwrap();
    }
    #[test]
    fn lifecycle_cancel_before_spawn_and_errors_remain_observable() {
        let owner = LifecycleHandle::default();
        owner.begin_start().unwrap();
        owner.canceled.store(true, Ordering::Release);
        let config = LaunchConfig::new(
            "/missing/never-executed".into(),
            Vault::Path("/tmp/disposable".into()),
            23000,
        );
        assert!(matches!(owner.start(config), Err(Error::Stale)));
        assert!(owner.shutdown().is_ok());
        assert!(owner.failure().is_some());
        assert!(owner.begin_start().is_err());
        owner.state.lock().unwrap().cleanup_unconfirmed = true;
        assert!(matches!(owner.shutdown(), Err(Error::Shutdown(_))));
    }

    #[test]
    fn quit_during_startup_cancels_and_reaps_exact_child() {
        use std::os::unix::fs::PermissionsExt;
        let root = std::env::temp_dir().join(format!(
            "track-startup-cancel-{}-{}",
            std::process::id(),
            NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir_all(&root).unwrap();
        let executable = root.join("fixture");
        let pid_file = root.join("pid");
        std::fs::write(
            &executable,
            format!(
                "#!/bin/sh\nprintf '%s' \"$$\" > '{}'\nexec /bin/sleep 30\n",
                pid_file.display()
            ),
        )
        .unwrap();
        std::fs::set_permissions(&executable, std::fs::Permissions::from_mode(0o700)).unwrap();
        let port = (23000..25000)
            .find(|port| {
                TcpListener::bind(("127.0.0.1", *port)).is_ok()
                    && TcpListener::bind(("127.0.0.1", *port + 1)).is_ok()
            })
            .unwrap();
        let mut config = LaunchConfig::new(executable, Vault::Path(root.join("vault")), port);
        config.config = Some(root.join("config"));
        config.cache = Some(root.join("cache"));
        let owner = Arc::new(LifecycleHandle::default());
        owner.begin_start().unwrap();
        let worker_owner = owner.clone();
        let worker = thread::spawn(move || worker_owner.start(config));
        let deadline = Instant::now() + Duration::from_secs(3);
        while !pid_file.exists() && Instant::now() < deadline {
            thread::sleep(Duration::from_millis(10));
        }
        let started = Instant::now();
        owner.shutdown().unwrap();
        assert!(started.elapsed() < Duration::from_secs(4));
        assert!(matches!(worker.join().unwrap(), Err(Error::Stale)));
        let pid = std::fs::read_to_string(&pid_file)
            .unwrap()
            .parse::<i32>()
            .unwrap();
        let mut status = 0;
        assert_eq!(
            unsafe { libc::waitpid(pid, &mut status, libc::WNOHANG) },
            -1
        );
        assert_eq!(
            std::io::Error::last_os_error().raw_os_error(),
            Some(libc::ECHILD)
        );
        std::fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn local_graph_preserves_scope_and_partial_state() {
        let (client, request) = mock(
            200,
            r#"{"vault":"work","graph":{"center_id":42,"nodes":[],"edges":[]},"read_state":{"partial":true}}"#,
        );
        let (graph, fields) = client
            .local_graph(&NoteKey {
                vault: "work".into(),
                note_id: 42,
            })
            .unwrap();
        assert_eq!(graph.center_id, 42);
        assert!(graph.nodes.is_empty());
        assert_eq!(fields["read_state"]["partial"], true);
        let request = request.join().unwrap();
        assert!(request.contains("/api/graph/local?vault=work&id=42"));
        let (client, request) = mock(
            200,
            r#"{"vault":"other","graph":{"center_id":42,"nodes":[],"edges":[]}}"#,
        );
        assert!(
            client
                .local_graph(&NoteKey {
                    vault: "work".into(),
                    note_id: 42
                })
                .is_err()
        );
        request.join().unwrap();
    }

    #[test]
    fn null_metadata_and_diagnostics_survive() {
        let data: Notes = serde_json::from_str(r#"{"notes":[{"note_id":42,"title":"日本語","file_kind":"note","path":"x","tags":null,"days":null,"flags":null}],"read_state":{"partial":true},"storage_state":"partial"}"#).unwrap();
        assert!(data.notes[0].tags.is_empty());
        assert_eq!(data.notes[0].note_id, 42);
        assert_eq!(data.diagnostics["read_state"]["partial"], true);
    }
    #[test]
    fn body_preserves_source_and_unknown_metadata() {
        let data: NoteResponse = serde_json::from_str(r#"{"note":{"note_id":42,"title":"T","file_kind":"note","path":"x","body":"[[Link]]","etag":"opaque","read_only":true,"props":[{"future":1}],"custom":"retained"},"unavailable":[{"name":"bad"}]}"#).unwrap();
        assert_eq!(data.note.body, "[[Link]]");
        assert_eq!(data.note.note.additional["custom"], "retained");
        assert!(data.note.read_only);
    }
    #[test]
    fn malformed_shapes_do_not_become_empty_results() {
        assert!(serde_json::from_str::<Notes>("{}").is_err());
        assert!(serde_json::from_str::<Notes>(r#"{"notes":[{"note_id":"42"}]}"#).is_err());
    }
    #[test]
    fn marker_is_exact_split_safe_and_secret_not_logged() {
        let logs = Arc::new(Mutex::new(VecDeque::new()));
        let ready = Arc::new(AtomicBool::new(false));
        struct Split {
            bytes: std::io::Cursor<Vec<u8>>,
        }
        impl Read for Split {
            fn read(&mut self, b: &mut [u8]) -> std::io::Result<usize> {
                self.bytes.read(&mut b[..1])
            }
        }
        drain(
            Split {
                bytes: std::io::Cursor::new(
                    b"TRACK_DESKTOP_READY:wrong\nTRACK_DESKTOP_READY:secret\n".to_vec(),
                ),
            },
            logs.clone(),
            Some("TRACK_DESKTOP_READY:secret".into()),
            ready.clone(),
        );
        for _ in 0..100 {
            if ready.load(Ordering::Acquire) {
                break;
            }
            thread::sleep(Duration::from_millis(5));
        }
        assert!(ready.load(Ordering::Acquire));
        assert!(logs.lock().unwrap().is_empty());
    }
    #[test]
    fn selection_and_ports_are_explicit() {
        assert!(
            LaunchConfig::new("track".into(), Vault::Path("/tmp/v".into()), 23000)
                .validate()
                .is_err()
        );
        assert!(
            LaunchConfig::new("/bin/track".into(), Vault::Registered("".into()), 23000)
                .validate()
                .is_err()
        );
        assert!(
            LaunchConfig::new("/bin/track".into(), Vault::Path("relative".into()), 23000)
                .validate()
                .is_err()
        );
    }
    fn mock(status: u16, body: &'static str) -> (ReadClient, thread::JoinHandle<String>) {
        use std::io::Write;
        let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let task = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream
                .set_read_timeout(Some(Duration::from_secs(2)))
                .unwrap();
            let mut request = Vec::new();
            let mut byte = [0u8; 1];
            while !request.ends_with(b"\r\n\r\n") {
                stream.read_exact(&mut byte).unwrap();
                request.push(byte[0]);
            }
            write!(
                stream,
                "HTTP/1.1 {status} Test\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            )
            .unwrap();
            String::from_utf8(request).unwrap()
        });
        (
            ReadClient {
                http: reqwest::blocking::Client::builder()
                    .no_proxy()
                    .redirect(reqwest::redirect::Policy::none())
                    .timeout(Duration::from_secs(2))
                    .build()
                    .unwrap(),
                origin,
                alive: Arc::new(AtomicBool::new(true)),
                launch_generation: 1,
            },
            task,
        )
    }
    #[test]
    fn queries_are_encoded_and_requests_read_only() {
        let (client, task) = mock(200, r#"{"results":[],"unavailable":[{"name":"offline"}]}"#);
        assert_eq!(
            client
                .search("a & 日本語", "work & home")
                .unwrap()
                .diagnostics["unavailable"][0]["name"],
            "offline"
        );
        let request = task.join().unwrap();
        assert!(request.starts_with("GET /api/search?"));
        assert!(request.contains("vault=work+%26+home"));
        assert!(request.contains("q=a+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E"));
    }
    #[test]
    fn http_failures_and_html_are_not_empty_vaults() {
        for status in [400, 404, 503] {
            let (client, task) = mock(status, r#"{"error":"read unavailable"}"#);
            assert!(
                matches!(client.notes(""), Err(Error::Http { status: actual, body }) if actual == status && body.contains("unavailable"))
            );
            task.join().unwrap();
        }
        let (client, task) = mock(200, "<html>foreign</html>");
        assert!(matches!(client.vaults(), Err(Error::Json(_))));
        task.join().unwrap();
        let (client, task) = mock(200, "{}");
        assert!(matches!(client.vaults(), Err(Error::Shape(_))));
        task.join().unwrap();
    }
    #[test]
    fn stopped_owned_child_is_killed_and_reaped() {
        let child = Command::new("/bin/sleep").arg("30").spawn().unwrap();
        let pid = child.id();
        unsafe {
            assert_eq!(libc::kill(pid as i32, libc::SIGSTOP), 0);
        }
        thread::sleep(Duration::from_millis(50));
        let client = ReadClient {
            http: reqwest::blocking::Client::new(),
            origin: "http://127.0.0.1:1".into(),
            alive: Arc::new(AtomicBool::new(true)),
            launch_generation: 1,
        };
        let mut server = OwnedServer {
            child: Some(child),
            client,
            logs: Arc::new(Mutex::new(VecDeque::new())),
            initial_vaults: Value::Null,
            initial_storage: None,
            storage_error: None,
        };
        let start = Instant::now();
        server.shutdown().unwrap();
        assert!(start.elapsed() < Duration::from_secs(4));
        assert!(server.child.is_none());
        assert!(matches!(server.client.notes(""), Err(Error::Stale)));
    }
    #[test]
    fn occupied_port_is_not_adopted_or_killed() {
        let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
        let config = LaunchConfig::new(
            "/nonexistent/track".into(),
            Vault::Path("/tmp/disposable".into()),
            listener.local_addr().unwrap().port(),
        );
        assert!(matches!(OwnedServer::start(config), Err(Error::Io(_))));
        assert!(std::net::TcpStream::connect(listener.local_addr().unwrap()).is_ok());
    }
}
