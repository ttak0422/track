//! Owned loopback API. Blocking operations belong on a background worker.
//! Reads never mutate notes; save is one explicit existing-note PUT, never retried.
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
    Http {
        status: u16,
        body: String,
    },
    Json(String),
    Shape(String),
    Oversized,
    Startup(String),
    Exited(String),
    Stale,
    Shutdown(String),
    /// A prerequisite failed before a mutation request was sent.
    SaveNotSent(Box<Error>),
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SaveDisposition {
    NotSent,
    Conflict,
    Rejected,
    Uncertain,
}
impl Error {
    /// Classification is meaningful for errors returned by ReadClient::save.
    /// Conflict means HTTP 409, not necessarily a changed body (storage can conflict).
    pub fn save_disposition(&self) -> SaveDisposition {
        match self {
            Self::SaveNotSent(_) => SaveDisposition::NotSent,
            Self::Http { status: 409, .. } => SaveDisposition::Conflict,
            Self::Http {
                status: 400 | 403 | 404 | 405 | 503,
                ..
            } => SaveDisposition::Rejected,
            _ => SaveDisposition::Uncertain,
        }
    }
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
            (
                "vault",
                match &self.vault {
                    Vault::Path(p) => Some(p),
                    _ => None,
                },
            ),
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
/// Sparse per-day activity in the requested working vault, never a federated sum.
#[derive(Clone, Debug, Deserialize)]
pub struct ActivityCount {
    pub date: String,
    pub count: u64,
}
#[derive(Clone, Debug, Deserialize)]
pub struct ActivitySummary {
    pub since: String,
    pub until: String,
    pub total: u64,
    pub counts: Vec<ActivityCount>,
}
#[derive(Clone, Debug, Deserialize)]
pub struct ActivityResponse {
    pub activity: ActivitySummary,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub launch_generation: u64,
    #[serde(flatten)]
    pub diagnostics: Fields,
}

// Calendar validation without timezone conversion or duration-based day arithmetic.
fn calendar_date_valid(date: &str) -> bool {
    let b = date.as_bytes();
    if b.len() != 10
        || b[4] != b'-'
        || b[7] != b'-'
        || !b
            .iter()
            .enumerate()
            .all(|(i, c)| i == 4 || i == 7 || c.is_ascii_digit())
    {
        return false;
    }
    let year: u32 = date[..4].parse().unwrap();
    let month: u32 = date[5..7].parse().unwrap();
    let day: u32 = date[8..].parse().unwrap();
    let days = match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) => 29,
        2 => 28,
        _ => return false,
    };
    day >= 1 && day <= days
}

/// Resolve accepts a source scope but returns an independent target identity.
/// In particular an omitted target vault means launch, never the source vault.
#[derive(Clone, Debug, Deserialize)]
pub struct ResolvedNote {
    pub note_id: i64,
    pub title: String,
    pub file_kind: String,
    #[serde(default)]
    pub vault: String,
    #[serde(default)]
    pub path: Option<String>,
    #[serde(default, deserialize_with = "nullable_vec")]
    pub flags: Vec<String>,
    #[serde(flatten)]
    pub additional: Fields,
}
#[derive(Clone, Debug, Deserialize)]
pub struct Resolution {
    pub found: bool,
    pub note: ResolvedNote,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub term: String,
    #[serde(skip)]
    pub launch_generation: u64,
    #[serde(flatten)]
    pub diagnostics: Fields,
}
impl Resolution {
    pub fn key(&self) -> Option<NoteKey> {
        (self.found && self.note.note_id != 0).then(|| NoteKey {
            vault: self.note.vault.clone(),
            note_id: self.note.note_id,
        })
    }
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
    /// Supply an explicit bounded calendar window. No server-default dates, pagination,
    /// journal creation, or fallback to a different vault is performed here.
    pub fn activity(
        &self,
        since: &str,
        until: &str,
        vault: &str,
    ) -> Result<ActivityResponse, Error> {
        if !calendar_date_valid(since) || !calendar_date_valid(until) || since > until {
            return Err(Error::Config(
                "activity requires an ordered YYYY-MM-DD range".into(),
            ));
        }
        let mut args = vault_query(vault);
        args.extend([("since", since.into()), ("until", until.into())]);
        let mut result: ActivityResponse = self.get("/api/activity", &args)?;
        let summary = &result.activity;
        if summary.since != since || summary.until != until {
            return Err(Error::Shape("activity date range mismatch".into()));
        }
        let mut previous: Option<&str> = None;
        let mut total = 0u64;
        for bucket in &summary.counts {
            let date = bucket.date.as_str();
            if !calendar_date_valid(date)
                || date < since
                || date > until
                || previous.is_some_and(|prior| prior >= date)
            {
                return Err(Error::Shape("invalid activity bucket date or order".into()));
            }
            total = total
                .checked_add(bucket.count)
                .ok_or_else(|| Error::Shape("activity count overflow".into()))?;
            previous = Some(date);
        }
        if total != summary.total {
            return Err(Error::Shape("activity total mismatch".into()));
        }
        result.source_vault = vault.to_owned();
        result.launch_generation = self.launch_generation;
        Ok(result)
    }

    /// The caller supplies the parsed target without label or anchor suffix.
    /// Do not interpret ID-shaped/numeric terms or vault prefixes in this client.
    pub fn resolve(&self, term: &str, source_vault: &str) -> Result<Resolution, Error> {
        let mut args = vec![("term", term.to_owned())];
        args.extend(vault_query(source_vault));
        let mut result: Resolution = self.get("/api/resolve", &args)?;
        if result.found && result.note.note_id == 0 {
            return Err(Error::Shape(
                "resolver reported found with the missing-note sentinel".into(),
            ));
        }
        result.source_vault = source_vault.to_owned();
        result.term = term.to_owned();
        result.launch_generation = self.launch_generation;
        Ok(result)
    }

    pub fn full_graph(&self, vault: &str) -> Result<(crate::graph::Graph, Fields), Error> {
        let value: Value = self.get("/api/graph", &vault_query(vault))?;
        if value
            .get("vault")
            .and_then(Value::as_str)
            .is_some_and(|v| v != vault)
        {
            return Err(Error::Shape("graph vault identity mismatch".into()));
        }
        let response: crate::graph::GraphResponse =
            serde_json::from_value(value.clone()).map_err(|e| Error::Shape(e.to_string()))?;
        if response.graph.center_id != 0 {
            return Err(Error::Shape("full graph center identity mismatch".into()));
        }
        let mut fields: Fields =
            serde_json::from_value(value).map_err(|e| Error::Shape(e.to_string()))?;
        fields.remove("graph");
        Ok((response.graph, fields))
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
/// A complete raw-source replacement bound to its original server, vault and revision.
/// Prefer from_source; the transport revalidates safety even for literal construction.
#[derive(Clone, Debug)]
pub struct SaveRequest {
    pub key: NoteKey,
    pub body: String,
    pub etag: String,
    pub launch_generation: u64,
    pub storage_generation: u64,
}
impl SaveRequest {
    pub fn from_source(
        launch_generation: u64,
        storage: &Storage,
        source: &NoteResponse,
        body: String,
    ) -> Result<Self, Error> {
        writable_source(storage, source).map_err(|e| Error::SaveNotSent(Box::new(e)))?;
        Ok(Self {
            key: source.note.note.key(),
            body,
            etag: source.note.etag.clone(),
            launch_generation,
            storage_generation: storage.generation,
        })
    }
}
#[derive(Clone, Debug, Deserialize)]
pub struct SaveAck {
    pub vault: String,
    pub note_id: i64,
    pub etag: String,
    pub saved: bool,
}
fn degraded_save_data(value: &Value) -> bool {
    match value {
        Value::Object(fields) => fields.iter().any(|(name, value)| match name.as_str() {
            "partial" | "cache_used" | "read_only" => {
                value.as_bool() == Some(true)
                    || value.as_array().is_some_and(|items| !items.is_empty())
            }
            "issues" | "unavailable" => value.as_array().is_some_and(|items| !items.is_empty()),
            "omitted" => value.as_u64().is_some_and(|count| count > 0),
            "storage_state" => value.as_str().is_some_and(|phase| phase != "ready"),
            _ => degraded_save_data(value),
        }),
        Value::Array(items) => items.iter().any(degraded_save_data),
        _ => false,
    }
}
fn writable_source(storage: &Storage, source: &NoteResponse) -> Result<(), Error> {
    if storage.phase != "ready"
        || storage.read_only
        || source.note.read_only
        || source.note.etag.is_empty()
        || source.note.note.note_id == 0
        || degraded_save_data(
            &serde_json::to_value(&storage.additional).expect("JSON fields serialize"),
        )
        || degraded_save_data(
            &serde_json::to_value(&source.related_and_diagnostics).expect("JSON fields serialize"),
        )
        || degraded_save_data(
            &serde_json::to_value(&source.note.note.additional).expect("JSON fields serialize"),
        )
    {
        return Err(Error::Config(
            "raw source and verified writable storage with a nonempty server revision are required"
                .into(),
        ));
    }
    Ok(())
}
impl ReadClient {
    pub fn save(&self, save: &SaveRequest) -> Result<SaveAck, Error> {
        // Every failure in this closure is known to precede the PUT. Never report
        // a failed preliminary GET as an ambiguous write, or silently substitute an etag.
        let preflight = (|| {
            if save.etag.is_empty() || save.key.note_id == 0 {
                return Err(Error::Config(
                    "note identity and server revision are required".into(),
                ));
            }
            if save.launch_generation != self.launch_generation
                || !self.alive.load(Ordering::Acquire)
            {
                return Err(Error::Stale);
            }
            let before = self.storage()?;
            if before.generation != save.storage_generation
                || before.phase != "ready"
                || before.read_only
            {
                return Err(Error::Config(
                    "storage generation is not verified writable".into(),
                ));
            }
            let mut source_query = vault_query(&save.key.vault);
            source_query.push(("id", save.key.note_id.to_string()));
            let raw: Value = self.get("/api/note", &source_query)?;
            if raw
                .get("note")
                .and_then(|note| note.get("read_only"))
                .and_then(Value::as_bool)
                != Some(false)
            {
                return Err(Error::Config(
                    "fresh source must explicitly confirm read_only=false".into(),
                ));
            }
            let source: NoteResponse =
                serde_json::from_value(raw).map_err(|error| Error::Shape(error.to_string()))?;
            let after = self.storage()?;
            if after.generation != save.storage_generation || source.note.note.key() != save.key {
                return Err(Error::Stale);
            }
            writable_source(&after, &source)?;
            if source.note.etag != save.etag {
                return Err(Error::Config("the server revision changed before saving; original draft and revision must be retained for comparison".into()));
            }
            if !self.alive.load(Ordering::Acquire) {
                return Err(Error::Stale);
            }
            Ok(())
        })();
        preflight.map_err(|error| Error::SaveNotSent(Box::new(error)))?;
        let mut args = vault_query(&save.key.vault);
        args.push(("id", save.key.note_id.to_string()));
        let mut response = self
            .http
            .put(format!("{}/api/note", self.origin))
            .query(&args)
            .json(&serde_json::json!({"body": save.body, "etag": save.etag}))
            .send()
            .map_err(|error| Error::Transport(error.to_string()))?;
        let status = response.status().as_u16();
        let mut bytes = Vec::new();
        (&mut response)
            .take(BODY_LIMIT + 1)
            .read_to_end(&mut bytes)
            .map_err(|error| Error::Transport(error.to_string()))?;
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
        let value: Value = serde_json::from_slice(&bytes).map_err(|error| {
            Error::Json(format!(
                "{error}; response: {}",
                String::from_utf8_lossy(&bytes)
            ))
        })?;
        let ack: SaveAck =
            serde_json::from_value(value).map_err(|error| Error::Shape(error.to_string()))?;
        if !ack.saved
            || ack.etag.is_empty()
            || ack.note_id != save.key.note_id
            || ack.vault != save.key.vault
        {
            return Err(Error::Shape(
                "save acknowledgement does not match the submitted identity and nonempty revision"
                    .into(),
            ));
        }
        Ok(ack)
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
            .retry(reqwest::retry::never())
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
    /// Opt-in only: supply an explicit Go binary and a disposable copy of public
    /// docs/help. The test copies that input again and mutates only its own copy.
    #[test]
    #[ignore = "requires TRACK_NATIVE_TEST_BINARY and TRACK_NATIVE_PUBLIC_FIXTURE_COPY; writes only a new temporary copy"]
    fn owned_public_fixture_save_read_conflict_and_reap() {
        fn copy_tree(
            source: &std::path::Path,
            destination: &std::path::Path,
        ) -> std::io::Result<()> {
            std::fs::create_dir(destination)?;
            for entry in std::fs::read_dir(source)? {
                let entry = entry?;
                let kind = entry.file_type()?;
                let target = destination.join(entry.file_name());
                if kind.is_dir() {
                    copy_tree(&entry.path(), &target)?;
                } else if kind.is_file() {
                    std::fs::copy(entry.path(), target)?;
                } else {
                    return Err(std::io::Error::other(
                        "fixture must not contain symlinks or special files",
                    ));
                }
            }
            Ok(())
        }
        struct TemporaryFixture(PathBuf);
        impl Drop for TemporaryFixture {
            fn drop(&mut self) {
                let _ = std::fs::remove_dir_all(&self.0);
            }
        }
        let executable = PathBuf::from(
            std::env::var_os("TRACK_NATIVE_TEST_BINARY").expect("set explicit test binary"),
        );
        let input = PathBuf::from(
            std::env::var_os("TRACK_NATIVE_PUBLIC_FIXTURE_COPY")
                .expect("set explicit public docs/help fixture copy"),
        );
        assert!(executable.is_absolute() && input.is_absolute());
        let unique = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let root =
            std::env::temp_dir().join(format!("track-native-save-{}-{unique}", std::process::id()));
        std::fs::create_dir(&root).unwrap();
        let fixture = TemporaryFixture(root);
        copy_tree(&input, &fixture.0.join("vault")).unwrap();
        std::fs::write(fixture.0.join("config.yml"), "{}\n").unwrap();
        let port = (24000..30000)
            .find(|port| {
                let Ok(first) = TcpListener::bind(("127.0.0.1", *port)) else {
                    return false;
                };
                let second = TcpListener::bind(("127.0.0.1", *port + 1));
                drop(first);
                second.is_ok()
            })
            .expect("free adjacent fixture ports");
        let mut config = LaunchConfig::new(executable, Vault::Path(fixture.0.join("vault")), port);
        config.config = Some(fixture.0.join("config.yml"));
        config.cache = Some(fixture.0.join("cache"));
        let mut server = OwnedServer::start(config).expect("owned fixture server readiness");
        let pid = server.child.as_ref().unwrap().id();
        let client = server.client();
        let deadline = Instant::now() + Duration::from_secs(30);
        let storage = loop {
            server.check_running().unwrap();
            let state = client.storage().unwrap();
            if state.phase == "ready" && !state.read_only {
                break state;
            }
            assert!(
                Instant::now() < deadline,
                "fixture did not become writable: {state:?}"
            );
            thread::sleep(Duration::from_millis(50));
        };
        let listing = client.notes("").unwrap();
        let key = listing
            .notes
            .iter()
            .find(|note| note.file_kind == "note")
            .expect("public fixture note")
            .key();
        let mut source = client.note(&key).unwrap();
        let original = source.clone();
        // PUT storage bytes and GET's legacy-footmatter-aware read model differ.
        // These expectations were independently verified against the public fixture.
        const UNICODE_SOURCE: &str =
            "# Transport fixture\n\n日本語 [[Link]]\n\n```rust\nlet value = 42;\n```";
        for (submitted, expected_read, expected_disk) in [
            (
                UNICODE_SOURCE,
                UNICODE_SOURCE,
                "# Transport fixture\n\n日本語 [[Link]]\n\n```rust\nlet value = 42;\n```\n",
            ),
            ("Final LF\n", "Final LF", "Final LF\n"),
            (
                "Multiple final LFs\n\n",
                "Multiple final LFs",
                "Multiple final LFs\n\n",
            ),
            ("", "", ""),
        ] {
            let previous_etag = source.note.etag.clone();
            let request = SaveRequest::from_source(
                client.launch_generation,
                &storage,
                &source,
                submitted.into(),
            )
            .unwrap();
            let ack = client.save(&request).expect("acknowledged fixture save");
            assert!(!ack.etag.is_empty());
            assert_ne!(ack.etag, previous_etag);
            source = client.note(&request.key).unwrap();
            let disk = std::fs::read_to_string(
                fixture
                    .0
                    .join("vault/note")
                    .join(format!("{}.md", request.key.note_id)),
            )
            .unwrap();
            assert_eq!(source.note.body, expected_read, "GET read-model body");
            assert_eq!(disk, expected_disk, "stored raw bytes");
            assert_eq!(source.note.etag, ack.etag);
            assert_eq!(source.note.note.key(), original.note.note.key());
            assert_eq!(source.note.note.title, original.note.note.title);
            assert_eq!(source.note.note.tags, original.note.note.tags);
        }
        let stale = SaveRequest::from_source(
            client.launch_generation,
            &storage,
            &original,
            "must not overwrite".into(),
        )
        .unwrap();
        let error = client.save(&stale).unwrap_err();
        assert_eq!(error.save_disposition(), SaveDisposition::NotSent);
        let unchanged = client.note(&stale.key).unwrap();
        assert_eq!(unchanged.note.body, source.note.body);
        assert_eq!(unchanged.note.etag, source.note.etag);
        server.shutdown().expect("fixture child reaped");
        let mut status = 0;
        assert_eq!(
            unsafe { libc::waitpid(pid as i32, &mut status, libc::WNOHANG) },
            -1
        );
        assert_eq!(
            std::io::Error::last_os_error().raw_os_error(),
            Some(libc::ECHILD)
        );
        assert!(matches!(client.notes(""), Err(Error::Stale)));
        assert!(TcpListener::bind(("127.0.0.1", port)).is_ok());
        assert!(TcpListener::bind(("127.0.0.1", port + 1)).is_ok());
        eprintln!(
            "fixture saves, etags, title/tags preservation, stale-revision no-overwrite, child reaping and both-port release verified"
        );
    }

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

    const SAVE_STORAGE: &str = r#"{"phase":"ready","read_only":false,"verified":1,"generation":2}"#;
    const SAVE_SOURCE: &str = r#"{"note":{"note_id":42,"vault":"work & 日本語","file_kind":"note","path":"display only","title":"Title","body":"original\n","etag":"opaque-old","read_only":false}}"#;
    const SAVE_OK: &str =
        r#"{"vault":"work & 日本語","note_id":42,"etag":"opaque-new","saved":true}"#;
    fn save_request() -> SaveRequest {
        SaveRequest::from_source(
            1,
            &serde_json::from_str(SAVE_STORAGE).unwrap(),
            &serde_json::from_str(SAVE_SOURCE).unwrap(),
            "日本語 [[Link]]\n```\nsource\n```".into(),
        )
        .unwrap()
    }
    fn save_mock(
        replies: Vec<Option<(u16, String)>>,
    ) -> (ReadClient, thread::JoinHandle<Vec<String>>) {
        gated_save_mock(replies, None)
    }
    /// Signal after receiving a complete PUT and hold its response until released.
    /// This is a controlled local HTTP fixture, not a filesystem/vault mutation.
    fn gated_save_mock(
        replies: Vec<Option<(u16, String)>>,
        mut put_gate: Option<(std::sync::mpsc::Sender<()>, std::sync::mpsc::Receiver<()>)>,
    ) -> (ReadClient, thread::JoinHandle<Vec<String>>) {
        use std::io::Write;
        let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
        listener.set_nonblocking(true).unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let task = thread::spawn(move || {
            let mut requests = Vec::new();
            for reply in replies {
                let deadline = Instant::now() + Duration::from_secs(2);
                let mut stream = loop {
                    match listener.accept() {
                        Ok((stream, _)) => break stream,
                        Err(error)
                            if error.kind() == std::io::ErrorKind::WouldBlock
                                && Instant::now() < deadline =>
                        {
                            thread::sleep(Duration::from_millis(2))
                        }
                        _ => return requests,
                    }
                };
                // Darwin can inherit O_NONBLOCK from the listening socket.
                // Request parsing below is bounded blocking IO, not a readiness poll.
                stream.set_nonblocking(false).unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(2)))
                    .unwrap();
                let mut header = Vec::new();
                let mut byte = [0u8; 1];
                while !header.ends_with(b"\r\n\r\n") {
                    stream.read_exact(&mut byte).unwrap();
                    header.push(byte[0]);
                }
                let header_text = String::from_utf8(header.clone()).unwrap();
                let length = header_text
                    .lines()
                    .find_map(|line| {
                        line.to_ascii_lowercase()
                            .strip_prefix("content-length:")
                            .and_then(|value| value.trim().parse::<usize>().ok())
                    })
                    .unwrap_or(0);
                let mut body = vec![0; length];
                stream.read_exact(&mut body).unwrap();
                header.extend(body);
                requests.push(String::from_utf8(header).unwrap());
                if requests.last().unwrap().starts_with("PUT ") {
                    if let Some((received, release)) = put_gate.take() {
                        received.send(()).unwrap();
                        release
                            .recv_timeout(Duration::from_secs(3))
                            .expect("release controlled PUT response");
                    }
                }
                if let Some((status, body)) = reply {
                    write!(stream, "HTTP/1.1 {status} Test\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len()).unwrap();
                }
            }
            // Leave the listener available briefly to detect any unexpected retry.
            let deadline = Instant::now() + Duration::from_millis(30);
            while Instant::now() < deadline {
                if let Ok((mut stream, _)) = listener.accept() {
                    stream.set_nonblocking(false).unwrap();
                    stream
                        .set_read_timeout(Some(Duration::from_millis(30)))
                        .unwrap();
                    let mut bytes = [0; 4096];
                    if let Ok(n) = stream.read(&mut bytes) {
                        requests.push(String::from_utf8_lossy(&bytes[..n]).into_owned());
                    }
                }
                thread::sleep(Duration::from_millis(2));
            }
            requests
        });
        (
            ReadClient {
                http: reqwest::blocking::Client::builder()
                    .no_proxy()
                    .retry(reqwest::retry::never())
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
    fn save_replies(last: Option<(u16, &str)>) -> Vec<Option<(u16, String)>> {
        vec![
            Some((200, SAVE_STORAGE.into())),
            Some((200, SAVE_SOURCE.into())),
            Some((200, SAVE_STORAGE.into())),
            last.map(|(status, body)| (status, body.into())),
        ]
    }
    #[test]
    fn pending_quit_waits_for_real_save_response_and_cancel_does_not_cancel_put() {
        use crate::editing::{EditSession, LeaveAction, LeaveChoice, LeavePrompt, SaveOutcome};
        for cancel_quit in [false, true] {
            let storage: Storage = serde_json::from_str(SAVE_STORAGE).unwrap();
            let source: NoteResponse = serde_json::from_str(SAVE_SOURCE).unwrap();
            let mut edit = EditSession::from_note(1, &storage, &source).unwrap();
            edit.replace_text("pending fixture draft".into()).unwrap();
            // The UI routes Quit through this same Save/Discard/Cancel leave gate.
            let LeaveAction::Save(prepared) =
                edit.choose_leave(LeaveChoice::Save, 1, &storage).unwrap()
            else {
                panic!("expected explicit save before quit")
            };
            let attempt = prepared.attempt;
            let (received_tx, received_rx) = std::sync::mpsc::channel();
            let (release_tx, release_rx) = std::sync::mpsc::channel();
            let (client, requests) = gated_save_mock(
                save_replies(Some((200, SAVE_OK))),
                Some((received_tx, release_rx)),
            );
            let worker = thread::spawn(move || client.save(&prepared.request));
            received_rx.recv_timeout(Duration::from_secs(2)).unwrap();
            assert_eq!(edit.leave_prompt(), LeavePrompt::WaitForSave);
            assert!(edit.choose_leave(LeaveChoice::Save, 1, &storage).is_err());
            assert!(
                edit.choose_leave(LeaveChoice::Discard, 1, &storage)
                    .is_err()
            );
            assert!(
                edit.replace_text("must not replace in-flight buffer".into())
                    .is_err()
            );
            assert!(edit.dirty());
            assert_eq!(edit.etag(), "opaque-old");
            if cancel_quit {
                assert!(matches!(
                    edit.choose_leave(LeaveChoice::Cancel, 1, &storage).unwrap(),
                    LeaveAction::Stay
                ));
                assert!(edit.is_saving());
            }
            release_tx.send(()).unwrap();
            assert_eq!(
                edit.finish_save(attempt, 1, 2, worker.join().unwrap()),
                SaveOutcome::Saved {
                    leave: !cancel_quit
                }
            );
            assert_eq!(edit.draft(), "pending fixture draft");
            assert!(!edit.dirty());
            let requests = requests.join().unwrap();
            assert_eq!(requests.len(), 4);
            assert_eq!(
                requests
                    .iter()
                    .filter(|request| request.starts_with("PUT "))
                    .count(),
                1
            );
        }
    }
    #[test]
    fn uncertain_recorded_write_cancels_pending_quit_and_never_retries_or_overwrites() {
        use crate::editing::{
            DraftState, EditSession, LeaveAction, LeaveChoice, LeavePrompt, SaveOutcome,
        };
        for final_reply in [
            None,
            Some((500, r#"{"error":"fixture wrote body but indexing failed"}"#)),
            Some((200, "malformed acknowledgment")),
        ] {
            let storage: Storage = serde_json::from_str(SAVE_STORAGE).unwrap();
            let source: NoteResponse = serde_json::from_str(SAVE_SOURCE).unwrap();
            let authored = "日本語 [[Link]]\n\nkeep authored trailing LFs\n\n";
            let mut edit = EditSession::from_note(1, &storage, &source).unwrap();
            edit.replace_text(authored.into()).unwrap();
            let LeaveAction::Save(prepared) =
                edit.choose_leave(LeaveChoice::Save, 1, &storage).unwrap()
            else {
                panic!()
            };
            let attempt = prepared.attempt;
            // Subsequent GET reports the committed fixture representation even though
            // the PUT response was lost/500/malformed. Never infer rollback.
            let mut committed: Value = serde_json::from_str(SAVE_SOURCE).unwrap();
            committed["note"]["body"] = authored.trim_end_matches('\n').into();
            committed["note"]["etag"] = "fixture-committed-revision".into();
            let mut replies = save_replies(final_reply);
            replies.push(Some((200, committed.to_string())));
            let (received_tx, received_rx) = std::sync::mpsc::channel();
            let (release_tx, release_rx) = std::sync::mpsc::channel();
            let (client, requests) = gated_save_mock(replies, Some((received_tx, release_rx)));
            let saver = client.clone();
            let key = prepared.request.key.clone();
            let worker = thread::spawn(move || saver.save(&prepared.request));
            received_rx.recv_timeout(Duration::from_secs(2)).unwrap();
            assert_eq!(edit.leave_prompt(), LeavePrompt::WaitForSave);
            release_tx.send(()).unwrap();
            let error = worker.join().unwrap().unwrap_err();
            assert_eq!(error.save_disposition(), SaveDisposition::Uncertain);
            assert_eq!(
                edit.finish_save(attempt, 1, 2, Err(error)),
                SaveOutcome::Retained
            );
            assert!(matches!(edit.state(), DraftState::Uncertain(_)));
            assert_eq!(edit.draft(), authored);
            assert_eq!(edit.etag(), "opaque-old");
            assert_eq!(edit.baseline(), source.note.body);
            assert!(edit.dirty());
            assert_eq!(edit.leave_prompt(), LeavePrompt::AskSaveDiscardCancel);
            assert!(edit.prepare_save(1, &storage).is_err());
            assert!(edit.choose_leave(LeaveChoice::Save, 1, &storage).is_err());
            let current = client.note(&key).unwrap();
            assert_eq!(current.note.body, authored.trim_end_matches('\n'));
            edit.inspect_latest(1, &storage, &current).unwrap();
            assert_eq!(edit.draft(), authored);
            assert_eq!(edit.etag(), "opaque-old");
            assert!(edit.dirty()); // Comparison alone neither saves nor discards.
            let requests = requests.join().unwrap();
            assert_eq!(requests.len(), 5);
            assert_eq!(
                requests
                    .iter()
                    .filter(|request| request.starts_with("PUT "))
                    .count(),
                1
            );
            assert!(requests[4].starts_with("GET /api/note?"));
            let sent: Value =
                serde_json::from_str(requests[3].split("\r\n\r\n").nth(1).unwrap()).unwrap();
            assert_eq!(
                sent,
                serde_json::json!({"body":authored,"etag":"opaque-old"})
            );
        }
    }

    #[test]
    fn save_blocks_unverified_storage_old_session_and_unknown_source_permission() {
        for changes in [
            serde_json::json!({"phase":"partial"}),
            serde_json::json!({"read_only":true}),
            serde_json::json!({"generation":3}),
        ] {
            let mut state: Value = serde_json::from_str(SAVE_STORAGE).unwrap();
            for (name, value) in changes.as_object().unwrap() {
                state[name] = value.clone();
            }
            let (client, recorded) = save_mock(vec![Some((200, state.to_string()))]);
            assert_eq!(
                client.save(&save_request()).unwrap_err().save_disposition(),
                SaveDisposition::NotSent
            );
            assert_eq!(recorded.join().unwrap().len(), 1);
        }
        let (client, recorded) = save_mock(vec![]);
        let mut save = save_request();
        save.launch_generation += 1;
        assert_eq!(
            client.save(&save).unwrap_err().save_disposition(),
            SaveDisposition::NotSent
        );
        assert!(recorded.join().unwrap().is_empty());
        let mut source: Value = serde_json::from_str(SAVE_SOURCE).unwrap();
        source["note"].as_object_mut().unwrap().remove("read_only");
        let (client, recorded) = save_mock(vec![
            Some((200, SAVE_STORAGE.into())),
            Some((200, source.to_string())),
        ]);
        assert_eq!(
            client.save(&save_request()).unwrap_err().save_disposition(),
            SaveDisposition::NotSent
        );
        assert_eq!(recorded.join().unwrap().len(), 2);
    }
    #[test]
    fn save_sends_exactly_one_scoped_put_with_complete_original_revision_and_source() {
        for body in ["", "日本語 [[Link]]", "a\n", "a\n\n"] {
            let (client, recorded) = save_mock(save_replies(Some((200, SAVE_OK))));
            let mut save = save_request();
            save.body = body.into();
            let ack = client.save(&save).unwrap();
            assert_eq!(ack.etag, "opaque-new");
            let requests = recorded.join().unwrap();
            assert_eq!(requests.len(), 4);
            assert!(
                requests[..3]
                    .iter()
                    .all(|request| request.starts_with("GET "))
            );
            let put = &requests[3];
            assert!(
                put.starts_with("PUT /api/note?vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E&id=42 ")
            );
            let json: Value = serde_json::from_str(put.split("\r\n\r\n").nth(1).unwrap()).unwrap();
            assert_eq!(json, serde_json::json!({"body":body,"etag":"opaque-old"}));
            assert!(!put.to_ascii_lowercase().contains("if-match:"));
        }
    }
    #[test]
    fn save_distinguishes_rejection_conflict_and_uncertainty_without_retrying() {
        for (status, disposition) in [
            (400, SaveDisposition::Rejected),
            (403, SaveDisposition::Rejected),
            (404, SaveDisposition::Rejected),
            (405, SaveDisposition::Rejected),
            (409, SaveDisposition::Conflict),
            (500, SaveDisposition::Uncertain),
            (503, SaveDisposition::Rejected),
        ] {
            let (client, recorded) = save_mock(save_replies(Some((
                status,
                r#"{"error":"retained diagnostic"}"#,
            ))));
            let error = client.save(&save_request()).unwrap_err();
            assert_eq!(
                error.save_disposition(),
                disposition,
                "expected PUT HTTP {status}; actual error: {error:?}"
            );
            assert!(
                matches!(error, Error::Http { status: actual, body } if actual == status && body.contains("retained diagnostic"))
            );
            assert_eq!(recorded.join().unwrap().len(), 4);
        }
        let (client, recorded) = save_mock(save_replies(None));
        assert_eq!(
            client.save(&save_request()).unwrap_err().save_disposition(),
            SaveDisposition::Uncertain
        );
        assert_eq!(recorded.join().unwrap().len(), 4);
    }
    #[test]
    fn save_malformed_or_mismatched_success_is_uncertain_not_acknowledged() {
        for body in [
            "not json",
            r#"{"saved":true}"#,
            r#"{"vault":"other","note_id":42,"etag":"new","saved":true}"#,
            r#"{"vault":"work & 日本語","note_id":42,"etag":"","saved":true}"#,
            r#"{"vault":"work & 日本語","note_id":42,"etag":"new","saved":false}"#,
        ] {
            let (client, recorded) = save_mock(save_replies(Some((200, body))));
            assert_eq!(
                client.save(&save_request()).unwrap_err().save_disposition(),
                SaveDisposition::Uncertain
            );
            assert_eq!(recorded.join().unwrap().len(), 4);
        }
    }
    #[test]
    fn save_preflight_retains_draft_and_old_etag_without_put_when_unsafe_or_changed() {
        for field in ["revision", "readonly", "partial", "cached"] {
            let mut source: Value = serde_json::from_str(SAVE_SOURCE).unwrap();
            match field {
                "revision" => source["note"]["etag"] = "external-new".into(),
                "readonly" => source["note"]["read_only"] = true.into(),
                "partial" => source["read_state"] = serde_json::json!({"partial":true}),
                _ => source["read_state"] = serde_json::json!({"cache_used":true}),
            }
            let mut replies = vec![
                Some((200, SAVE_STORAGE.into())),
                Some((200, source.to_string())),
            ];
            if field != "readonly" {
                replies.push(Some((200, SAVE_STORAGE.into())));
            }
            let (client, recorded) = save_mock(replies);
            let save = save_request();
            assert_eq!(
                client.save(&save).unwrap_err().save_disposition(),
                SaveDisposition::NotSent
            );
            assert_eq!(save.etag, "opaque-old");
            assert!(save.body.contains("[[Link]]"));
            assert!(
                recorded
                    .join()
                    .unwrap()
                    .iter()
                    .all(|request| request.starts_with("GET "))
            );
        }
        let (client, recorded) = save_mock(vec![]);
        let mut save = save_request();
        save.etag.clear();
        assert_eq!(
            client.save(&save).unwrap_err().save_disposition(),
            SaveDisposition::NotSent
        );
        assert!(recorded.join().unwrap().is_empty());
    }
    #[test]
    fn resolver_encodes_source_and_preserves_independent_target_identity() {
        let (client, request) = mock(
            200,
            r#"{"found":true,"note":{"note_id":9007199254740993,"file_kind":"note","vault":"target","title":"日本語","flags":null},"read_state":{"partial":true,"cache_used":true},"future":"kept"}"#,
        );
        let result = client
            .resolve("work:id:9007199254740993 & 日本語", "source & personal")
            .unwrap();
        assert_eq!(result.source_vault, "source & personal");
        assert_eq!(result.launch_generation, client.launch_generation);
        assert_eq!(
            result.key().unwrap(),
            NoteKey {
                vault: "target".into(),
                note_id: 9007199254740993
            }
        );
        assert!(result.note.path.is_none());
        assert!(result.note.flags.is_empty());
        assert_eq!(result.diagnostics["read_state"]["partial"], true);
        assert_eq!(result.diagnostics["future"], "kept");
        let request = request.join().unwrap();
        assert!(request.starts_with("GET /api/resolve?term=work%3Aid%3A9007199254740993+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E&vault=source+%26+personal "));
    }
    #[test]
    fn resolver_missing_and_unnamed_target_do_not_inherit_source_scope() {
        let (client, request) = mock(
            200,
            r#"{"found":false,"note":{"note_id":0,"file_kind":"","title":""}}"#,
        );
        let result = client.resolve("123", "work").unwrap();
        assert!(result.key().is_none());
        assert_eq!(result.term, "123");
        assert!(request.join().unwrap().contains("term=123&vault=work"));
        let (client, request) = mock(
            200,
            r#"{"found":true,"note":{"note_id":123,"file_kind":"note","title":"Resolved title"}}"#,
        );
        let result = client.resolve("id:123", "work").unwrap();
        assert_eq!(result.key().unwrap().vault, "");
        assert!(request.join().unwrap().contains("term=id%3A123&vault=work"));
    }
    #[test]
    fn resolver_errors_are_not_missing_results_and_never_fallback() {
        for status in [400, 403, 500, 503] {
            let (client, request) = mock(status, r#"{"error":"ambiguous or unavailable"}"#);
            assert!(
                matches!(client.resolve("work:Title", ""), Err(Error::Http { status: actual, body }) if actual == status && body.contains("unavailable"))
            );
            let request = request.join().unwrap();
            assert!(request.starts_with("GET /api/resolve?term=work%3ATitle "));
        }
        for body in [
            r#"{"found":"true","note":{}}"#,
            r#"{"found":true,"note":{"note_id":0,"file_kind":"","title":""}}"#,
            r#"{"found":true,"note":{"note_id":"123","file_kind":"note","title":"T"}}"#,
        ] {
            let (client, request) = mock(200, body);
            assert!(matches!(client.resolve("123", ""), Err(Error::Shape(_))));
            request.join().unwrap();
        }
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
    fn full_graph_scope_optional_metadata_partial_and_errors() {
        for scope in ["", "work space"] {
            let body = serde_json::json!({"vault":scope,"graph":{"center_id":0,"nodes":[{"note_id":-42,"title":"A"}],"edges":[]},"read_state":{"partial":true}}).to_string();
            let (client, request) = mock(200, Box::leak(body.into_boxed_str()));
            let (graph, fields) = client.full_graph(scope).unwrap();
            assert_eq!(graph.nodes[0].note_id, -42);
            assert_eq!(graph.nodes[0].area_score, None);
            assert_eq!(fields["read_state"]["partial"], true);
            let wire = request.join().unwrap();
            if scope.is_empty() {
                assert!(wire.contains("/api/graph HTTP"));
            } else {
                assert!(wire.contains("/api/graph?vault=work+space"));
            }
            assert!(!wire.contains("id="));
        }
        let (client, request) = mock(200, r#"{"graph":{"nodes":[],"edges":[]}}"#);
        assert!(client.full_graph("").unwrap().0.nodes.is_empty());
        request.join().unwrap();
        let (client, request) = mock(409, r#"{"error":"invalid link format"}"#);
        let error = client.full_graph("").unwrap_err().to_string();
        assert!(error.contains("409") && error.contains("invalid link format"));
        request.join().unwrap();
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
                    .retry(reqwest::retry::never())
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
    fn activity_preserves_sparse_counts_scope_and_partial_diagnostics() {
        let (client, task) = mock(
            200,
            r#"{"activity":{"since":"2026-11-01","until":"2026-11-13","total":4,"counts":[{"date":"2026-11-02","count":1},{"date":"2026-11-10","count":3}]},"read_state":{"partial":true,"cache_used":true,"issues":["offline"]}}"#,
        );
        let result = client
            .activity("2026-11-01", "2026-11-13", "work & 日本語")
            .unwrap();
        assert_eq!(result.source_vault, "work & 日本語");
        assert_eq!(result.launch_generation, client.launch_generation);
        assert_eq!(result.activity.total, 4);
        assert_eq!(result.activity.counts.len(), 2);
        assert_eq!(result.activity.counts[1].count, 3);
        assert_eq!(result.diagnostics["read_state"]["partial"], true);
        assert_eq!(result.diagnostics["read_state"]["cache_used"], true);
        let request = task.join().unwrap();
        assert!(request.starts_with("GET /api/activity?"));
        assert!(request.contains("vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E"));
        assert!(request.contains("since=2026-11-01&until=2026-11-13"));
    }

    #[test]
    fn activity_valid_empty_launch_scope_is_not_an_error() {
        let (client, task) = mock(
            200,
            r#"{"activity":{"since":"2024-02-29","until":"2024-03-01","total":0,"counts":[]}}"#,
        );
        let result = client.activity("2024-02-29", "2024-03-01", "").unwrap();
        assert!(result.source_vault.is_empty());
        assert!(result.activity.counts.is_empty());
        assert_eq!(result.activity.total, 0);
        let request = task.join().unwrap();
        assert!(!request.contains("vault="));
        assert!(request.starts_with("GET /api/activity?"));
    }

    #[test]
    fn activity_rejects_malformed_or_inconsistent_success() {
        for body in [
            "{}",
            r#"{"activity":{"since":"2026-11-01","until":"2026-11-13","total":0,"counts":null}}"#,
            r#"{"activity":{"since":"2026-11-02","until":"2026-11-13","total":0,"counts":[]}}"#,
            r#"{"activity":{"since":"2026-11-01","until":"2026-11-13","total":1,"counts":[]}}"#,
            r#"{"activity":{"since":"2026-11-01","until":"2026-11-13","total":-1,"counts":[]}}"#,
            r#"{"activity":{"since":"2026-11-01","until":"2026-11-13","total":1,"counts":[{"date":"2026-11-14","count":1}]}}"#,
            r#"{"activity":{"since":"2026-11-01","until":"2026-11-13","total":1,"counts":[{"date":"2026-11-02","count":"1"}]}}"#,
            r#"{"activity":{"since":"2026-11-01","until":"2026-11-13","total":2,"counts":[{"date":"2026-11-02","count":1},{"date":"2026-11-02","count":1}]}}"#,
            r#"{"activity":{"since":"2026-11-01","until":"2026-11-13","total":2,"counts":[{"date":"2026-11-03","count":1},{"date":"2026-11-02","count":1}]}}"#,
        ] {
            let (client, task) = mock(200, body);
            assert!(
                matches!(
                    client.activity("2026-11-01", "2026-11-13", "work"),
                    Err(Error::Shape(_))
                ),
                "{body}"
            );
            task.join().unwrap();
        }
    }

    #[test]
    fn activity_errors_do_not_fallback_and_invalid_ranges_are_not_sent() {
        for status in [400, 403, 500, 503] {
            let (client, task) = mock(status, r#"{"error":"unavailable selected vault"}"#);
            assert!(
                matches!(client.activity("2026-11-01", "2026-11-13", "missing"), Err(Error::Http { status: actual, body }) if actual == status && body.contains("selected vault"))
            );
            let request = task.join().unwrap();
            assert!(request.contains("vault=missing"));
        }
        let (client, task) = mock(200, "<html>foreign server</html>");
        // Invalid arguments fail locally, without consuming the single mock request.
        for (since, until) in [
            ("", "2026-11-13"),
            ("2026-02-29", "2026-11-13"),
            ("2026-11-14", "2026-11-13"),
            ("日本語", "2026-11-13"),
        ] {
            assert!(matches!(
                client.activity(since, until, ""),
                Err(Error::Config(_))
            ));
        }
        assert!(matches!(
            client.activity("2026-11-01", "2026-11-13", ""),
            Err(Error::Json(_))
        ));
        task.join().unwrap();
        client.alive.store(false, Ordering::Release);
        assert!(matches!(
            client.activity("2026-11-01", "2026-11-13", ""),
            Err(Error::Stale)
        ));
        assert!(!calendar_date_valid("1900-02-29"));
        assert!(calendar_date_valid("2000-02-29"));
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
