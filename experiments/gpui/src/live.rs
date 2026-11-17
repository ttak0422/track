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
    pub creation_fixture: Option<CreateFixture>,
    pub read_fixture: Option<ReadFixture>,
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
            creation_fixture: None,
            read_fixture: None,
        }
    }
    fn canonical_read_only(&self) -> bool {
        self.creation_fixture.is_none()
    }
    fn configure_read_mode(&self, command: &mut Command) -> Result<(), Error> {
        if self.canonical_read_only() {
            let cache = self.cache.as_ref().ok_or_else(|| {
                Error::Config(
                    "Canonical read fixtures require an explicit private cache root".into(),
                )
            })?;
            use std::os::unix::fs::PermissionsExt;
            if std::fs::metadata(cache)?.permissions().mode() & 0o077 != 0 {
                return Err(Error::Config(
                    "Canonical read fixture cache must have private permissions (0700)".into(),
                ));
            }
            command
                .arg("--canonical-read-only")
                .arg("--read-only-cache-root")
                .arg(cache);
        }
        Ok(())
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

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct CanonicalCapability {
    protocol: String,
    version: u64,
    canonical_read_only: bool,
}
impl CanonicalCapability {
    fn validate(&self) -> Result<(), Error> {
        if self.protocol != "track-canonical-read-only"
            || self.version != 1
            || !self.canonical_read_only
        {
            return Err(Error::Startup("Owned engine does not advertise the required canonical-read-only protocol version 1".into()));
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
#[derive(Clone, Debug, Deserialize, PartialEq)]
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
    #[serde(deserialize_with = "nullable_vec")]
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
/// Validate before namespace normalization. No sorting, deduplication, or title inference.
fn validate_recently_created(data: &mut Notes, vault: &str, launch: &str) -> Result<(), Error> {
    if data.notes.len() > 100 {
        return Err(Error::Shape("recent notes exceed 100 rows".into()));
    }
    let mut seen = std::collections::HashSet::new();
    for note in &data.notes {
        if note.note_id <= 0
            || note.file_kind != "note"
            || !seen.insert(note.note_id)
            || !calendar_range_vault_matches(vault, &note.vault, launch)
            || note.title.len() > 64 * 1024
            || note.path.len() > 16 * 1024
        {
            return Err(Error::Shape(
                "invalid recent-note identity or bounds".into(),
            ));
        }
    }
    let canonical = if vault.is_empty() { launch } else { vault };
    for note in &mut data.notes {
        note.vault = canonical.into();
    }
    Ok(())
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

/// Read-only notes active on one explicit calendar day, never journal creation.
#[derive(Clone, Debug, Deserialize)]
pub struct AgendaResponse {
    pub date: String,
    pub notes: Vec<Note>,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub launch_generation: u64,
    #[serde(flatten)]
    pub diagnostics: Fields,
}

/// One indexed dated task. Display-only: line numbers are not write authority.
#[derive(Clone, Debug, Deserialize)]
pub struct DatedTask {
    pub note_id: i64,
    pub file_kind: String,
    pub title: String,
    #[serde(default)]
    pub path: String,
    pub line: u64,
    pub state: String,
    pub done: bool,
    #[serde(default)]
    pub priority: String,
    #[serde(default)]
    pub scheduled: String,
    #[serde(default)]
    pub due: String,
    #[serde(default)]
    pub completed: String,
    pub text: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct DatedTasksResponse {
    pub vault: String,
    pub tasks: Vec<DatedTask>,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub launch_generation: u64,
    #[serde(flatten)]
    pub diagnostics: Fields,
}

/// Per-note source task lines, including undated items. No mutable task API is exposed.
#[derive(Clone, Debug, Deserialize)]
pub struct NoteTask {
    pub line: u64,
    pub state: String,
    pub done: bool,
    #[serde(default)]
    pub priority: String,
    #[serde(default)]
    pub scheduled: String,
    #[serde(default)]
    pub due: String,
    #[serde(default)]
    pub completed: String,
    pub text: String,
}
#[derive(Deserialize)]
struct NoteTaskSet {
    items: Vec<NoteTask>,
}
/// Local source replacement has no refreshed engine task projection. Keep an explicit
/// unavailable marker: removing the field would incorrectly mean zero tasks.
pub fn invalidate_note_tasks(fields: &mut Fields) {
    fields.insert("tasks".into(), Value::Null);
}

/// Validate task metadata attached to the accepted note body; absent means no tasks.
pub fn note_tasks(value: Option<&Value>) -> Result<Vec<NoteTask>, Error> {
    let Some(value) = value else {
        return Ok(Vec::new());
    };
    if value.is_null() {
        return Err(Error::Shape(
            "Task metadata unavailable; reload the note.".into(),
        ));
    }
    if value
        .get("items")
        .and_then(Value::as_array)
        .is_none_or(|items| items.len() > 4096)
    {
        return Err(Error::Shape("invalid or excessive note tasks".into()));
    }
    let set: NoteTaskSet =
        serde_json::from_value(value.clone()).map_err(|e| Error::Shape(e.to_string()))?;
    let items = set.items;
    if items.len() > 4096 {
        return Err(Error::Shape("too many note tasks".into()));
    }
    let mut previous = 0;
    for item in &items {
        let done = match item.state.as_str() {
            "TODO" | "DOING" | "WAITING" => false,
            "DONE" | "CANCELLED" => true,
            _ => return Err(Error::Shape("unknown task state".into())),
        };
        if item.line <= previous
            || item.line > i64::MAX as u64
            || item.done != done
            || item.text.len() > 64 * 1024
            || (!item.priority.is_empty()
                && !(item.priority.len() == 1 && item.priority.as_bytes()[0].is_ascii_uppercase()))
            || [&item.scheduled, &item.due, &item.completed]
                .iter()
                .any(|d| !d.is_empty() && !date_wire_shape(d))
        {
            return Err(Error::Shape("invalid note task line or metadata".into()));
        }
        previous = item.line;
    }
    Ok(items)
}

/// Validated preorder forest, preserving the engine's sibling order.
#[derive(Clone, Debug)]
pub struct HierarchyRow {
    pub note_id: i64,
    pub title: String,
    pub flags: Vec<String>,
    pub parent: Option<usize>,
    pub depth: usize,
    pub has_children: bool,
}
#[derive(Clone, Debug)]
pub struct HierarchyResponse {
    pub vault: String,
    pub rows: Vec<HierarchyRow>,
    pub source_vault: String,
    pub launch_generation: u64,
    pub diagnostics: Fields,
}
#[derive(Deserialize)]
struct HierarchyWire {
    vault: String,
    hierarchy: Vec<HierarchyNode>,
    #[serde(flatten)]
    diagnostics: Fields,
}
#[derive(Deserialize)]
struct HierarchyNode {
    note_id: i64,
    title: String,
    file_kind: String,
    #[serde(default)]
    vault: String,
    #[serde(default, deserialize_with = "nullable_vec")]
    flags: Vec<String>,
    #[serde(default, deserialize_with = "nullable_vec")]
    children: Vec<HierarchyNode>,
}
fn flatten_hierarchy(nodes: Vec<HierarchyNode>, vault: &str) -> Result<Vec<HierarchyRow>, Error> {
    let mut rows = Vec::new();
    let mut ids = std::collections::HashSet::new();
    let mut pending: Vec<_> = nodes.into_iter().rev().map(|n| (n, None, 0)).collect();
    while let Some((node, parent, depth)) = pending.pop() {
        if rows.len() >= 4096
            || depth >= 32
            || node.note_id <= 0
            || !ids.insert(node.note_id)
            || node.title.len() > 64 * 1024
            || !matches!(node.file_kind.as_str(), "note" | "journal")
            || (!node.vault.is_empty() && node.vault != vault)
            || node.flags.len() > 128
            || node.flags.iter().any(|f| f.len() > 1024)
        {
            return Err(Error::Shape("invalid hierarchy identity or metadata, duplicate node, or forest exceeds 4096 nodes / 32 levels".into()));
        }
        let index = rows.len();
        rows.push(HierarchyRow {
            note_id: node.note_id,
            title: node.title,
            flags: node.flags,
            parent,
            depth,
            has_children: !node.children.is_empty(),
        });
        pending.extend(
            node.children
                .into_iter()
                .rev()
                .map(|n| (n, Some(index), depth + 1)),
        );
    }
    Ok(rows)
}

/// Calendar records remain display-only. Timed timestamps are bounded opaque
/// engine values: this client does not reinterpret them in a viewer time zone.
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarEvent {
    pub version: u32,
    pub id: String,
    pub revision: u64,
    pub title: String,
    pub all_day: bool,
    pub start: String,
    pub end: String,
    pub time_zone: String,
    pub description: String,
    pub location: String,
    pub url: String,
    pub note_id: String,
    pub status: String,
    pub deleted: bool,
    pub updated_at: String,
}
impl CalendarEvent {
    pub fn all_day_on(&self, date: &str) -> bool {
        self.all_day
            && !self.deleted
            && event_date_valid(date)
            && self.start.as_str() <= date
            && date < self.end.as_str()
    }
}
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarEventsResponse {
    pub vault: String,
    pub events: Vec<CalendarEvent>,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub launch_generation: u64,
    #[serde(flatten)]
    pub diagnostics: Fields,
}
/// Day membership is authoritative engine output; stored event labels stay opaque.
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarDayEventsResponse {
    pub vault: String,
    pub date: String,
    pub time_zone: String,
    pub day_start: String,
    pub day_end: String,
    #[serde(default)]
    pub day_start_unix: Option<i64>,
    #[serde(default)]
    pub day_end_unix: Option<i64>,
    pub events: Vec<CalendarEvent>,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub launch_generation: u64,
    #[serde(flatten)]
    pub diagnostics: Fields,
}

/// Bounded engine-projected range preview; membership is never recomputed locally.
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarRangePreview {
    pub id: String,
    pub revision: u64,
    pub title: String,
    pub status: String,
}
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarRangeDay {
    pub date: String,
    pub count: u64,
    pub previews: Vec<CalendarRangePreview>,
}
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarRangeResponse {
    pub vault: String,
    pub since: String,
    pub until: String,
    pub time_zone: String,
    pub days: Vec<CalendarRangeDay>,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub launch_generation: u64,
    #[serde(flatten)]
    pub diagnostics: Fields,
}
/// Scoped, index-only civil month summary. No note body or mutation authority.
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarTaskPreview {
    pub note_id: i64,
    pub file_kind: String,
    pub line: u64,
    pub text: String,
    pub text_truncated: bool,
    pub state: String,
    pub done: bool,
    pub priority: String,
    pub scheduled: String,
    pub due: String,
}
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarNotePreview {
    pub note_id: i64,
    pub file_kind: String,
    pub title: String,
    pub title_truncated: bool,
}
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarContentDay {
    pub date: String,
    pub task_count: u64,
    pub tasks: Vec<CalendarTaskPreview>,
    pub note_count: u64,
    pub notes: Vec<CalendarNotePreview>,
}
#[derive(Clone, Debug, Deserialize)]
pub struct CalendarContentResponse {
    pub vault: String,
    pub since: String,
    pub until: String,
    pub time_zone: String,
    pub today: String,
    pub days: Vec<CalendarContentDay>,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub launch_generation: u64,
    #[serde(flatten)]
    pub diagnostics: Fields,
}
fn validate_calendar_content(
    value: &CalendarContentResponse,
    vault: &str,
    launch: &str,
    since: &str,
    until: &str,
    zone: &str,
) -> Result<(), Error> {
    let dates = range_dates(since, until)
        .ok_or_else(|| Error::Shape("invalid calendar content range".into()))?;
    let kind = |s: &str| !s.is_empty() && s.len() <= 128 && !s.chars().any(char::is_control);
    let label = |s: &str, truncated: bool| {
        s.len() <= 256 && !s.chars().any(char::is_control) && (!truncated || s.ends_with('…'))
    };
    if value.vault.len() > 1024
        || value.vault.contains('\0')
        || !calendar_range_vault_matches(vault, &value.vault, launch)
        || value.since != since
        || value.until != until
        || value.time_zone != zone
        || !calendar_date_valid(&value.today)
        || value.days.len() != dates.len()
    {
        return Err(Error::Shape(
            "calendar content identity or coverage mismatch".into(),
        ));
    }
    for (day, date) in value.days.iter().zip(dates) {
        let mut tasks = std::collections::HashSet::new();
        let mut notes = std::collections::HashSet::new();
        if day.date != date
            || day.task_count > 16384
            || day.note_count > 16384
            || day.tasks.len() != (day.task_count as usize).min(3)
            || day.notes.len() != (day.note_count as usize).min(3)
        {
            return Err(Error::Shape(
                "calendar content count or day mismatch".into(),
            ));
        }
        for task in &day.tasks {
            let terminal = match task.state.as_str() {
                "TODO" | "DOING" | "WAITING" => false,
                "DONE" | "CANCELLED" => true,
                _ => return Err(Error::Shape("unknown calendar task state".into())),
            };
            if task.note_id <= 0
                || task.line == 0
                || task.line > i64::MAX as u64
                || !kind(&task.file_kind)
                || !label(&task.text, task.text_truncated)
                || task.done != terminal
                || !tasks.insert((task.note_id, task.line))
                || (!task.priority.is_empty()
                    && !(task.priority.len() == 1
                        && task.priority.as_bytes()[0].is_ascii_uppercase()))
                || [&task.scheduled, &task.due]
                    .iter()
                    .any(|d| !d.is_empty() && !date_wire_shape(d))
                || (task.scheduled != date && task.due != date)
            {
                return Err(Error::Shape("invalid calendar task preview".into()));
            }
        }
        for note in &day.notes {
            if note.note_id <= 0
                || !kind(&note.file_kind)
                || !label(&note.title, note.title_truncated)
                || !notes.insert(note.note_id)
            {
                return Err(Error::Shape("invalid calendar note preview".into()));
            }
        }
    }
    Ok(())
}
fn range_dates(since: &str, until: &str) -> Option<Vec<String>> {
    // Match the engine's finite projection domain, including its end-date bound.
    if !event_date_valid(since)
        || !event_date_valid(until)
        || since.starts_with("9999")
        || until.starts_with("9999")
        || since > until
    {
        return None;
    }
    let mut days = vec![since.to_owned()];
    while days.last()?.as_str() != until {
        if days.len() >= 42 {
            return None;
        }
        days.push(crate::workspace_activity::adjacent_day(days.last()?, true)?);
    }
    Some(days)
}
pub(crate) fn calendar_range_vault_matches(requested: &str, wire: &str, launch: &str) -> bool {
    if requested.is_empty() || requested == launch {
        wire.is_empty() || wire == launch
    } else {
        wire == requested
    }
}
fn validate_calendar_range(
    value: &CalendarRangeResponse,
    vault: &str,
    launch: &str,
    since: &str,
    until: &str,
    zone: &str,
) -> Result<(), Error> {
    let days =
        range_dates(since, until).ok_or_else(|| Error::Shape("invalid calendar range".into()))?;
    if value.vault.len() > 1024
        || value.vault.contains('\0')
        || !calendar_range_vault_matches(vault, &value.vault, launch)
        || value.since != since
        || value.until != until
        || value.time_zone != zone
        || value.days.len() != days.len()
    {
        return Err(Error::Shape(
            "calendar range identity or date coverage mismatch".into(),
        ));
    }
    for (row, date) in value.days.iter().zip(days) {
        let mut ids = std::collections::HashSet::new();
        if row.date != date
            || row.count > 4096
            || row.previews.len() != (row.count as usize).min(3)
            || row.previews.iter().any(|p| {
                !event_id_valid(&p.id)
                    || !ids.insert(&p.id)
                    || p.revision == 0
                    || p.revision > i64::MAX as u64
                    || p.title.trim().is_empty()
                    || p.title.len() > 500
                    || p.title.chars().any(char::is_control)
                    || !matches!(p.status.as_str(), "confirmed" | "cancelled")
            })
        {
            return Err(Error::Shape(
                "invalid calendar range preview or count".into(),
            ));
        }
    }
    Ok(())
}

// Validate the explicit UTC boundary contract with civil arithmetic, not a local
// timezone conversion or interpretation of the stored event timestamps.
fn utc_day_bounds(date: &str) -> Option<(String, String)> {
    if !event_date_valid(date) || date.starts_with("9999") {
        return None;
    }
    let mut year: u32 = date[..4].parse().ok()?;
    let mut month: u32 = date[5..7].parse().ok()?;
    let mut day: u32 = date[8..].parse().ok()?;
    day += 1;
    if !calendar_date_valid(&format!("{year:04}-{month:02}-{day:02}")) {
        day = 1;
        month += 1;
        if month == 13 {
            month = 1;
            year += 1;
        }
    }
    Some((
        format!("{date}T00:00:00Z"),
        format!("{year:04}-{month:02}-{day:02}T00:00:00Z"),
    ))
}

// UTC formatting delegates calendar conversion to the platform, never its local
// timezone. The engine supplies integral-second boundaries. Require matching
// strings as well as bounded signed instants, so contradictory wire data fails.
fn calendar_boundary_matches(label: &str, unix: i64) -> bool {
    // RFC3339's supported positive four-digit years, inclusive through 9999.
    if !(-62_135_596_800..=253_402_300_799).contains(&unix) {
        return false;
    }
    let seconds = unix as libc::time_t;
    if seconds as i64 != unix {
        return false;
    }
    let mut calendar = std::mem::MaybeUninit::<libc::tm>::uninit();
    // SAFETY: both pointers are valid and gmtime_r initializes the output on
    // success. It has no mutable global timezone dependency.
    let calendar = unsafe {
        if libc::gmtime_r(&seconds, calendar.as_mut_ptr()).is_null() {
            return false;
        }
        calendar.assume_init()
    };
    label
        == format!(
            "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}Z",
            calendar.tm_year + 1900,
            calendar.tm_mon + 1,
            calendar.tm_mday,
            calendar.tm_hour,
            calendar.tm_min,
            calendar.tm_sec
        )
}

fn validate_calendar_records(
    returned_vault: &mut String,
    events: &[CalendarEvent],
    vault: &str,
) -> Result<(), Error> {
    if returned_vault.len() > 1024
        || returned_vault.contains('\0')
        || (!vault.is_empty() && !returned_vault.is_empty() && returned_vault.as_str() != vault)
        || events.len() > 4096
    {
        return Err(Error::Shape(
            "calendar vault mismatch or excessive rows".into(),
        ));
    }
    if returned_vault.is_empty() {
        *returned_vault = vault.into();
    }
    let mut ids = std::collections::HashSet::new();
    for e in events {
        let metadata = [
            (&e.title, 500, false),
            (&e.description, 32768, true),
            (&e.location, 2048, false),
            (&e.url, 4096, false),
            (&e.time_zone, 128, false),
            (&e.note_id, 20, false),
            (&e.start, 64, false),
            (&e.end, 64, false),
            (&e.updated_at, 64, false),
        ];
        if e.version != 1
            || e.revision == 0
            || e.revision > i64::MAX as u64
            || !event_id_valid(&e.id)
            || !ids.insert(&e.id)
            || e.deleted
            || e.title.trim().is_empty()
            || !matches!(e.status.as_str(), "confirmed" | "cancelled")
            || metadata.iter().any(|(s, max, multiline)| {
                s.len() > *max
                    || s.chars()
                        .any(|c| c.is_control() && !(*multiline && matches!(c, '\n' | '\r' | '\t')))
            })
            || (!e.note_id.is_empty()
                && !e
                    .note_id
                    .parse::<i64>()
                    .is_ok_and(|id| id > 0 && id.to_string() == e.note_id))
            || (e.all_day
                && (!event_date_valid(&e.start)
                    || !event_date_valid(&e.end)
                    || e.start >= e.end
                    || !e.time_zone.is_empty()))
            || (!e.all_day && (e.start.is_empty() || e.end.is_empty() || e.time_zone.is_empty()))
        {
            return Err(Error::Shape(
                "invalid calendar event identity, range or metadata".into(),
            ));
        }
    }
    Ok(())
}

fn event_date_valid(date: &str) -> bool {
    calendar_date_valid(date) && !date.starts_with("0000")
}
fn event_id_valid(id: &str) -> bool {
    id.len() == 36
        && id.bytes().enumerate().all(|(i, b)| {
            if [8, 13, 18, 23].contains(&i) {
                b == b'-'
            } else {
                b.is_ascii_digit() || (b'a'..=b'f').contains(&b)
            }
        })
}

// Task tokens preserve the engine's lexical date shape, even for impossible
// calendar days. Read-only listings must not discard other valid tasks for these.
fn date_wire_shape(date: &str) -> bool {
    let b = date.as_bytes();
    b.len() == 10
        && b[4] == b'-'
        && b[7] == b'-'
        && b.iter()
            .enumerate()
            .all(|(i, c)| i == 4 || i == 7 || c.is_ascii_digit())
}

// Calendar validation without timezone conversion or duration-based day arithmetic.
pub(crate) fn calendar_date_valid(date: &str) -> bool {
    if !date_wire_shape(date) {
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

#[derive(Clone, Debug, Deserialize)]
pub struct JournalLookupNote {
    pub note_id: i64,
    pub file_kind: String,
    pub title: String,
}
#[derive(Clone, Debug, Deserialize)]
pub struct JournalLookup {
    pub vault: String,
    pub title: String,
    pub found: bool,
    pub note: Option<JournalLookupNote>,
    #[serde(skip)]
    pub source_vault: String,
    #[serde(skip)]
    pub launch_generation: u64,
}
pub(crate) fn journal_title_valid(title: &str) -> bool {
    if !matches!(title.len(), 6 | 8) || !title.bytes().all(|b| b.is_ascii_digit()) {
        return false;
    }
    let date = format!(
        "{}-{}-{}",
        &title[..4],
        &title[4..6],
        if title.len() == 8 { &title[6..] } else { "01" }
    );
    !title.starts_with("0000") && !title.starts_with("9999") && calendar_date_valid(&date)
}
#[derive(Clone, Debug)]
pub(crate) struct JournalExpectation {
    pub key: NoteKey,
    pub title: String,
    pub launch_vault: String,
}
impl JournalExpectation {
    pub fn accepts(&self, note: &Note) -> bool {
        note.note_id == self.key.note_id
            && note.file_kind == "journal"
            && note.title == self.title
            && calendar_range_vault_matches(&self.key.vault, &note.vault, &self.launch_vault)
    }
    pub fn verify(&self, response: NoteResponse) -> Result<NoteResponse, Error> {
        if self.accepts(&response.note.note) {
            Ok(response)
        } else {
            Err(Error::Shape("Journal changed after lookup; no mismatched body was displayed. Return to Calendar and retry.".into()))
        }
    }
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
#[derive(Clone, Debug, Deserialize, PartialEq)]
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
#[derive(Clone, Debug, Deserialize, PartialEq)]
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
    canonical_read_only: bool,
    creation_store: Arc<Mutex<Option<Result<crate::session_store::CreationStore, String>>>>,
    creation_fixture: Option<CreateFixture>,
    http: reqwest::blocking::Client,
    origin: String,
    alive: Arc<AtomicBool>,
    pub launch_generation: u64,
}
/// Sanitized handoff errors never retain response bodies, URLs, or capabilities.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum HandoffError {
    NotSent,
    Expired,
    Http(u16),
    Uncertain,
    Invalid(&'static str),
    Stale,
}
impl fmt::Display for HandoffError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NotSent => f.write_str("Owned server changed; handoff was not sent"),
            Self::Expired => {
                f.write_str("Handoff expired or was already used; run track markdown again")
            }
            Self::Http(status) => write!(
                f,
                "Handoff server returned HTTP {status}; run track markdown again"
            ),
            Self::Uncertain => f.write_str(
                "Opening failed; the handoff may have been consumed. Run track markdown again",
            ),
            Self::Invalid(reason) => write!(
                f,
                "{reason}; the handoff may have been consumed. Run track markdown again"
            ),
            Self::Stale => f.write_str(
                "Owned server changed while opening; the handoff may have been consumed",
            ),
        }
    }
}
impl ReadClient {
    pub(crate) fn markdown_origin(&self) -> &str {
        &self.origin
    }
    pub(crate) fn is_alive(&self) -> bool {
        self.alive.load(Ordering::Acquire)
    }
    /// Exactly one destructive temporary-queue consume; never probe storage,
    /// follow the pasted URL, retry, or retain server-provided error content.
    pub(crate) fn consume_markdown(
        &self,
        capability: crate::markdown_handoff::Capability,
    ) -> Result<crate::markdown_handoff::Snapshot, HandoffError> {
        self.consume_markdown_with_timeout(capability, Duration::from_secs(10))
    }
    fn consume_markdown_with_timeout(
        &self,
        capability: crate::markdown_handoff::Capability,
        timeout: Duration,
    ) -> Result<crate::markdown_handoff::Snapshot, HandoffError> {
        if !self.is_alive()
            || capability.launch != self.launch_generation
            || capability.origin != self.origin
        {
            return Err(HandoffError::NotSent);
        }
        let request = self
            .http
            .post(format!("{}/api/markdown/consume", self.origin))
            .header("Cache-Control", "no-cache, no-store")
            .timeout(timeout)
            .json(&serde_json::json!({"token": capability.token}));
        drop(capability);
        if !self.is_alive() {
            return Err(HandoffError::NotSent);
        }
        let mut response = request.send().map_err(|_| HandoffError::Uncertain)?;
        if !self.is_alive() {
            return Err(HandoffError::Stale);
        }
        let status = response.status().as_u16();
        if status == 410 {
            return Err(HandoffError::Expired);
        }
        if status != 200 {
            return Err(HandoffError::Http(status));
        }
        let mut bytes = Vec::new();
        (&mut response)
            .take(crate::markdown_handoff::WIRE_LIMIT as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| HandoffError::Uncertain)?;
        if !self.is_alive() {
            return Err(HandoffError::Stale);
        }
        let snapshot =
            crate::markdown_handoff::Snapshot::decode(&bytes).map_err(HandoffError::Invalid)?;
        if !self.is_alive() {
            return Err(HandoffError::Stale);
        }
        Ok(snapshot)
    }
}
impl ReadClient {
    /// Read-only owned-origin editor state. Never retain untrusted response/error bodies.
    pub(crate) fn follow(&self) -> Result<crate::editor_follow::FollowResponse, Error> {
        if !self.is_alive() {
            return Err(Error::Stale);
        }
        let mut response = self
            .http
            .get(format!("{}/api/follow", self.origin))
            .header("Cache-Control", "no-cache, no-store")
            .timeout(Duration::from_secs(3))
            .send()
            .map_err(|_| Error::Transport("Follow request unavailable".into()))?;
        if !self.is_alive() {
            return Err(Error::Stale);
        }
        if response.status().as_u16() != 200 {
            return Err(Error::Http {
                status: response.status().as_u16(),
                body: String::new(),
            });
        }
        let mut bytes = Vec::new();
        (&mut response)
            .take(crate::editor_follow::WIRE_LIMIT as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| Error::Transport("Follow response unavailable".into()))?;
        if !self.is_alive() {
            return Err(Error::Stale);
        }
        crate::editor_follow::FollowResponse::decode(&bytes)
            .map_err(|reason| Error::Shape(reason.into()))
    }
}
impl ReadClient {
    fn require_canonical_capability(&self, timeout: Duration) -> Result<(), Error> {
        if !self.is_alive() {
            return Err(Error::Stale);
        }
        let mut response = self
            .http
            .get(format!("{}/api/capabilities", self.origin))
            .header("Cache-Control", "no-cache, no-store")
            .timeout(timeout)
            .send()
            .map_err(|e| Error::Transport(e.to_string()))?;
        if response.status().as_u16() != 200 {
            return Err(Error::Startup(format!(
                "Canonical capability endpoint returned HTTP {}",
                response.status().as_u16()
            )));
        }
        let json_type = response
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .and_then(|v| v.split(';').next())
            .is_some_and(|v| v.trim().eq_ignore_ascii_case("application/json"));
        if !json_type {
            return Err(Error::Shape(
                "Canonical capability requires application/json".into(),
            ));
        }
        let mut bytes = Vec::new();
        (&mut response)
            .take(4097)
            .read_to_end(&mut bytes)
            .map_err(|e| Error::Transport(e.to_string()))?;
        if bytes.len() > 4096 {
            return Err(Error::Oversized);
        }
        if !self.is_alive() {
            return Err(Error::Stale);
        }
        let capability: CanonicalCapability =
            serde_json::from_slice(&bytes).map_err(|e| Error::Shape(e.to_string()))?;
        capability.validate()
    }
    fn get<T: DeserializeOwned>(&self, path: &str, query: &[(&str, String)]) -> Result<T, Error> {
        self.get_with_timeout(path, query, None)
    }
    fn get_with_timeout<T: DeserializeOwned>(
        &self,
        path: &str,
        query: &[(&str, String)],
        timeout: Option<Duration>,
    ) -> Result<T, Error> {
        self.get_bounded(path, query, timeout, BODY_LIMIT)
    }
    fn get_bounded<T: DeserializeOwned>(
        &self,
        path: &str,
        query: &[(&str, String)],
        timeout: Option<Duration>,
        limit: u64,
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
            .take(limit + 1)
            .read_to_end(&mut bytes)
            .map_err(|e| Error::Transport(e.to_string()))?;
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        if bytes.len() as u64 > limit {
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
    /// Bounded read-only resolution on the owned server. Scope authority is captured
    /// and revalidated by the preview worker; this method never infers a vault.
    pub(crate) fn inline_viewspec(
        &self,
        vault: &str,
        spec: &str,
    ) -> Result<crate::numeric_chart::Chart, String> {
        const LIMIT: usize = 256 * 1024;
        crate::inline_viewspec::admit(spec)?;
        if vault.len() > 1024 || vault.contains('\0') {
            return Err("ViewSpec unavailable: invalid origin".into());
        }
        // Count JSON-string escaping before allocating the request payload.
        let wire_len = 11
            + spec
                .chars()
                .map(|c| match c {
                    '\"' | '\\' => 2,
                    c if c <= '\u{001f}' => 6,
                    c => c.len_utf8(),
                })
                .sum::<usize>();
        if wire_len > LIMIT {
            return Err("ViewSpec unavailable: request limit".into());
        }
        if !self.is_alive() {
            return Err("ViewSpec unavailable: server changed".into());
        }
        #[derive(serde::Serialize)]
        struct Request<'a> {
            spec: &'a str,
        }
        let mut response = self
            .http
            .post(format!("{}/api/viewspec", self.origin))
            .query(&[("vault", vault)])
            .header("Cache-Control", "no-cache, no-store")
            .timeout(Duration::from_secs(10))
            .json(&Request { spec })
            .send()
            .map_err(|_| "ViewSpec unavailable: request failed".to_string())?;
        if !self.is_alive() {
            return Err("ViewSpec unavailable: server changed".into());
        }
        if response.status().as_u16() != 200 {
            return Err("ViewSpec unavailable: engine rejected the request".into());
        }
        let json = response
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .and_then(|v| v.split(';').next())
            .is_some_and(|v| v.trim().eq_ignore_ascii_case("application/json"));
        if !json {
            return Err("ViewSpec unavailable: invalid response type".into());
        }
        if response
            .content_length()
            .is_some_and(|len| len > LIMIT as u64)
        {
            return Err("ViewSpec unavailable: response limit".into());
        }
        let mut bytes = Vec::new();
        (&mut response)
            .take(LIMIT as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| "ViewSpec unavailable: response read failed".to_string())?;
        if !self.is_alive() {
            return Err("ViewSpec unavailable: server changed".into());
        }
        if bytes.len() > LIMIT {
            return Err("ViewSpec unavailable: response limit".into());
        }
        let value: Value = serde_json::from_slice(&bytes)
            .map_err(|_| "ViewSpec unavailable: invalid JSON response".to_string())?;
        let option = value
            .as_object()
            .and_then(|v| v.get("echarts"))
            .filter(|v| v.is_object())
            .ok_or_else(|| "ViewSpec unavailable: missing chart response".to_string())?;
        crate::numeric_chart::decode(&option.to_string())
            .map_err(|_| "ViewSpec native rendering unsupported; original source retained".into())
    }
    /// Render-only POST. Returned Markdown is validated but never installed in the reader.
    pub(crate) fn included_notes(
        &self,
        vault: &str,
        body: &str,
    ) -> Result<crate::included_notes::Response, Error> {
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        if vault.len() > 1024 || vault.contains('\0') || body.len() as u64 > BODY_LIMIT {
            return Err(Error::Oversized);
        }
        let mut response = self
            .http
            .post(format!("{}/api/render", self.origin))
            .query(&vault_query(vault))
            .header("Cache-Control", "no-cache, no-store")
            .json(&serde_json::json!({"body":body}))
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
        if !(200..300).contains(&status) {
            return Err(Error::Http {
                status,
                body: String::from_utf8_lossy(&bytes).into_owned(),
            });
        }
        let value = serde_json::from_slice(&bytes).map_err(|e| Error::Json(e.to_string()))?;
        crate::included_notes::decode(value, vault)
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
    /// Engine-created order, regular notes only, one bounded scoped response.
    pub fn recently_created(&self, vault: &str, launch: &str) -> Result<Notes, Error> {
        if vault.len() > 1024
            || launch.len() > 1024
            || vault.contains('\0')
            || launch.contains('\0')
        {
            return Err(Error::Config("invalid recent-note scope".into()));
        }
        let mut args = vault_query(vault);
        args.extend([("sort", "created".into()), ("limit", "100".into())]);
        let mut data: Notes = self.get("/api/notes", &args)?;
        validate_recently_created(&mut data, vault, launch)?;
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

    /// Only the listing endpoint is used. Empty vault means the launch vault;
    /// canonical returned labels are retained so activation never loses identity.
    pub fn agenda(&self, date: &str, vault: &str) -> Result<AgendaResponse, Error> {
        if !calendar_date_valid(date) || vault.len() > 1024 || vault.contains('\0') {
            return Err(Error::Config(
                "agenda requires YYYY-MM-DD and a bounded vault name".into(),
            ));
        }
        let mut args = vault_query(vault);
        args.push(("date", date.into()));
        let mut result: AgendaResponse = self.get("/api/agenda", &args)?;
        if result.date != date || result.notes.len() > 4096 {
            return Err(Error::Shape(
                "agenda date mismatch or excessive notes".into(),
            ));
        }
        let mut identities = std::collections::HashSet::new();
        let mut launch_label: Option<String> = None;
        for note in &mut result.notes {
            if note.note_id <= 0
                || note.vault.len() > 1024
                || note.vault.contains('\0')
                || note.title.len() > 64 * 1024
                || note.path.len() > 16 * 1024
                || note.file_kind.is_empty()
                || note.file_kind.len() > 128
            {
                return Err(Error::Shape(
                    "invalid agenda note identity or metadata".into(),
                ));
            }
            if !vault.is_empty() {
                if !note.vault.is_empty() && note.vault != vault {
                    return Err(Error::Shape("agenda returned a different vault".into()));
                }
                note.vault = vault.into();
            } else if launch_label
                .as_ref()
                .is_some_and(|label| label != &note.vault)
            {
                return Err(Error::Shape("agenda mixed vault identities".into()));
            } else {
                launch_label = Some(note.vault.clone());
            }
            if !identities.insert((note.vault.clone(), note.note_id)) {
                return Err(Error::Shape("duplicate agenda note identity".into()));
            }
        }
        result.source_vault = vault.into();
        result.launch_generation = self.launch_generation;
        Ok(result)
    }

    /// Vault-wide dated tasks in every state. No id/open filter, task mutation,
    /// journal creation, recurrence expansion, or fallback vault request.
    pub fn dated_tasks(&self, vault: &str) -> Result<DatedTasksResponse, Error> {
        self.task_listing(vault, None)
    }
    /// Explicit engine-prioritized open dated tasks; preserve received order/rows.
    pub fn open_tasks(&self, vault: &str, launch_vault: &str) -> Result<DatedTasksResponse, Error> {
        self.task_listing(vault, Some(launch_vault))
    }
    fn task_listing(
        &self,
        vault: &str,
        open_launch: Option<&str>,
    ) -> Result<DatedTasksResponse, Error> {
        if vault.len() > 1024 || vault.contains('\0') {
            return Err(Error::Config(
                "dated tasks require a bounded vault name".into(),
            ));
        }
        let mut query = vault_query(vault);
        if open_launch.is_some() {
            query.push(("open", "1".into()));
        }
        let mut result: DatedTasksResponse = self.get("/api/tasks", &query)?;
        if open_launch
            .is_some_and(|launch| !calendar_range_vault_matches(vault, &result.vault, launch))
        {
            return Err(Error::Shape(
                "open tasks returned an unverified vault".into(),
            ));
        }
        if result.vault.len() > 1024
            || result.vault.contains('\0')
            || (!vault.is_empty() && !result.vault.is_empty() && result.vault != vault)
            || result.tasks.len() > 4096
        {
            return Err(Error::Shape(
                "dated tasks vault mismatch or excessive rows".into(),
            ));
        }
        if result.vault.is_empty() {
            result.vault = vault.into();
        }
        let mut identities = std::collections::HashSet::new();
        for task in &result.tasks {
            let terminal = match task.state.as_str() {
                "TODO" | "DOING" | "WAITING" => false,
                "DONE" | "CANCELLED" => true,
                _ => return Err(Error::Shape("unknown task state".into())),
            };
            if task.note_id <= 0
                || task.line == 0
                || task.line > i64::MAX as u64
                || task.done != terminal
                || (open_launch.is_none() && !identities.insert((task.note_id, task.line)))
                || (open_launch.is_some() && terminal)
                || task.file_kind.is_empty()
                || task.file_kind.len() > 128
                || task.title.len() > 64 * 1024
                || task.text.len() > 64 * 1024
                || task.path.len() > 16 * 1024
                || (!task.priority.is_empty()
                    && !(task.priority.len() == 1
                        && task.priority.as_bytes()[0].is_ascii_uppercase()))
                || [&task.due, &task.scheduled, &task.completed]
                    .iter()
                    .any(|date| !date.is_empty() && !date_wire_shape(date))
                || (task.due.is_empty() && task.scheduled.is_empty())
            {
                return Err(Error::Shape(
                    "invalid dated task identity, state, dates or metadata".into(),
                ));
            }
        }
        result.source_vault = vault.into();
        result.launch_generation = self.launch_generation;
        Ok(result)
    }

    /// Whole-vault up forest; the engine intentionally omits unplaced notes and cycles.
    pub fn hierarchy(&self, vault: &str) -> Result<HierarchyResponse, Error> {
        if vault.len() > 1024 || vault.contains('\0') {
            return Err(Error::Config(
                "hierarchy requires a bounded vault name".into(),
            ));
        }
        let mut wire: HierarchyWire = self.get("/api/hierarchy", &vault_query(vault))?;
        if wire.vault.len() > 1024
            || wire.vault.contains('\0')
            || (!vault.is_empty() && !wire.vault.is_empty() && wire.vault != vault)
        {
            return Err(Error::Shape("hierarchy vault mismatch".into()));
        }
        if wire.vault.is_empty() {
            wire.vault = vault.into();
        }
        let rows = flatten_hierarchy(wire.hierarchy, &wire.vault)?;
        Ok(HierarchyResponse {
            vault: wire.vault,
            rows,
            source_vault: vault.into(),
            launch_generation: self.launch_generation,
            diagnostics: wire.diagnostics,
        })
    }

    /// GET only; excludes deleted records and never creates journals/events.
    pub fn calendar_events(&self, vault: &str) -> Result<CalendarEventsResponse, Error> {
        if vault.len() > 1024 || vault.contains('\0') {
            return Err(Error::Config(
                "calendar events require a bounded vault name".into(),
            ));
        }
        let mut result: CalendarEventsResponse =
            self.get("/api/calendar-events", &vault_query(vault))?;
        validate_calendar_records(&mut result.vault, &result.events, vault)?;
        result.source_vault = vault.into();
        result.launch_generation = self.launch_generation;
        Ok(result)
    }

    /// Explicit UTC projection, with no legacy-list fallback or local filtering.
    pub fn calendar_day_events(
        &self,
        vault: &str,
        date: &str,
    ) -> Result<CalendarDayEventsResponse, Error> {
        let (day_start, day_end) = utc_day_bounds(date).ok_or_else(|| {
            Error::Config("calendar day requires a date in years 0001..9998".into())
        })?;
        if vault.len() > 1024 || vault.contains('\0') {
            return Err(Error::Config(
                "calendar events require a bounded vault name".into(),
            ));
        }
        let mut args = vault_query(vault);
        args.push(("date", date.into()));
        args.push(("time_zone", "UTC".into()));
        let mut result: CalendarDayEventsResponse = self.get("/api/calendar-events/day", &args)?;
        if result.date != date
            || result.time_zone != "UTC"
            || result.day_start != day_start
            || result.day_end != day_end
        {
            return Err(Error::Shape(
                "calendar UTC date or boundaries mismatch".into(),
            ));
        }
        validate_calendar_records(&mut result.vault, &result.events, vault)?;
        result.source_vault = vault.into();
        result.launch_generation = self.launch_generation;
        Ok(result)
    }

    /// Named display-zone projection. Zone authority and event membership stay
    /// in the engine; no host timezone discovery or legacy fallback is used.
    pub fn calendar_day_events_in_zone(
        &self,
        vault: &str,
        date: &str,
        zone: &str,
    ) -> Result<CalendarDayEventsResponse, Error> {
        self.calendar_day_events_zoned(vault, date, zone, None)
    }
    /// Agenda reads verify the raw wire namespace before legacy normalization.
    pub fn calendar_agenda_day(
        &self,
        vault: &str,
        launch: &str,
        date: &str,
        zone: &str,
    ) -> Result<CalendarDayEventsResponse, Error> {
        self.calendar_day_events_zoned(vault, date, zone, Some(launch))
    }
    fn calendar_day_events_zoned(
        &self,
        vault: &str,
        date: &str,
        zone: &str,
        launch: Option<&str>,
    ) -> Result<CalendarDayEventsResponse, Error> {
        let utc_bounds = utc_day_bounds(date).ok_or_else(|| {
            Error::Config("calendar day requires a date in years 0001..9998".into())
        })?;
        if vault.len() > 1024 || vault.contains('\0') {
            return Err(Error::Config(
                "calendar events require a bounded vault name".into(),
            ));
        }
        if zone.is_empty() || zone.len() > 128 || zone.chars().any(char::is_control) {
            return Err(Error::Config(
                "calendar day requires a bounded named time zone".into(),
            ));
        }
        let mut args = vault_query(vault);
        args.push(("date", date.into()));
        args.push(("time_zone", zone.into()));
        let mut result: CalendarDayEventsResponse = self.get("/api/calendar-events/day", &args)?;
        if launch.is_some_and(|launch| !calendar_range_vault_matches(vault, &result.vault, launch))
        {
            return Err(Error::Shape("calendar agenda wire vault mismatch".into()));
        }
        let valid_bounds = match (result.day_start_unix, result.day_end_unix) {
            (Some(start), Some(end)) => {
                start < end
                    && calendar_boundary_matches(&result.day_start, start)
                    && calendar_boundary_matches(&result.day_end, end)
            }
            _ => false,
        };
        if result.date != date
            || result.time_zone != zone
            || !valid_bounds
            || (zone == "UTC"
                && (result.day_start != utc_bounds.0 || result.day_end != utc_bounds.1))
        {
            return Err(Error::Shape(
                "calendar date, zone or boundaries mismatch".into(),
            ));
        }
        validate_calendar_records(&mut result.vault, &result.events, vault)?;
        result.source_vault = vault.into();
        result.launch_generation = self.launch_generation;
        Ok(result)
    }

    /// One bounded read, never 42 separate day requests or a local timezone fallback.
    pub fn calendar_range(
        &self,
        vault: &str,
        launch: &str,
        since: &str,
        until: &str,
        zone: &str,
    ) -> Result<CalendarRangeResponse, Error> {
        if launch.len() > 1024
            || launch.contains('\0')
            || range_dates(since, until).is_none()
            || vault.len() > 1024
            || vault.contains('\0')
            || zone.is_empty()
            || zone.len() > 128
            || zone.chars().any(char::is_control)
        {
            return Err(Error::Config(
                "calendar range requires <=42 civil dates, a vault and a display zone".into(),
            ));
        }
        let mut args = vault_query(vault);
        args.extend([
            ("since", since.into()),
            ("until", until.into()),
            ("time_zone", zone.into()),
        ]);
        let mut response: CalendarRangeResponse = self.get("/api/calendar-events/range", &args)?;
        validate_calendar_range(&response, vault, launch, since, until, zone)?;
        response.source_vault = vault.into();
        response.launch_generation = self.launch_generation;
        Ok(response)
    }

    /// Read an already verified index; 503 remains unavailable, never an empty month.
    pub fn calendar_content(
        &self,
        vault: &str,
        launch: &str,
        since: &str,
        until: &str,
        zone: &str,
    ) -> Result<CalendarContentResponse, Error> {
        if range_dates(since, until).is_none()
            || vault.len() > 1024
            || launch.len() > 1024
            || vault.contains('\0')
            || launch.contains('\0')
            || zone.is_empty()
            || zone.len() > 128
            || zone.chars().any(char::is_control)
        {
            return Err(Error::Config(
                "calendar content requires a bounded scope, civil range and zone".into(),
            ));
        }
        let mut args = vault_query(vault);
        args.extend([
            ("since", since.into()),
            ("until", until.into()),
            ("time_zone", zone.into()),
        ]);
        let mut response: CalendarContentResponse =
            self.get_bounded("/api/calendar-activity/range", &args, None, 512 * 1024)?;
        validate_calendar_content(&response, vault, launch, since, until, zone)?;
        response.source_vault = vault.into();
        response.launch_generation = self.launch_generation;
        Ok(response)
    }

    /// Fetch a raster from the owned engine only. Lexical validation cannot prove
    /// physical filesystem containment: the engine owns asset symlink policy.
    pub fn raster_asset(
        &self,
        reference: &str,
        vault: &str,
    ) -> Result<crate::raster::Decoded, Error> {
        self.raster_variant(reference, vault, false)
    }

    /// Same owned local transport as standalone images, with bounded retention.
    /// Call only from the owned background worker.
    pub fn raster_thumbnail(
        &self,
        reference: &str,
        vault: &str,
    ) -> Result<crate::raster::Decoded, Error> {
        self.raster_variant(reference, vault, true)
    }

    fn raster_variant(
        &self,
        reference: &str,
        vault: &str,
        thumbnail: bool,
    ) -> Result<crate::raster::Decoded, Error> {
        let name = crate::raster::asset_name(reference).map_err(Error::Config)?;
        if vault.len() > 1024 || vault.contains('\0') {
            return Err(Error::Config("image requires a bounded vault name".into()));
        }
        let origin = reqwest::Url::parse(&self.origin).map_err(|e| Error::Config(e.to_string()))?;
        if origin.scheme() != "http"
            || origin.host_str() != Some("127.0.0.1")
            || !origin.username().is_empty()
            || origin.password().is_some()
            || origin.path() != "/"
            || origin.query().is_some()
            || origin.fragment().is_some()
        {
            return Err(Error::Config(
                "image origin must be the owned loopback server".into(),
            ));
        }
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        let mut args = if thumbnail {
            vec![("vault", vault.to_owned())]
        } else {
            vault_query(vault)
        };
        args.push(("name", name.into()));
        let mut response = self
            .http
            .get(format!("{}/api/asset", self.origin))
            .query(&args)
            .header("Cache-Control", "no-cache, no-store")
            .send()
            .map_err(|e| Error::Transport(e.to_string()))?;
        let status = response.status().as_u16();
        let mime = response
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|value| value.to_str().ok())
            .unwrap_or("")
            .to_owned();
        if response
            .content_length()
            .is_some_and(|n| n > crate::raster::MAX_ENCODED as u64)
        {
            return Err(Error::Oversized);
        }
        let mut bytes = Vec::new();
        (&mut response)
            .take(crate::raster::MAX_ENCODED as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(|e| Error::Transport(e.to_string()))?;
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        if bytes.len() > crate::raster::MAX_ENCODED {
            return Err(Error::Oversized);
        }
        if status != 200 {
            return Err(Error::Http {
                status,
                body: String::from_utf8_lossy(&bytes[..bytes.len().min(1024)]).into_owned(),
            });
        }
        let decoded = if thumbnail {
            crate::raster::decode_thumbnail(&bytes, &mime)
        } else {
            crate::raster::decode(&bytes, &mime)
        }
        .map_err(Error::Shape)?;
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        Ok(decoded)
    }

    /// Explicit bounded PDF bytes from the owned engine. Never a URL or local
    /// filesystem open; physical asset containment remains the engine contract.
    pub(crate) fn pdf_asset(&self, reference: &str, vault: &str) -> Result<Vec<u8>, Error> {
        let name = crate::reader_pdf::asset_name(reference).map_err(Error::Config)?;
        if vault.len() > 1024 || vault.contains('\0') {
            return Err(Error::Config(
                "PDF requires an exact canonical vault".into(),
            ));
        }
        let origin = reqwest::Url::parse(&self.origin).map_err(|e| Error::Config(e.to_string()))?;
        if origin.scheme() != "http"
            || origin.host_str() != Some("127.0.0.1")
            || !origin.username().is_empty()
            || origin.password().is_some()
            || origin.path() != "/"
            || origin.query().is_some()
            || origin.fragment().is_some()
        {
            return Err(Error::Config(
                "PDF origin must be the owned loopback server".into(),
            ));
        }
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        let mut response = self
            .http
            .get(format!("{}/api/asset", self.origin))
            .query(&[("vault", vault), ("name", name)])
            .header("Cache-Control", "no-cache, no-store")
            .timeout(Duration::from_secs(3))
            .send()
            .map_err(|e| Error::Transport(e.to_string()))?;
        if response.status().as_u16() != 200 {
            return Err(Error::Shape("PDF asset unavailable".into()));
        }
        let mime = response
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .unwrap_or("")
            .split(';')
            .next()
            .unwrap_or("")
            .trim();
        if !mime.eq_ignore_ascii_case("application/pdf") {
            return Err(Error::Shape("PDF content type rejected".into()));
        }
        if response
            .content_length()
            .is_some_and(|n| n > crate::pdf_probe::MAX_INPUT as u64)
        {
            return Err(Error::Oversized);
        }
        let mut bytes = Vec::new();
        (&mut response)
            .take(crate::pdf_probe::MAX_INPUT as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(|e| Error::Transport(e.to_string()))?;
        if !self.alive.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        crate::pdf_probe::admit(&bytes, 1, crate::pdf_probe::MAX_EDGE)
            .map_err(|e| Error::Shape(e.into()))?;
        Ok(bytes)
    }

    pub fn journal_lookup(
        &self,
        title: &str,
        vault: &str,
        launch: &str,
    ) -> Result<JournalLookup, Error> {
        if !journal_title_valid(title)
            || vault.len() > 1024
            || launch.len() > 1024
            || vault.contains('\0')
            || launch.contains('\0')
        {
            return Err(Error::Config("Invalid journal lookup identity.".into()));
        }
        let mut args = vault_query(vault);
        args.push(("title", title.into()));
        let mut response: JournalLookup =
            self.get_bounded("/api/journal/lookup", &args, None, 8192)?;
        if response.vault.len() > 1024
            || response.vault.contains('\0')
            || !calendar_range_vault_matches(vault, &response.vault, launch)
            || response.title != title
            || response.found != response.note.is_some()
            || response
                .note
                .as_ref()
                .is_some_and(|n| n.note_id <= 0 || n.file_kind != "journal" || n.title != title)
        {
            return Err(Error::Shape("Journal lookup identity mismatch.".into()));
        }
        response.source_vault = vault.into();
        response.launch_generation = self.launch_generation;
        Ok(response)
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

    // The launch view uses an empty wire label, even when requested by its
    // registered name. Only a separately verified launch identity permits that
    // alias; a foreign graph must never inherit the active namespace.
    fn graph_scope_inputs(vault: &str, launch: &str) -> Result<(), Error> {
        if [vault, launch]
            .iter()
            .any(|name| name.len() > 1024 || name.chars().any(char::is_control))
        {
            return Err(Error::Config("Invalid graph vault scope".into()));
        }
        Ok(())
    }
    fn scoped_graph_response(
        mut value: Value,
        vault: &str,
        launch: &str,
    ) -> Result<(crate::graph::Graph, Fields), Error> {
        let canonical = if vault.is_empty() { launch } else { vault };
        let normalize = |object: &mut serde_json::Map<String, Value>| -> Result<(), Error> {
            match object.get("vault") {
                // Missing metadata is the legacy single-scoped response shape.
                None => {}
                Some(Value::String(wire)) if calendar_range_vault_matches(vault, wire, launch) => {}
                _ => return Err(Error::Shape("graph vault identity mismatch".into())),
            }
            object.insert("vault".into(), Value::String(canonical.into()));
            Ok(())
        };
        normalize(
            value
                .as_object_mut()
                .ok_or_else(|| Error::Shape("Invalid graph response".into()))?,
        )?;
        if let Some(nodes) = value
            .pointer_mut("/graph/nodes")
            .and_then(Value::as_array_mut)
        {
            for node in nodes {
                normalize(
                    node.as_object_mut()
                        .ok_or_else(|| Error::Shape("Invalid graph node".into()))?,
                )?;
            }
        }
        let response: crate::graph::GraphResponse =
            serde_json::from_value(value.clone()).map_err(|e| Error::Shape(e.to_string()))?;
        let mut fields: Fields =
            serde_json::from_value(value).map_err(|e| Error::Shape(e.to_string()))?;
        fields.remove("graph");
        Ok((response.graph, fields))
    }

    pub fn full_graph(
        &self,
        vault: &str,
        launch: &str,
    ) -> Result<(crate::graph::Graph, Fields), Error> {
        Self::graph_scope_inputs(vault, launch)?;
        let value: Value = self.get("/api/graph", &vault_query(vault))?;
        let (graph, fields) = Self::scoped_graph_response(value, vault, launch)?;
        if graph.center_id != 0 {
            return Err(Error::Shape("full graph center identity mismatch".into()));
        }
        Ok((graph, fields))
    }

    pub fn local_graph(
        &self,
        key: &NoteKey,
        launch: &str,
    ) -> Result<(crate::graph::Graph, Fields), Error> {
        Self::graph_scope_inputs(&key.vault, launch)?;
        let mut args = vault_query(&key.vault);
        args.push(("id", key.note_id.to_string()));
        let value: Value = self.get("/api/graph/local", &args)?;
        let (graph, fields) = Self::scoped_graph_response(value, &key.vault, launch)?;
        if graph.center_id != key.note_id {
            return Err(Error::Shape("graph center identity mismatch".into()));
        }
        Ok((graph, fields))
    }

    /// Refresh-only strict namespace proof before legacy alias normalization.
    pub(crate) fn note_for_refresh(
        &self,
        key: &NoteKey,
        launch: &str,
    ) -> Result<NoteResponse, Error> {
        let mut args = vault_query(&key.vault);
        args.push(("id", key.note_id.to_string()));
        let mut data: NoteResponse = self.get("/api/note", &args)?;
        if data.note.note.note_id != key.note_id
            || !calendar_range_vault_matches(&key.vault, &data.note.note.vault, launch)
        {
            return Err(Error::Shape(
                "Refresh note namespace/identity mismatch".into(),
            ));
        }
        data.note.note.vault = key.vault.clone();
        Ok(data)
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
/// Captured only for explicit disposable developer opt-in, then bound to the
/// owned launch config before spawning. Independent of any existing note path.
#[derive(Clone, Debug)]
pub struct CreateFixture {
    root: FileIdentity,
    vault_input: PathBuf,
    config: FileIdentity,
    cache: FileIdentity,
    marker: FileIdentity,
    config_bytes: Vec<u8>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct FileIdentity {
    path: PathBuf,
    input: PathBuf,
    device: u64,
    inode: u64,
}
impl FileIdentity {
    fn capture(path: &std::path::Path) -> Result<Self, String> {
        use std::os::unix::fs::MetadataExt;
        let canonical = path
            .canonicalize()
            .map_err(|e| format!("Fixture binding: {e}"))?;
        let metadata = std::fs::metadata(path).map_err(|e| format!("Fixture binding: {e}"))?;
        Ok(Self {
            path: canonical,
            input: path.to_owned(),
            device: metadata.dev(),
            inode: metadata.ino(),
        })
    }
    fn unchanged(&self) -> bool {
        Self::capture(&self.input).as_ref() == Ok(self)
    }
}
/// Permission to run the current engine only against an isolated disposable fixture.
/// This proof never enables editor, creation or persistence actions.
#[derive(Clone, Debug)]
pub struct ReadFixture(CreateFixture);
impl ReadFixture {
    pub fn capture(
        root: &std::path::Path,
        vault: &std::path::Path,
        config: Option<&std::path::Path>,
        cache: Option<&std::path::Path>,
    ) -> Result<Self, String> {
        let proof = Self(CreateFixture::capture(root, vault, config, cache)?);
        proof.verify_paths(vault, config, cache)?;
        Ok(proof)
    }
    pub(crate) fn verify_paths(
        &self,
        vault: &std::path::Path,
        config: Option<&std::path::Path>,
        cache: Option<&std::path::Path>,
    ) -> Result<(), String> {
        if vault != self.0.vault_input
            || config != Some(self.0.config.input.as_path())
            || cache != Some(self.0.cache.input.as_path())
        {
            return Err("Disposable read fixture launch paths changed.".into());
        }
        self.0.verify()?;
        for path in [
            &self.0.vault_input,
            &self.0.config.input,
            &self.0.cache.input,
        ] {
            if path.canonicalize().ok().as_ref() != Some(path) {
                return Err(
                    "Read fixture paths must be canonical, without symlink aliases.".into(),
                );
            }
        }
        Self::isolated_tree(&self.0.root.path)?;
        Self::isolated_tree(&self.0.cache.path)?;
        Self::single_file(&self.0.config.path)?;
        if std::str::from_utf8(&self.0.config_bytes)
            .ok()
            .map(str::trim)
            != Some("{}")
        {
            return Err("Disposable read fixture requires config containing only {}: registered peers and inherited settings are not allowed.".into());
        }
        Ok(())
    }
    fn single_file(path: &std::path::Path) -> Result<(), String> {
        use std::os::unix::fs::MetadataExt;
        let m = std::fs::symlink_metadata(path).map_err(|e| e.to_string())?;
        if !m.is_file() || m.nlink() != 1 {
            return Err(
                "Read fixture requires single-link regular files, no hardlinks or symlinks.".into(),
            );
        }
        Ok(())
    }
    fn isolated_tree(root: &std::path::Path) -> Result<(), String> {
        let mut pending = vec![(root.to_owned(), 0usize)];
        let mut count = 0usize;
        while let Some((dir, depth)) = pending.pop() {
            if depth > 64
                || !std::fs::symlink_metadata(&dir)
                    .map_err(|e| e.to_string())?
                    .is_dir()
            {
                return Err("Read fixture directory boundary is invalid.".into());
            }
            for entry in std::fs::read_dir(dir).map_err(|e| e.to_string())? {
                let path = entry.map_err(|e| e.to_string())?.path();
                count += 1;
                if count > 50_000 {
                    return Err("Read fixture tree exceeds 50000 entries.".into());
                }
                let m = std::fs::symlink_metadata(&path).map_err(|e| e.to_string())?;
                if m.is_dir() {
                    pending.push((path, depth + 1));
                } else {
                    Self::single_file(&path)?;
                }
            }
        }
        Ok(())
    }
    fn verify_launch(&self, config: &LaunchConfig) -> Result<(), String> {
        let Vault::Path(vault) = &config.vault else {
            return Err("Registered vault launch is disabled.".into());
        };
        self.verify_paths(vault, config.config.as_deref(), config.cache.as_deref())
    }
}
impl CreateFixture {
    pub fn capture(
        root: &std::path::Path,
        vault: &std::path::Path,
        config: Option<&std::path::Path>,
        cache: Option<&std::path::Path>,
    ) -> Result<Self, String> {
        let root = crate::workspace_edit::validate_fixture(root, vault, config, cache)?;
        let config = config.ok_or("Creation requires isolated config.")?;
        let cache = cache.ok_or("Creation requires isolated cache.")?;
        let config_bytes = Self::read_config(config)?;
        let result = Self {
            marker: FileIdentity::capture(&root.join(".track-native-edit-fixture"))?,
            root: FileIdentity::capture(&root)?,
            vault_input: vault.to_owned(),
            config: FileIdentity::capture(config)?,
            cache: FileIdentity::capture(cache)?,
            config_bytes,
        };
        result.verify()?;
        Ok(result)
    }
    fn read_config(path: &std::path::Path) -> Result<Vec<u8>, String> {
        let mut bytes = Vec::new();
        std::fs::File::open(path)
            .map_err(|e| e.to_string())?
            .take(1024 * 1024 + 1)
            .read_to_end(&mut bytes)
            .map_err(|e| e.to_string())?;
        if bytes.len() > 1024 * 1024 {
            return Err("Fixture config exceeds creation verification limit.".into());
        }
        Ok(bytes)
    }
    /// Creation chooses a new path, so checking only an existing note would miss
    /// symlinked output directories. Small disposable trees only; never follow
    /// links or special files while proving this conservative fixture boundary.
    fn verify_tree(&self) -> Result<(), String> {
        let mut pending = vec![(self.root.path.clone(), 0usize)];
        let mut count = 0usize;
        while let Some((directory, depth)) = pending.pop() {
            if depth > 64 {
                return Err("Fixture traversal depth exceeded.".into());
            }
            for entry in std::fs::read_dir(directory).map_err(|e| e.to_string())? {
                let entry = entry.map_err(|e| e.to_string())?;
                count += 1;
                if count > 50_000 {
                    return Err("Fixture traversal entry limit exceeded.".into());
                }
                let kind = entry.file_type().map_err(|e| e.to_string())?;
                if kind.is_symlink() || !(kind.is_file() || kind.is_dir()) {
                    return Err(
                        "Creation fixture must not contain symlinks or special files.".into(),
                    );
                }
                if kind.is_dir() {
                    pending.push((entry.path(), depth + 1));
                }
            }
        }
        Ok(())
    }
    fn verify(&self) -> Result<(), String> {
        if self.vault_input.canonicalize().ok().as_ref() != Some(&self.root.path)
            || !self.root.unchanged()
            || !self.config.unchanged()
            || !self.cache.unchanged()
            || !self.marker.unchanged()
            || Self::read_config(&self.config.path)? != self.config_bytes
        {
            return Err(
                "Fixture root/config/cache binding changed; no creation is permitted.".into(),
            );
        }
        crate::workspace_edit::validate_fixture(
            &self.root.path,
            &self.root.path,
            Some(&self.config.path),
            Some(&self.cache.path),
        )?;
        self.verify_tree()?;
        Ok(())
    }
    fn verify_launch(&self, launch: &LaunchConfig) -> Result<(), String> {
        if !matches!(&launch.vault, Vault::Path(path) if path == &self.vault_input)
            || launch.config.as_ref() != Some(&self.config.input)
            || launch.cache.as_ref() != Some(&self.cache.input)
        {
            return Err("Creation fixture does not match exact owned launch paths.".into());
        }
        self.verify()
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CreateAck {
    pub launch_generation: u64,
    pub vault: String,
    pub note_id: i64,
    pub title: String,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum CreateError {
    NotSent(String),
    Rejected { status: u16 },
    Uncertain(String),
}
impl fmt::Display for CreateError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NotSent(reason) => write!(f, "Not created: request not sent. {reason}"),
            Self::Rejected { status: 409 } => {
                f.write_str("Creation conflict (HTTP 409); no overwrite or automatic retry.")
            }
            Self::Rejected { status } => {
                write!(f, "Creation rejected (HTTP {status}); title retained.")
            }
            Self::Uncertain(reason) => write!(
                f,
                "Creation may have happened. {reason} Do not retry automatically."
            ),
        }
    }
}
impl ReadClient {
    pub fn creation_available(&self) -> bool {
        self.is_alive() && self.creation_fixture.is_some()
    }
    // Background-only: clones share a single lock owner. Ordinary clients never
    // inspect or create receipt files, even if a cache path happens to exist.
    fn with_creation_store<T>(
        &self,
        action: impl FnOnce(&mut crate::session_store::CreationStore) -> Result<T, String>,
    ) -> Result<T, String> {
        let fixture = self
            .creation_fixture
            .as_ref()
            .ok_or("Creation receipt storage is not enabled.")?;
        fixture.verify()?;
        let mut owner = self
            .creation_store
            .lock()
            .map_err(|_| "Creation receipt owner failed.")?;
        if owner.is_none() {
            *owner = Some(crate::session_store::CreationStore::open(
                &fixture.root.path,
                &fixture.config.path,
                &fixture.cache.path,
            ));
        }
        match owner.as_mut().unwrap() {
            Ok(store) => action(store),
            Err(error) => Err(error.clone()),
        }
    }
    pub fn creation_snapshot(
        &self,
    ) -> Result<Option<crate::session_store::CreationRecord>, String> {
        self.with_creation_store(|store| store.inspect())
    }
    /// Revalidate and sync the exact retained evidence. This never resolves a
    /// mutation, deletes its receipt, opens a note, or authorizes another POST.
    pub fn retain_uncertain_creation(
        &self,
        exact: &crate::session_store::CreationRecord,
    ) -> Result<(), String> {
        if exact.outcome != crate::session_store::CreationOutcome::Pending {
            return Err("Only an unresolved creation receipt can be acknowledged here.".into());
        }
        self.with_creation_store(|store| store.verify_retained(exact))
    }
    /// One title-only POST. No body override, implicit PUT, redirect or retry.
    /// The Workspace owns pending/uncertain outcomes and prevents close/quit.
    pub fn create_note(
        &self,
        title: &str,
        scope: &str,
        expected_launch: u64,
    ) -> Result<CreateAck, CreateError> {
        let title = title.trim();
        let preflight = (|| -> Result<(), String> {
            if title.is_empty() || !scope.is_empty() {
                return Err("A nonblank title and launch-vault scope are required.".into());
            }
            if !self.is_alive() || self.launch_generation != expected_launch {
                return Err("Owned launch changed.".into());
            }
            let fixture = self
                .creation_fixture
                .as_ref()
                .ok_or("Disposable fixture creation is not enabled.")?;
            // Controlled disposable fixture, not a hostile-filesystem sandbox.
            fixture.verify()?;
            if !self.is_alive() {
                return Err("Owned launch changed.".into());
            }
            Ok(())
        })();
        preflight.map_err(CreateError::NotSent)?;
        // Hold the sole receipt owner through dispatch and checkpoint completion.
        // Durable Pending is acknowledged before send. A crash even immediately
        // before send is intentionally unresolved after restart, never retried.
        self.with_creation_store(|store| {
            let exact = store.begin(title)?;
            Ok(self.send_creation_once(title, store, &exact))
        })
        .map_err(CreateError::NotSent)?
    }
    fn send_creation_once(
        &self,
        title: &str,
        store: &mut crate::session_store::CreationStore,
        exact: &crate::session_store::CreationRecord,
    ) -> Result<CreateAck, CreateError> {
        // Readiness and pinned binding are rechecked after the intent fsync and
        // immediately before dispatch. A known pre-send failure is checkpointed.
        let ready = (|| -> Result<(), String> {
            let storage = self.storage().map_err(|e| e.to_string())?;
            if storage.phase != "ready"
                || storage.read_only
                || degraded_save_data(&serde_json::to_value(&storage.additional).unwrap())
            {
                return Err("Storage must explicitly be ready and writable.".into());
            }
            self.creation_fixture
                .as_ref()
                .ok_or("Fixture unavailable.")?
                .verify()?;
            if !self.is_alive() {
                return Err("Owned launch changed.".into());
            }
            Ok(())
        })();
        if let Err(reason) = ready {
            store
                .finish(exact, crate::session_store::CreationOutcome::NotSent)
                .map_err(|e| {
                    CreateError::Uncertain(format!(
                        "No POST sent, but the intent checkpoint could not be resolved: {e}"
                    ))
                })?;
            return Err(CreateError::NotSent(reason));
        }
        // The blocking readiness read could race receipt/lock replacement. Verify
        // exact durable ownership again before dispatch, not only after response.
        store.verify_retained(exact).map_err(CreateError::NotSent)?;
        let mut response = self
            .http
            .post(format!("{}/api/note", self.origin))
            .json(&serde_json::json!({"title": title}))
            .send()
            .map_err(|_| CreateError::Uncertain("Response unavailable.".into()))?;
        let status = response.status().as_u16();
        let mut bytes = Vec::new();
        (&mut response)
            .take(64 * 1024 + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| CreateError::Uncertain("Response was interrupted.".into()))?;
        if !self.is_alive() || bytes.len() > 64 * 1024 {
            return Err(CreateError::Uncertain(
                "Response or launch could not be verified.".into(),
            ));
        }
        if matches!(status, 400 | 403 | 404 | 405 | 409) {
            store
                .finish(
                    exact,
                    crate::session_store::CreationOutcome::Rejected { status },
                )
                .map_err(|e| {
                    CreateError::Uncertain(format!("HTTP {status}; receipt checkpoint failed: {e}"))
                })?;
            return Err(CreateError::Rejected { status });
        }
        if status != 200 {
            return Err(CreateError::Uncertain(format!(
                "Server returned HTTP {status}."
            )));
        }
        #[derive(Deserialize)]
        struct Ack {
            created: bool,
            vault: String,
            note_id: i64,
            title: String,
        }
        let ack: Ack = serde_json::from_slice(&bytes)
            .map_err(|_| CreateError::Uncertain("Malformed acknowledgement.".into()))?;
        if !ack.created
            || !ack.vault.is_empty()
            || ack.note_id <= 0
            || ack.note_id > 9_007_199_254_740_991
            || ack.title != title
        {
            return Err(CreateError::Uncertain(
                "Acknowledgement identity mismatch.".into(),
            ));
        }
        store.finish(exact, crate::session_store::CreationOutcome::Created {
            note_id: ack.note_id, title: ack.title.clone(),
        }).map_err(|e| CreateError::Uncertain(format!(
            "Server acknowledged note #{}, but durable receipt checkpoint failed: {e}. Original intent is retained; do not repeat creation.", ack.note_id
        )))?;
        Ok(CreateAck {
            launch_generation: self.launch_generation,
            vault: ack.vault,
            note_id: ack.note_id,
            title: ack.title,
        })
    }
    /// Read-only verification after an accepted receipt. Failure MUST retain the
    /// receipt as created, not issue another POST. Storage may advance on create.
    pub fn verify_created(&self, ack: &CreateAck) -> Result<(NoteResponse, Storage), Error> {
        if !self.is_alive()
            || ack.launch_generation != self.launch_generation
            || !ack.vault.is_empty()
            || ack.note_id <= 0
        {
            return Err(Error::Stale);
        }
        let fixture = self
            .creation_fixture
            .as_ref()
            .ok_or_else(|| Error::Config("Creation fixture unavailable.".into()))?;
        fixture.verify().map_err(Error::Config)?;
        let before = self.storage()?;
        let raw: Value = self.get("/api/note", &[("id", ack.note_id.to_string())])?;
        if raw
            .get("note")
            .and_then(|n| n.get("read_only"))
            .and_then(Value::as_bool)
            != Some(false)
        {
            return Err(Error::Shape(
                "Created source must explicitly confirm read_only=false.".into(),
            ));
        }
        let source: NoteResponse =
            serde_json::from_value(raw).map_err(|e| Error::Shape(e.to_string()))?;
        let after = self.storage()?;
        fixture.verify().map_err(Error::Config)?;
        writable_source(&after, &source)?;
        let path = std::path::Path::new(&source.note.note.path);
        let path = if path.is_absolute() {
            path.to_owned()
        } else {
            fixture.root.path.join(path)
        };
        if before.generation != after.generation
            || !self.is_alive()
            || source.note.note.note_id != ack.note_id
            || source.note.note.vault != ack.vault
            || source.note.note.title != ack.title
            || source.note.note.file_kind != "note"
            || !path
                .canonicalize()
                .ok()
                .is_some_and(|p| p.is_file() && p.starts_with(&fixture.root.path))
        {
            return Err(Error::Shape(
                "Created source is not verified in the same fixture.".into(),
            ));
        }
        Ok((source, after))
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
            if self.canonical_read_only {
                return Err(Error::Config(
                    "Canonical read-only session cannot save".into(),
                ));
            }
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

/// Eliminate environment-backed registry/config overrides before installing explicit paths.
pub(crate) fn clear_track_environment(command: &mut Command) {
    command.env_clear().env("PATH", "/usr/bin:/bin");
}

// The production variant still requires the existing disposable ReadFixture.
enum StartupAdmission {
    Fixture,
    #[cfg(test)]
    SyntheticCanonical(read_inputs::SyntheticCanonical),
    #[cfg(test)]
    SyntheticBound(bound_fixture::SyntheticBound),
}
impl StartupAdmission {
    fn verify(&self, config: &LaunchConfig) -> Result<(), Error> {
        match self {
            Self::Fixture => config.read_fixture.as_ref().ok_or_else(|| Error::Config("Ordinary vault launch remains disabled pending native verification. Use synthetic/JSON input or an explicitly verified disposable fixture.".into()))?.verify_launch(config).map_err(Error::Config),
            #[cfg(test)]
            Self::SyntheticCanonical(proof) => proof.verify().map_err(Error::Config),
            #[cfg(test)]
            Self::SyntheticBound(proof) => proof.verify(),
        }
    }
}

#[derive(Clone, Copy)]
enum EventPolicy {
    Sse,
    #[cfg(test)]
    FiniteOnly,
}

pub struct OwnedServer {
    event_policy: EventPolicy,
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
        Self::start_admitted(config, canceled, StartupAdmission::Fixture)
    }
    #[cfg(test)]
    fn start_synthetic_canonical(proof: read_inputs::SyntheticCanonical) -> Result<Self, Error> {
        Self::start_admitted(
            proof.launch(),
            &AtomicBool::new(false),
            StartupAdmission::SyntheticCanonical(proof),
        )
    }
    fn start_admitted(
        config: LaunchConfig,
        canceled: &AtomicBool,
        #[allow(unused_mut)] mut admission: StartupAdmission,
    ) -> Result<Self, Error> {
        config.validate()?;
        if canceled.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        admission.verify(&config)?;
        if let Some(fixture) = &config.creation_fixture {
            fixture.verify_launch(&config).map_err(Error::Config)?;
        }
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
            canonical_read_only: config.canonical_read_only(),
            creation_store: Default::default(),
            creation_fixture: config.creation_fixture.clone(),
            http,
            origin: format!("http://127.0.0.1:{}", config.port),
            alive,
            launch_generation: NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed),
        };
        let mut cmd = Command::new(&config.executable);
        match &admission {
            StartupAdmission::Fixture => {
                clear_track_environment(&mut cmd);
                cmd.env_remove("TRACK_VAULT")
                    .env_remove("TRACK_CONFIG")
                    .env_remove("TRACK_CACHE_DIR");
                match &config.vault {
                    Vault::Path(p) => {
                        cmd.env("TRACK_VAULT", p)
                            .env("HOME", p)
                            .env("XDG_CONFIG_HOME", p)
                            .current_dir(p);
                    }
                    Vault::Registered(n) => {
                        cmd.args(["--vault", n]);
                    }
                }
                if let Some(p) = &config.config {
                    cmd.env("TRACK_CONFIG", p);
                }
                if let Some(p) = &config.cache {
                    cmd.env("TRACK_CACHE_DIR", p)
                        .env("XDG_CACHE_HOME", p)
                        .env("TMPDIR", p);
                }
                cmd.args(["web", "--addr", &format!("127.0.0.1:{}", config.port)]);
                config.configure_read_mode(&mut cmd)?;
            }
            #[cfg(test)]
            StartupAdmission::SyntheticCanonical(proof) => {
                proof.configure(&mut cmd).map_err(Error::Config)?
            }
            #[cfg(test)]
            StartupAdmission::SyntheticBound(proof) => proof.configure(&mut cmd)?,
        }
        cmd.env(
            "TRACK_WEB_DESKTOP_PARENT_PID",
            std::process::id().to_string(),
        )
        .env("TRACK_WEB_DESKTOP_READY_TOKEN", &token)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
        if let Some(fixture) = &config.creation_fixture {
            fixture.verify_launch(&config).map_err(Error::Config)?;
        }
        admission.verify(&config)?;
        #[cfg(test)]
        if let StartupAdmission::SyntheticBound(proof) = &mut admission {
            proof.capture(&mut cmd, config.startup_timeout, canceled)?;
        }
        #[cfg(test)]
        if matches!(&admission, StartupAdmission::SyntheticBound(_)) {
            cmd.stdin(Stdio::piped());
        }
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
        let event_policy = match &admission {
            #[cfg(test)]
            StartupAdmission::SyntheticBound(_) => EventPolicy::FiniteOnly,
            _ => EventPolicy::Sse,
        };
        let mut owner = Self {
            event_policy,
            child: Some(child),
            client,
            logs,
            initial_vaults: Value::Null,
            initial_storage: None,
            storage_error: None,
        };
        let start = Instant::now();
        #[cfg(test)]
        if let StartupAdmission::SyntheticBound(proof) = &mut admission {
            // Establish owner cleanup and output drains before transmitting.
            // This shares the readiness deadline and does not own a process group.
            let payload = proof.take_serve_request()?;
            let stdin = owner.child.as_mut().and_then(|child| child.stdin.take())
                .ok_or_else(|| Error::Startup("Missing owned request pipe".into()))?;
            let mut request = request_pipe::RequestPipe::new(stdin, payload)?;
            loop {
                if canceled.load(Ordering::Acquire) {
                    owner.shutdown()?;
                    return Err(Error::Stale);
                }
                if start.elapsed() >= config.startup_timeout {
                    owner.shutdown()?;
                    return Err(Error::Startup("Bound request startup deadline exceeded".into()));
                }
                owner.check_running()?;
                if request.pump()? { break; }
                thread::sleep(Duration::from_millis(5));
            }
        }
        let mut last = String::new();
        let mut capability_verified = !config.canonical_read_only();
        while start.elapsed() < config.startup_timeout {
            if canceled.load(Ordering::Acquire) {
                owner.shutdown()?;
                return Err(Error::Stale);
            }
            owner.check_running()?;
            if ready.load(Ordering::Acquire) {
                if !capability_verified {
                    let timeout = config
                        .startup_timeout
                        .saturating_sub(start.elapsed())
                        .max(Duration::from_millis(1))
                        .min(config.request_timeout)
                        .min(Duration::from_secs(3));
                    #[cfg(test)]
                    let result = match &admission {
                        StartupAdmission::SyntheticBound(proof) => {
                            proof.capability(&owner.client, timeout)
                        }
                        _ => owner.client.require_canonical_capability(timeout),
                    };
                    #[cfg(not(test))]
                    let result = owner.client.require_canonical_capability(timeout);
                    if canceled.load(Ordering::Acquire) {
                        owner.shutdown()?;
                        return Err(Error::Stale);
                    }
                    if let Err(error) = result {
                        owner.shutdown()?;
                        return Err(Error::Startup(format!(
                            "Canonical read-only handshake failed; no fallback: {error}"
                        )));
                    }
                    owner.check_running()?;
                    if start.elapsed() >= config.startup_timeout {
                        owner.shutdown()?;
                        return Err(Error::Startup(
                            "Canonical capability startup deadline exceeded".into(),
                        ));
                    }
                    capability_verified = true;
                }
                #[cfg(test)]
                let admission_generation = if matches!(
                    &admission,
                    StartupAdmission::SyntheticCanonical(_) | StartupAdmission::SyntheticBound(_)
                ) {
                    let state: Result<Storage, Error> = owner.client.get_with_timeout(
                        "/api/storage",
                        &[],
                        Some(
                            config
                                .startup_timeout
                                .saturating_sub(start.elapsed())
                                .max(Duration::from_millis(1))
                                .min(config.request_timeout),
                        ),
                    );
                    match state {
                        Ok(state) if state.phase == "ready" && !state.read_only => {
                            Some(state.generation)
                        }
                        _ => {
                            thread::sleep(Duration::from_millis(20));
                            continue;
                        }
                    }
                } else {
                    None
                };
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
                        #[cfg(test)]
                        if let StartupAdmission::SyntheticCanonical(proof) = &admission {
                            match proof.verify_registry(&v) {
                                Ok(true) => {}
                                Ok(false) => {
                                    last = "Synthetic peers are initializing".into();
                                    thread::sleep(Duration::from_millis(20));
                                    continue;
                                }
                                Err(error) => {
                                    owner.shutdown()?;
                                    return Err(Error::Startup(error));
                                }
                            }
                        }
                        #[cfg(test)]
                        if let StartupAdmission::SyntheticCanonical(proof) = &admission {
                            if let Err(error) = proof.verify_bindings() {
                                let _ = owner.shutdown();
                                return Err(Error::Config(error));
                            }
                        }
                        #[cfg(test)]
                        if let StartupAdmission::SyntheticBound(proof) = &admission {
                            match proof.registry(&v) {
                                Ok(true) => {}
                                Ok(false) => {
                                    thread::sleep(Duration::from_millis(20));
                                    continue;
                                }
                                Err(error) => {
                                    owner.shutdown()?;
                                    return Err(error);
                                }
                            }
                            if let Err(error) = proof.proof.verify_bindings() {
                                let _ = owner.shutdown();
                                return Err(Error::Config(error));
                            }
                        }
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
                        #[cfg(test)]
                        if let Some(generation) = admission_generation {
                            if owner.initial_storage.as_ref().is_none_or(|s| {
                                s.phase != "ready" || s.read_only || s.generation != generation
                            }) {
                                last = "Synthetic storage verification changed".into();
                                thread::sleep(Duration::from_millis(20));
                                continue;
                            }
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
    events: Arc<crate::live_events::Observer>,
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
            events: Arc::new(match server.event_policy {
                EventPolicy::Sse => crate::live_events::Observer::start(
                    &server.client.origin,
                    server.client.alive.clone(),
                ),
                #[cfg(test)]
                EventPolicy::FiniteOnly => crate::live_events::Observer::unavailable(),
            }),
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
    pub fn events(&self) -> crate::live_events::Snapshot {
        self.events.snapshot()
    }
    pub fn failure(&self) -> Option<String> {
        self.status.lock().unwrap().clone()
    }
    /// Background-only barrier for retries. Never start a replacement if this fails.
    pub fn shutdown(&self) -> Result<(), Error> {
        self.events.cancel();
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
    fn authorize_read_fixture(config: &mut LaunchConfig) {
        let Vault::Path(root) = &config.vault else {
            panic!()
        };
        std::fs::create_dir_all(root).unwrap();
        std::fs::write(
            root.join(".track-native-edit-fixture"),
            "disposable-fixture-only\n",
        )
        .unwrap();
        std::fs::create_dir_all(config.cache.as_ref().unwrap()).unwrap();
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(
                config.cache.as_ref().unwrap(),
                std::fs::Permissions::from_mode(0o700),
            )
            .unwrap();
        }
        std::fs::write(config.config.as_ref().unwrap(), "{}\n").unwrap();
        config.read_fixture = Some(
            ReadFixture::capture(
                root,
                root,
                config.config.as_deref(),
                config.cache.as_deref(),
            )
            .unwrap(),
        );
    }
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
        let fixture = TemporaryFixture(root.canonicalize().unwrap());
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
        authorize_read_fixture(&mut config);
        config.creation_fixture = Some(
            CreateFixture::capture(
                &fixture.0.join("vault"),
                &fixture.0.join("vault"),
                config.config.as_deref(),
                config.cache.as_deref(),
            )
            .unwrap(),
        );
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
            event_policy: EventPolicy::Sse,
            child: Some(child),
            client: ReadClient { canonical_read_only: false, creation_store: Default::default(), creation_fixture: None, http: reqwest::blocking::Client::new(), origin: "http://127.0.0.1:1".into(), alive: Arc::new(AtomicBool::new(true)), launch_generation: 1 },
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
        put_gate: Option<(std::sync::mpsc::Sender<()>, std::sync::mpsc::Receiver<()>)>,
    ) -> (ReadClient, thread::JoinHandle<Vec<String>>) {
        gated_request_mock(replies, put_gate, "PUT ")
    }
    fn gated_request_mock(
        replies: Vec<Option<(u16, String)>>,
        mut put_gate: Option<(std::sync::mpsc::Sender<()>, std::sync::mpsc::Receiver<()>)>,
        gate_prefix: &'static str,
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
                if requests.last().unwrap().starts_with(gate_prefix) {
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
                canonical_read_only: false,
                creation_store: Default::default(),
                creation_fixture: None,
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
    struct CreationFixtureTest {
        root: PathBuf,
        binding: CreateFixture,
    }
    impl CreationFixtureTest {
        fn new() -> Self {
            let root = std::env::temp_dir().join(format!(
                "track-create-{}-{}",
                std::process::id(),
                NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed)
            ));
            std::fs::create_dir_all(root.join("vault")).unwrap();
            let root = root.canonicalize().unwrap();
            std::fs::create_dir(root.join("cache")).unwrap();
            {
                use std::os::unix::fs::PermissionsExt;
                std::fs::set_permissions(
                    root.join("cache"),
                    std::fs::Permissions::from_mode(0o700),
                )
                .unwrap();
            }
            std::fs::write(root.join("config"), "{}\n").unwrap();
            std::fs::write(
                root.join("vault/.track-native-edit-fixture"),
                "disposable-fixture-only\n",
            )
            .unwrap();
            let binding = CreateFixture::capture(
                &root.join("vault"),
                &root.join("vault"),
                Some(&root.join("config")),
                Some(&root.join("cache")),
            )
            .unwrap();
            Self { root, binding }
        }
        fn client(
            &self,
            replies: Vec<Option<(u16, String)>>,
        ) -> (ReadClient, thread::JoinHandle<Vec<String>>) {
            let (mut client, requests) = save_mock(replies);
            client.creation_fixture = Some(self.binding.clone());
            (client, requests)
        }
    }
    impl Drop for CreationFixtureTest {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.root);
        }
    }
    #[test]
    fn ordinary_launch_and_cli_never_execute_child() {
        use std::os::unix::fs::PermissionsExt;
        let f = CreationFixtureTest::new();
        let executable = f.root.join("would-run");
        let sentinel = f.root.join("executed");
        std::fs::write(
            &executable,
            format!("#!/bin/sh\ntouch '{}'\n", sentinel.display()),
        )
        .unwrap();
        std::fs::set_permissions(&executable, std::fs::Permissions::from_mode(0o700)).unwrap();
        let launch =
            LaunchConfig::new(executable.clone(), Vault::Path(f.root.join("vault")), 23000);
        assert!(matches!(OwnedServer::start(launch), Err(Error::Config(_))));
        let backend = crate::backend::Backend::new(executable, f.root.join("vault")).unwrap();
        assert!(matches!(
            backend.notes(),
            Err(crate::backend::BackendError::InvalidConfig(_))
        ));
        assert!(!sentinel.exists());
    }
    #[test]
    fn read_fixture_rejects_peers_hardlinks_cache_links_and_changed_bindings() {
        use std::os::unix::fs::symlink;
        for case in [
            "peer",
            "hardlink",
            "cache-hardlink",
            "cache-symlink",
            "config-hardlink",
            "changed",
        ] {
            let f = CreationFixtureTest::new();
            let vault = f.root.join("vault");
            let config = f.root.join("config");
            let cache = f.root.join("cache");
            let proof = ReadFixture::capture(&vault, &vault, Some(&config), Some(&cache)).unwrap();
            match case {
                "peer" => std::fs::write(&config, "vaults: {work: /outside}\n").unwrap(),
                "hardlink" => std::fs::hard_link(&config, vault.join("linked")).unwrap(),
                "cache-hardlink" => std::fs::hard_link(&config, cache.join("linked")).unwrap(),
                "cache-symlink" => symlink(&config, cache.join("linked")).unwrap(),
                "config-hardlink" => {
                    std::fs::hard_link(&config, f.root.join("config-alias")).unwrap()
                }
                _ => std::fs::remove_file(vault.join(".track-native-edit-fixture")).unwrap(),
            }
            assert!(
                proof
                    .verify_paths(&vault, Some(&config), Some(&cache))
                    .is_err(),
                "{case}"
            );
            assert!(
                ReadFixture::capture(&vault, &vault, Some(&config), Some(&cache)).is_err(),
                "{case}"
            );
        }
    }
    #[test]
    fn special_and_oversized_marker_are_rejected_before_read() {
        let f = CreationFixtureTest::new();
        let vault = f.root.join("vault");
        let marker = vault.join(".track-native-edit-fixture");
        std::fs::remove_file(&marker).unwrap();
        use std::os::unix::ffi::OsStrExt;
        let fifo = std::ffi::CString::new(marker.as_os_str().as_bytes()).unwrap();
        assert_eq!(
            unsafe { libc::mkfifo(fifo.as_ptr(), 0o600) },
            0,
            "mkfifo fixture failed: {}",
            std::io::Error::last_os_error()
        );
        assert!(
            ReadFixture::capture(
                &vault,
                &vault,
                Some(&f.root.join("config")),
                Some(&f.root.join("cache"))
            )
            .is_err()
        );
        std::fs::remove_file(&marker).unwrap();
        std::fs::write(&marker, vec![b'x'; 1024 * 1024]).unwrap();
        assert!(
            ReadFixture::capture(
                &vault,
                &vault,
                Some(&f.root.join("config")),
                Some(&f.root.join("cache"))
            )
            .is_err()
        );
    }
    #[test]
    #[ignore = "explicit TRACK_NATIVE_TEST_BINARY and TRACK_NATIVE_TEST_SYNTHETIC_ONLY=1; isolated actual-Go sentinel regression"]
    fn owned_and_cli_fixture_reject_inherited_peer_without_touching_sentinel() {
        use std::io::Write;
        use std::os::unix::fs::MetadataExt;
        const CHILD: &str = "TRACK_NATIVE_GATE_CHILD";
        const NAME: &str =
            "live::tests::owned_and_cli_fixture_reject_inherited_peer_without_touching_sentinel";
        let binary = PathBuf::from(
            std::env::var_os("TRACK_NATIVE_TEST_BINARY").expect("explicit matching Go binary"),
        );
        assert!(binary.is_absolute());
        assert_eq!(
            std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
            Ok("1")
        );
        fn wait(child: &mut Child, seconds: u64) {
            let until = Instant::now() + Duration::from_secs(seconds);
            loop {
                if let Some(status) = child.try_wait().unwrap() {
                    assert!(status.success(), "isolated process failed: {status}");
                    return;
                }
                if Instant::now() >= until {
                    let _ = child.kill();
                    let _ = child.wait();
                    panic!("isolated process deadline");
                }
                thread::sleep(Duration::from_millis(10));
            }
        }
        fn snapshot(
            root: &std::path::Path,
        ) -> std::collections::BTreeMap<PathBuf, (Vec<u8>, i64, i64)> {
            let mut pending = vec![root.to_owned()];
            let mut files = std::collections::BTreeMap::new();
            while let Some(dir) = pending.pop() {
                for e in std::fs::read_dir(dir).unwrap() {
                    let p = e.unwrap().path();
                    let m = std::fs::symlink_metadata(&p).unwrap();
                    if m.is_dir() {
                        pending.push(p);
                    } else {
                        assert!(m.is_file());
                        files.insert(
                            p.strip_prefix(root).unwrap().to_owned(),
                            (std::fs::read(&p).unwrap(), m.mtime(), m.mtime_nsec()),
                        );
                    }
                }
            }
            files
        }
        if let Some(root) = std::env::var_os(CHILD) {
            // This process alone has a hostile environment. No set_var pollution of parallel tests.
            let root = PathBuf::from(root);
            let vault = root.join("vault");
            let cache = root.join("cache");
            let config = root.join("config");
            let outside = root.join("outside");
            let before = snapshot(&outside);
            assert_eq!(
                std::env::var_os("TRACK_VAULTS_ESCAPE"),
                Some(outside.join("vault").into_os_string())
            );
            let proof = ReadFixture::capture(&vault, &vault, Some(&config), Some(&cache)).unwrap();
            let port = (24000..35000)
                .find(|p| {
                    TcpListener::bind(("127.0.0.1", *p)).is_ok()
                        && TcpListener::bind(("127.0.0.1", *p + 1)).is_ok()
                })
                .unwrap();
            let mut launch = LaunchConfig::new(binary.clone(), Vault::Path(vault.clone()), port);
            launch.config = Some(config.clone());
            launch.cache = Some(cache.clone());
            launch.read_fixture = Some(proof.clone());
            assert!(launch.creation_fixture.is_none());
            let mut server = OwnedServer::start(launch).unwrap();
            let registry = server.client().vaults().unwrap();
            let wire = registry.to_string();
            assert!(
                !wire.contains("escape") && !wire.contains(outside.to_str().unwrap()),
                "unexpected peer: {wire}"
            );
            assert!(!server.client().creation_available());
            server.shutdown().unwrap();
            assert_eq!(before, snapshot(&outside));
            let backend = crate::backend::Backend::new(binary, vault)
                .unwrap()
                .with_config(config)
                .unwrap()
                .with_cache(cache)
                .unwrap()
                .with_read_fixture(proof)
                .unwrap();
            let notes = backend.notes().unwrap();
            assert!(notes.value.notes.iter().any(|n| n.title == "Host fixture"));
            assert!(
                !notes
                    .value
                    .notes
                    .iter()
                    .any(|n| n.title == "Escape sentinel")
            );
            assert_eq!(before, snapshot(&outside));
            std::fs::write(root.join("peer-isolation-completed"), b"verified").unwrap();
            return;
        }
        let f = CreationFixtureTest::new();
        let outside = f.root.join("outside");
        std::fs::create_dir_all(outside.join("vault")).unwrap();
        std::fs::create_dir(outside.join("cache")).unwrap();
        std::fs::write(outside.join("config"), "{}\n").unwrap();
        for (root, title) in [(&f.root, "Host fixture"), (&outside, "Escape sentinel")] {
            // Legitimate empty layout: the fixture marker alone is not a vault.
            std::fs::create_dir_all(root.join("vault/note")).unwrap();
            let mut cmd = Command::new(&binary);
            clear_track_environment(&mut cmd);
            let mut child = cmd
                .args(["new", "--title", title])
                .env("TRACK_VAULT", root.join("vault"))
                .env("TRACK_CONFIG", root.join("config"))
                .env("TRACK_CACHE_DIR", root.join("cache"))
                .env("HOME", root)
                .env("XDG_CONFIG_HOME", root)
                .env("XDG_CACHE_HOME", root.join("cache"))
                .env("TMPDIR", root.join("cache"))
                .current_dir(root)
                .stdin(Stdio::piped())
                .stdout(Stdio::inherit())
                .spawn()
                .unwrap();
            child
                .stdin
                .take()
                .unwrap()
                .write_all(b"Synthetic isolated sentinel\n")
                .unwrap();
            wait(&mut child, 20);
        }
        // Make the outside note stale, so an accidental peer refresh has observable activity work.
        let note = std::fs::read_dir(outside.join("vault/note"))
            .unwrap()
            .map(|e| e.unwrap().path())
            .find(|p| p.extension().is_some_and(|x| x == "md"))
            .unwrap();
        std::fs::OpenOptions::new()
            .append(true)
            .open(&note)
            .unwrap()
            .write_all(b"External synthetic change\n")
            .unwrap();
        std::fs::File::open(&note)
            .unwrap()
            .set_modified(std::time::SystemTime::now() + Duration::from_secs(2 * 86_400))
            .unwrap();
        let before = snapshot(&outside);
        let mut child = Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                NAME,
                "--ignored",
                "--nocapture",
                "--test-threads=1",
            ])
            .env(CHILD, &f.root)
            .env("TRACK_NATIVE_TEST_BINARY", &binary)
            .env("TRACK_NATIVE_TEST_SYNTHETIC_ONLY", "1")
            .env("TRACK_VAULTS_ESCAPE", outside.join("vault"))
            .env("TRACK_VAULT", outside.join("vault"))
            .env("TRACK_CONFIG", outside.join("config"))
            .env("TRACK_CACHE_DIR", outside.join("cache"))
            .env("HTTP_PROXY", "http://127.0.0.1:1")
            .spawn()
            .unwrap();
        wait(&mut child, 60);
        assert_eq!(
            std::fs::read(f.root.join("peer-isolation-completed")).unwrap(),
            b"verified"
        );
        assert_eq!(before, snapshot(&outside));
    }
    #[test]
    fn child_environment_is_allowlisted_not_inherited() {
        let mut command = Command::new("/bin/sh");
        command
            .env("TRACK_VAULTS_WORK", "/outside")
            .env("LD_PRELOAD", "/outside.so")
            .env("HTTP_PROXY", "http://outside");
        clear_track_environment(&mut command);
        let vars: Vec<_> = command.get_envs().filter(|(_, v)| v.is_some()).collect();
        assert_eq!(
            vars,
            vec![(
                std::ffi::OsStr::new("PATH"),
                Some(std::ffi::OsStr::new("/usr/bin:/bin"))
            )]
        );
        let output = command.args(["-c", "env"]).output().unwrap();
        let env = String::from_utf8(output.stdout).unwrap();
        assert!(
            !env.contains("TRACK_") && !env.contains("LD_PRELOAD") && !env.contains("HTTP_PROXY")
        );
    }
    const CREATE_STORAGE: &str =
        r#"{"phase":"ready","read_only":false,"verified":0,"generation":2}"#;
    const CREATE_OK: &str = r#"{"created":true,"vault":"","note_id":42,"title":"Alpha"}"#;
    #[test]
    fn create_first_note_posts_title_only_once_and_never_puts() {
        let fixture = CreationFixtureTest::new();
        let (client, requests) = fixture.client(vec![
            Some((200, CREATE_STORAGE.into())),
            Some((200, CREATE_OK.into())),
        ]);
        let ack = client.create_note(" Alpha ", "", 1).unwrap();
        assert_eq!(ack.note_id, 42);
        assert_eq!(ack.vault, "");
        let requests = requests.join().unwrap();
        assert_eq!(requests.len(), 2);
        assert!(requests[0].starts_with("GET /api/storage "));
        assert!(requests[1].starts_with("POST /api/note "));
        assert_eq!(
            serde_json::from_str::<Value>(requests[1].split("\r\n\r\n").nth(1).unwrap()).unwrap(),
            serde_json::json!({"title":"Alpha"})
        );
        assert!(!requests.iter().any(|r| r.starts_with("PUT ")));
    }
    #[test]
    fn create_is_disabled_without_bound_fixture_and_for_invalid_scope_or_title() {
        let fixture = CreationFixtureTest::new();
        for (title, scope, launch, enabled) in [
            ("Alpha", "", 1, false),
            (" \n", "", 1, true),
            ("Alpha", "named", 1, true),
            ("Alpha", "", 99, true),
        ] {
            let (mut client, requests) = fixture.client(vec![]);
            if !enabled {
                client.creation_fixture = None;
            }
            assert!(matches!(
                client.create_note(title, scope, launch),
                Err(CreateError::NotSent(_))
            ));
            assert!(requests.join().unwrap().is_empty());
        }
    }
    #[test]
    fn create_binding_changes_send_no_request() {
        for change in ["config-content", "config-inode", "cache", "root", "marker"] {
            let fixture = CreationFixtureTest::new();
            let (client, requests) = fixture.client(vec![]);
            match change {
                "config-content" => {
                    std::fs::write(fixture.root.join("config"), "changed: true\n").unwrap()
                }
                "config-inode" => {
                    std::fs::rename(fixture.root.join("config"), fixture.root.join("old-config"))
                        .unwrap();
                    std::fs::write(fixture.root.join("config"), "{}\n").unwrap();
                }
                "cache" => {
                    std::fs::rename(fixture.root.join("cache"), fixture.root.join("old-cache"))
                        .unwrap();
                    std::fs::create_dir(fixture.root.join("cache")).unwrap();
                }
                "root" => {
                    std::fs::rename(fixture.root.join("vault"), fixture.root.join("old-vault"))
                        .unwrap();
                    std::fs::create_dir(fixture.root.join("vault")).unwrap();
                    std::fs::write(
                        fixture.root.join("vault/.track-native-edit-fixture"),
                        "disposable-fixture-only\n",
                    )
                    .unwrap();
                }
                _ => std::fs::remove_file(fixture.root.join("vault/.track-native-edit-fixture"))
                    .unwrap(),
            }
            assert!(matches!(
                client.create_note("Alpha", "", 1),
                Err(CreateError::NotSent(_))
            ));
            assert!(requests.join().unwrap().is_empty());
        }
    }
    #[test]
    fn symlinked_output_directory_or_file_never_receives_create() {
        use std::os::unix::fs::symlink;
        for directory in [true, false] {
            let fixture = CreationFixtureTest::new();
            let (client, requests) = fixture.client(vec![]);
            if directory {
                symlink(fixture.root.join("cache"), fixture.root.join("vault/note")).unwrap();
            } else {
                std::fs::create_dir(fixture.root.join("vault/note")).unwrap();
                symlink(
                    fixture.root.join("config"),
                    fixture.root.join("vault/note/escape.md"),
                )
                .unwrap();
            }
            assert!(matches!(
                client.create_note("Alpha", "", 1),
                Err(CreateError::NotSent(_))
            ));
            assert!(requests.join().unwrap().is_empty());
        }
    }
    #[test]
    fn two_explicit_sequential_creations_use_only_engine_returned_ids() {
        let fixture = CreationFixtureTest::new();
        let title = "雪 / \"quoted\"";
        let second =
            serde_json::json!({"created":true,"vault":"","note_id":43,"title":title}).to_string();
        let (client, requests) = fixture.client(vec![
            Some((200, CREATE_STORAGE.into())),
            Some((200, CREATE_OK.into())),
            Some((200, CREATE_STORAGE.into())),
            Some((200, second)),
        ]);
        let first = client.create_note("Alpha", "", 1).unwrap();
        let second = client.create_note(title, "", 1).unwrap();
        assert_ne!(first.note_id, second.note_id);
        assert_eq!(second.title, title);
        let requests = requests.join().unwrap();
        assert_eq!(
            requests.iter().filter(|r| r.starts_with("POST ")).count(),
            2
        );
        assert_eq!(
            serde_json::from_str::<Value>(requests[3].split("\r\n\r\n").nth(1).unwrap()).unwrap(),
            serde_json::json!({"title":title})
        );
        assert!(!requests.iter().any(|r| r.starts_with("PUT ")));
    }
    #[test]
    fn create_binding_cannot_authorize_a_different_launch() {
        let fixture = CreationFixtureTest::new();
        let mut launch = LaunchConfig::new(
            "/not-executed".into(),
            Vault::Path(fixture.root.join("vault")),
            20000,
        );
        launch.config = Some(fixture.root.join("config"));
        launch.cache = Some(fixture.root.join("cache"));
        assert!(fixture.binding.verify_launch(&launch).is_ok());
        launch.vault = Vault::Registered("other".into());
        assert!(fixture.binding.verify_launch(&launch).is_err());
    }
    #[test]
    fn create_read_only_unknown_or_unready_storage_never_posts() {
        let fixture = CreationFixtureTest::new();
        for storage in [
            CREATE_STORAGE.replace("false", "true"),
            CREATE_STORAGE.replace("ready", "initializing"),
            r#"{"phase":"ready","verified":0,"generation":2}"#.into(),
        ] {
            let (client, requests) = fixture.client(vec![Some((200, storage))]);
            assert!(matches!(
                client.create_note("Alpha", "", 1),
                Err(CreateError::NotSent(_))
            ));
            let requests = requests.join().unwrap();
            assert_eq!(requests.len(), 1);
            assert!(requests[0].starts_with("GET "));
        }
    }
    #[test]
    fn create_lost_malformed_mismatched_and_server_error_responses_never_retry() {
        let replies = [
            None,
            Some((500, "failure".into())),
            Some((503, "unavailable".into())),
            Some((201, CREATE_OK.into())),
            Some((200, "{}".into())),
            Some((200, CREATE_OK.replace("true", "false"))),
            Some((200, CREATE_OK.replace("42", "0"))),
            Some((200, CREATE_OK.replace("42", "42.5"))),
            Some((200, CREATE_OK.replace("42", "9007199254740992"))),
            Some((
                200,
                CREATE_OK.replace("\"vault\":\"\"", "\"vault\":\"other\""),
            )),
            Some((200, CREATE_OK.replace("Alpha", "Other"))),
        ];
        for reply in replies {
            let fixture = CreationFixtureTest::new();
            let (client, requests) =
                fixture.client(vec![Some((200, CREATE_STORAGE.into())), reply]);
            assert!(matches!(
                client.create_note("Alpha", "", 1),
                Err(CreateError::Uncertain(_))
            ));
            let requests = requests.join().unwrap();
            assert_eq!(requests.len(), 2);
            assert_eq!(
                requests.iter().filter(|r| r.starts_with("POST ")).count(),
                1
            );
        }
    }
    #[test]
    fn ordinary_clients_never_touch_creation_journal() {
        let fixture = CreationFixtureTest::new();
        let (mut client, requests) = fixture.client(vec![]);
        client.creation_fixture = None;
        assert!(client.creation_snapshot().is_err());
        assert!(client.create_note("Alpha", "", 1).is_err());
        assert_eq!(
            std::fs::read_dir(fixture.root.join("cache"))
                .unwrap()
                .count(),
            0
        );
        assert!(requests.join().unwrap().is_empty());
    }
    #[test]
    fn unsafe_receipt_storage_blocks_before_any_network_request() {
        use std::os::unix::fs::symlink;
        let fixture = CreationFixtureTest::new();
        let target = fixture.root.join("outside");
        std::fs::write(&target, "untouched").unwrap();
        symlink(
            &target,
            fixture.root.join("cache/track-native-create-v1.json"),
        )
        .unwrap();
        let (client, requests) = fixture.client(vec![]);
        assert!(matches!(
            client.create_note("Alpha", "", 1),
            Err(CreateError::NotSent(_))
        ));
        assert!(requests.join().unwrap().is_empty());
        assert_eq!(std::fs::read_to_string(target).unwrap(), "untouched");
    }
    #[test]
    fn lost_response_retained_exit_and_restart_never_authorize_retry() {
        let fixture = CreationFixtureTest::new();
        let (client, requests) = fixture.client(vec![Some((200, CREATE_STORAGE.into())), None]);
        assert!(matches!(
            client.create_note("Alpha", "", 1),
            Err(CreateError::Uncertain(_))
        ));
        let exact = client.creation_snapshot().unwrap().unwrap();
        assert_eq!(
            exact.outcome,
            crate::session_store::CreationOutcome::Pending
        );
        client.retain_uncertain_creation(&exact).unwrap();
        assert!(matches!(
            client.clone().create_note("Alpha", "", 1),
            Err(CreateError::NotSent(_))
        ));
        assert_eq!(
            requests
                .join()
                .unwrap()
                .iter()
                .filter(|r| r.starts_with("POST "))
                .count(),
            1
        );
        drop(client);
        let (mut restarted, requests) = fixture.client(vec![]);
        restarted.launch_generation = 99;
        assert_eq!(restarted.creation_snapshot().unwrap(), Some(exact.clone()));
        restarted.retain_uncertain_creation(&exact).unwrap();
        assert!(matches!(
            restarted.create_note("Other", "", 99),
            Err(CreateError::NotSent(_))
        ));
        assert!(requests.join().unwrap().is_empty());
    }
    #[test]
    fn receipt_or_lock_changed_during_readiness_prevents_post() {
        use std::os::unix::fs::PermissionsExt;
        for name in ["track-native-create-v1.json", "track-native-create-v1.lock"] {
            let fixture = CreationFixtureTest::new();
            let (received, waiting) = std::sync::mpsc::channel();
            let (release, resume) = std::sync::mpsc::channel();
            let (mut client, requests) = gated_request_mock(
                vec![Some((200, CREATE_STORAGE.into()))],
                Some((received, resume)),
                "GET ",
            );
            client.creation_fixture = Some(fixture.binding.clone());
            let worker = thread::spawn(move || client.create_note("Alpha", "", 1));
            waiting.recv_timeout(Duration::from_secs(3)).unwrap();
            let path = fixture.root.join("cache").join(name);
            let bytes = std::fs::read(&path).unwrap();
            std::fs::rename(&path, path.with_extension("retained")).unwrap();
            std::fs::write(&path, bytes).unwrap();
            std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600)).unwrap();
            release.send(()).unwrap();
            assert!(matches!(
                worker.join().unwrap(),
                Err(CreateError::NotSent(_))
            ));
            let requests = requests.join().unwrap();
            assert_eq!(requests.len(), 1);
            assert!(!requests.iter().any(|r| r.starts_with("POST ")));
        }
    }
    #[test]
    fn cloned_clients_serialize_pending_mutation_and_durable_intent_precedes_post() {
        let fixture = CreationFixtureTest::new();
        let (received, waiting) = std::sync::mpsc::channel();
        let (release, resume) = std::sync::mpsc::channel();
        let (mut client, requests) = gated_request_mock(
            vec![Some((200, CREATE_STORAGE.into())), None],
            Some((received, resume)),
            "POST ",
        );
        client.creation_fixture = Some(fixture.binding.clone());
        let first = client.clone();
        let worker = thread::spawn(move || first.create_note("Alpha", "", 1));
        waiting.recv_timeout(Duration::from_secs(3)).unwrap();
        let bytes = std::fs::read(fixture.root.join("cache/track-native-create-v1.json")).unwrap();
        let record: crate::session_store::CreationRecord = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(
            record.outcome,
            crate::session_store::CreationOutcome::Pending
        );
        let (completed, completion) = std::sync::mpsc::channel();
        let second = thread::spawn(move || {
            completed.send(client.create_note("Beta", "", 1)).unwrap();
        });
        assert!(completion.recv_timeout(Duration::from_millis(20)).is_err());
        release.send(()).unwrap();
        assert!(matches!(
            worker.join().unwrap(),
            Err(CreateError::Uncertain(_))
        ));
        assert!(matches!(
            completion.recv_timeout(Duration::from_secs(3)).unwrap(),
            Err(CreateError::NotSent(_))
        ));
        second.join().unwrap();
        assert_eq!(
            requests
                .join()
                .unwrap()
                .iter()
                .filter(|r| r.starts_with("POST "))
                .count(),
            1
        );
    }
    #[test]
    fn create_conflict_is_not_success_and_does_not_overwrite() {
        let fixture = CreationFixtureTest::new();
        let original = fixture.root.join("vault/original.md");
        std::fs::write(&original, "unchanged\n").unwrap();
        let (client, requests) = fixture.client(vec![
            Some((200, CREATE_STORAGE.into())),
            Some((409, "conflict".into())),
        ]);
        assert_eq!(
            client.create_note("Alpha", "", 1),
            Err(CreateError::Rejected { status: 409 })
        );
        let requests = requests.join().unwrap();
        assert_eq!(
            requests.iter().filter(|r| r.starts_with("POST ")).count(),
            1
        );
        assert!(!requests.iter().any(|r| r.starts_with("PUT ")));
        assert_eq!(std::fs::read_to_string(original).unwrap(), "unchanged\n");
    }
    #[test]
    fn acknowledged_creation_reads_actual_template_with_new_storage_generation() {
        let fixture = CreationFixtureTest::new();
        let path = fixture.root.join("vault/42.md");
        std::fs::write(&path, "Default template\n").unwrap();
        let source = serde_json::json!({"note":{"note_id":42,"vault":"","file_kind":"note","path":path,"title":"Alpha","body":"Default template\n","etag":"created-etag","read_only":false}}).to_string();
        let fresh_storage = CREATE_STORAGE.replace("generation\":2", "generation\":3");
        let (client, requests) = fixture.client(vec![
            Some((200, CREATE_STORAGE.into())),
            Some((200, CREATE_OK.into())),
            Some((200, fresh_storage.clone())),
            Some((200, source)),
            Some((200, fresh_storage)),
        ]);
        let ack = client.create_note("Alpha", "", 1).unwrap();
        let (note, storage) = client.verify_created(&ack).unwrap();
        assert_eq!(note.note.body, "Default template\n");
        assert_eq!(storage.generation, 3);
        let requests = requests.join().unwrap();
        assert_eq!(requests.len(), 5);
        assert_eq!(
            requests.iter().filter(|r| r.starts_with("POST ")).count(),
            1
        );
        assert!(!requests.iter().any(|r| r.starts_with("PUT ")));
    }
    #[test]
    fn created_source_requires_exact_identity_kind_fixture_path_and_stable_fresh_storage() {
        let fixture = CreationFixtureTest::new();
        let path = fixture.root.join("vault/42.md");
        std::fs::write(&path, "Default template\n").unwrap();
        let ack = CreateAck {
            launch_generation: 1,
            vault: String::new(),
            note_id: 42,
            title: "Alpha".into(),
        };
        for field in ["note_id", "vault", "file_kind", "path", "generation"] {
            let mut source = serde_json::json!({"note":{"note_id":42,"vault":"","file_kind":"note","path":path,"title":"Alpha","body":"Default template\n","etag":"created-etag","read_only":false}});
            match field {
                "note_id" => source["note"][field] = 99.into(),
                "vault" => source["note"][field] = "other".into(),
                "file_kind" => source["note"][field] = "journal".into(),
                "path" => {
                    source["note"][field] = fixture
                        .root
                        .join("config")
                        .to_string_lossy()
                        .into_owned()
                        .into()
                }
                _ => (),
            }
            let after = if field == "generation" {
                CREATE_STORAGE.replace("generation\":2", "generation\":3")
            } else {
                CREATE_STORAGE.into()
            };
            let (client, requests) = fixture.client(vec![
                Some((200, CREATE_STORAGE.into())),
                Some((200, source.to_string())),
                Some((200, after)),
            ]);
            assert!(client.verify_created(&ack).is_err());
            assert!(
                requests
                    .join()
                    .unwrap()
                    .iter()
                    .all(|r| r.starts_with("GET "))
            );
        }
    }
    #[test]
    fn acknowledged_creation_get_failure_never_reposts() {
        let fixture = CreationFixtureTest::new();
        let (client, requests) = fixture.client(vec![
            Some((200, CREATE_STORAGE.into())),
            Some((200, CREATE_OK.into())),
            Some((200, CREATE_STORAGE.into())),
            Some((500, "read failed".into())),
        ]);
        let ack = client.create_note("Alpha", "", 1).unwrap();
        assert!(client.verify_created(&ack).is_err());
        assert_eq!(ack.note_id, 42);
        assert_eq!(
            requests
                .join()
                .unwrap()
                .iter()
                .filter(|r| r.starts_with("POST "))
                .count(),
            1
        );
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
        let root = root.canonicalize().unwrap();
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
        authorize_read_fixture(&mut config);
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
            let (graph, fields) = client.full_graph(scope, "main").unwrap();
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
        assert!(client.full_graph("", "main").unwrap().0.nodes.is_empty());
        request.join().unwrap();
        let (client, request) = mock(409, r#"{"error":"invalid link format"}"#);
        let error = client.full_graph("", "main").unwrap_err().to_string();
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
            .local_graph(
                &NoteKey {
                    vault: "work".into(),
                    note_id: 42,
                },
                "main",
            )
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
                .local_graph(
                    &NoteKey {
                        vault: "work".into(),
                        note_id: 42
                    },
                    "main"
                )
                .is_err()
        );
        request.join().unwrap();
    }

    #[test]
    fn empty_engine_note_list_accepts_null_without_hiding_bad_shapes() {
        for wire in [r#"{"notes":null}"#, r#"{"notes":[]}"#] {
            let (client, request) = mock(200, wire);
            assert!(client.notes("").unwrap().notes.is_empty());
            request.join().unwrap();
        }
        let data: Notes =
            serde_json::from_str(r#"{"notes":null,"read_state":{"partial":true}}"#).unwrap();
        assert!(data.notes.is_empty());
        assert_eq!(data.diagnostics["read_state"]["partial"], true);
        for wire in ["{}", r#"{"notes":{}}"#, r#"{"notes":""}"#] {
            assert!(serde_json::from_str::<Notes>(wire).is_err());
        }
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
    #[test]
    fn follow_is_get_only_bounded_and_sanitizes_error_bodies() {
        let (client, worker) = mock(200, r#"{"active":false}"#);
        assert_eq!(
            client.follow().unwrap(),
            crate::editor_follow::FollowResponse::Inactive
        );
        assert!(
            worker
                .join()
                .unwrap()
                .starts_with("GET /api/follow HTTP/1.1\r\n")
        );
        for (status, body) in [
            (200, "x".repeat(crate::editor_follow::WIRE_LIMIT + 1)),
            (302, "/private/never-report".into()),
            (500, "/private/never-report".into()),
        ] {
            let (client, worker) = mock(status, body);
            let err = client.follow().unwrap_err();
            assert!(!format!("{err}").contains("private"));
            assert!(
                worker
                    .join()
                    .unwrap()
                    .starts_with("GET /api/follow HTTP/1.1\r\n")
            );
        }
    }
    #[test]
    fn recently_created_is_bounded_scoped_and_preserves_order() {
        for (scope, wire, accepted) in [
            ("foreign", "foreign", true),
            ("foreign", "", false),
            ("host", "", true),
            ("", "", true),
        ] {
            let row = |id| serde_json::json!({"note_id":id,"file_kind":"note","title":"","path":"note.md","vault":wire});
            let (client, worker) = mock(
                200,
                serde_json::json!({"notes":[row(3),row(1)]}).to_string(),
            );
            let result = client.recently_created(scope, "host");
            let request = worker.join().unwrap();
            assert!(request.contains("sort=created"));
            assert!(request.contains("limit=100"));
            assert_eq!(result.is_ok(), accepted);
            if let Ok(result) = result {
                assert_eq!(
                    result.notes.iter().map(|n| n.note_id).collect::<Vec<_>>(),
                    vec![3, 1]
                );
            }
        }
    }
    #[test]
    fn recently_created_rejects_duplicates_and_journals_but_preserves_partial_empty() {
        for rows in [
            serde_json::json!([{"note_id":1,"file_kind":"journal","title":"x","path":"j.md","vault":"host"}]),
            serde_json::json!([{"note_id":1,"file_kind":"note","title":"x","path":"n.md","vault":"host"},{"note_id":1,"file_kind":"note","title":"x","path":"n.md","vault":"host"}]),
        ] {
            let mut data: Notes =
                serde_json::from_value(serde_json::json!({"notes":rows})).unwrap();
            assert!(validate_recently_created(&mut data, "host", "host").is_err());
        }
        let mut data: Notes =
            serde_json::from_value(serde_json::json!({"notes":null,"read_state":{"partial":true}}))
                .unwrap();
        assert!(validate_recently_created(&mut data, "host", "host").is_ok());
        assert!(data.notes.is_empty());
        assert_eq!(data.diagnostics["read_state"]["partial"], true);
    }
    pub(super) fn mock(
        status: u16,
        body: impl Into<String>,
    ) -> (ReadClient, thread::JoinHandle<String>) {
        let body = body.into();
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
            // Consume POST payloads too, so closing the fixture socket cannot reset
            // a render response with unread request data.
            let headers = String::from_utf8_lossy(&request);
            if let Some(len) = headers.lines().find_map(|line| {
                line.to_ascii_lowercase()
                    .strip_prefix("content-length: ")
                    .and_then(|n| n.parse::<usize>().ok())
            }) {
                let mut payload = vec![0; len];
                stream.read_exact(&mut payload).unwrap();
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
                canonical_read_only: false,
                creation_store: Default::default(),
                creation_fixture: None,
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
    const INLINE_SPEC: &str = r#"{"version":2,"mark":"line","data":{"kind":"metric","records":[{"version":1,"name":"score","time":"A","value":0}]},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}"#;
    fn viewspec_mock(
        status: u16,
        content_type: &str,
        response: String,
    ) -> (ReadClient, thread::JoinHandle<(String, Vec<u8>)>) {
        use std::io::Write;
        let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let content_type = content_type.to_owned();
        let worker = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream
                .set_read_timeout(Some(Duration::from_secs(2)))
                .unwrap();
            let mut headers = Vec::new();
            let mut byte = [0];
            while !headers.ends_with(b"\r\n\r\n") {
                stream.read_exact(&mut byte).unwrap();
                headers.push(byte[0]);
            }
            let headers = String::from_utf8(headers).unwrap();
            let len = headers
                .lines()
                .find_map(|line| {
                    line.to_ascii_lowercase()
                        .strip_prefix("content-length: ")
                        .and_then(|s| s.parse().ok())
                })
                .unwrap();
            let mut body = vec![0; len];
            stream.read_exact(&mut body).unwrap();
            let _ = write!(
                stream,
                "HTTP/1.1 {status} Test\r\nContent-Type: {content_type}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{response}",
                response.len()
            );
            (headers, body)
        });
        (
            ReadClient {
                canonical_read_only: false,
                creation_store: Default::default(),
                creation_fixture: None,
                http: reqwest::blocking::Client::builder()
                    .no_proxy()
                    .retry(reqwest::retry::never())
                    .redirect(reqwest::redirect::Policy::none())
                    .build()
                    .unwrap(),
                origin,
                alive: Arc::new(AtomicBool::new(true)),
                launch_generation: 1,
            },
            worker,
        )
    }
    #[test]
    fn inline_viewspec_transport_exact_string_and_explicit_scope() {
        let response = r#"{"echarts":{"xAxis":{"type":"category","data":["A","B","C"]},"yAxis":{"type":"value"},"series":[{"type":"line","data":[0,null,4]}]}}"#;
        for vault in ["", "work & 日本語"] {
            let (client, worker) =
                viewspec_mock(200, "application/json; charset=utf-8", response.into());
            let spec = format!(" \n{INLINE_SPEC}\n");
            let chart = client.inline_viewspec(vault, &spec).unwrap();
            assert_eq!(chart.series[0].values, vec![Some(0.), None, Some(4.)]);
            let (headers, bytes) = worker.join().unwrap();
            assert!(headers.starts_with(if vault.is_empty() {
                "POST /api/viewspec?vault= HTTP/"
            } else {
                "POST /api/viewspec?vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E HTTP/"
            }));
            assert!(
                headers
                    .to_ascii_lowercase()
                    .contains("content-type: application/json")
            );
            assert_eq!(
                serde_json::from_slice::<Value>(&bytes).unwrap(),
                serde_json::json!({"spec":spec})
            );
        }
    }
    #[test]
    fn inline_viewspec_transport_rejects_without_payload_leakage() {
        for (status, mime, body) in [
            (302, "application/json", "/private/secret".into()),
            (500, "application/json", "/private/secret".into()),
            (200, "text/html", "/private/secret".into()),
            (200, "application/json", "not json /private/secret".into()),
            (200, "application/json", r#"{"echarts":[]}"#.into()),
            (
                200,
                "application/json",
                r#"{"echarts":{"/private/secret":true}}"#.into(),
            ),
            (200, "application/json", "x".repeat(256 * 1024 + 1)),
        ] {
            let (client, worker) = viewspec_mock(status, mime, body);
            let error = client.inline_viewspec("scope", INLINE_SPEC).unwrap_err();
            assert!(!error.contains("private"));
            assert!(error.len() < 128);
            worker.join().unwrap();
        }
    }
    #[test]
    fn inline_viewspec_invalid_admission_performs_zero_io() {
        let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
        listener.set_nonblocking(true).unwrap();
        let client = ReadClient {
            canonical_read_only: false,
            creation_store: Default::default(),
            creation_fixture: None,
            http: reqwest::blocking::Client::builder()
                .no_proxy()
                .build()
                .unwrap(),
            origin: format!("http://{}", listener.local_addr().unwrap()),
            alive: Arc::new(AtomicBool::new(true)),
            launch_generation: 1,
        };
        for spec in [
            INLINE_SPEC.replace(
                "\"kind\":\"metric\"",
                "\"kind\":\"metric\",\"source\":\"../private\"",
            ),
            INLINE_SPEC.replace("\"version\":2", "\"version\":2,\"version\":2"),
            "{}".into(),
        ] {
            assert!(client.inline_viewspec("scope", &spec).is_err());
            assert_eq!(
                listener.accept().unwrap_err().kind(),
                std::io::ErrorKind::WouldBlock
            );
        }
        client.alive.store(false, Ordering::Release);
        assert!(client.inline_viewspec("scope", INLINE_SPEC).is_err());
        assert_eq!(
            listener.accept().unwrap_err().kind(),
            std::io::ErrorKind::WouldBlock
        );
    }
    #[test]
    fn includes_transport_is_render_only_and_scoped() {
        use std::io::Write;
        for vault in ["", "work & 日本語"] {
            let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
            let (mut client, unused) = mock(200, "{}");
            // Release the helper server without using its response for this request.
            client.vaults().unwrap_err();
            unused.join().unwrap();
            client.origin = format!("http://{}", listener.local_addr().unwrap());
            let worker = thread::spawn(move || {
                let (mut stream, _) = listener.accept().unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(2)))
                    .unwrap();
                let mut headers = Vec::new();
                let mut byte = [0u8; 1];
                while !headers.ends_with(b"\r\n\r\n") {
                    stream.read_exact(&mut byte).unwrap();
                    headers.push(byte[0]);
                }
                let headers = String::from_utf8(headers).unwrap();
                let len = headers
                    .lines()
                    .find_map(|l| {
                        l.to_ascii_lowercase()
                            .strip_prefix("content-length: ")
                            .and_then(|v| v.parse::<usize>().ok())
                    })
                    .unwrap();
                let mut body = vec![0; len];
                stream.read_exact(&mut body).unwrap();
                let response = r#"{"markdown":"ignored","includes":[]}"#;
                write!(
                    stream,
                    "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{response}",
                    response.len()
                )
                .unwrap();
                (headers, body)
            });
            let body = "# Host\r\n![[Other]]\n";
            assert!(client.included_notes(vault, body).unwrap().is_empty());
            let (headers, bytes) = worker.join().unwrap();
            assert!(headers.starts_with(if vault.is_empty() {
                "POST /api/render HTTP/"
            } else {
                "POST /api/render?vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E HTTP/"
            }));
            assert!(
                headers
                    .to_ascii_lowercase()
                    .contains("content-type: application/json")
            );
            assert_eq!(
                serde_json::from_slice::<Value>(&bytes).unwrap(),
                serde_json::json!({"body":body})
            );
        }
    }
    #[test]
    fn includes_transport_fails_closed_on_http_json_size_and_session() {
        for (status, body) in [
            (302, r#"{"markdown":"","includes":[]}"#.to_owned()),
            (500, "failure".into()),
            (200, "not json".into()),
            (200, "x".repeat(BODY_LIMIT as usize + 1)),
        ] {
            let (client, worker) = mock(status, body);
            assert!(client.included_notes("work", "host").is_err());
            worker.join().unwrap();
        }
        let (client, worker) = mock(200, r#"{"markdown":"","includes":[]}"#);
        assert!(client.included_notes("", "host").is_ok());
        worker.join().unwrap();
        client.alive.store(false, Ordering::Release);
        assert!(matches!(
            client.included_notes("", "host"),
            Err(Error::Stale)
        ));
    }
    #[test]
    fn pdf_transport_is_local_bounded_and_source_qualified() {
        use std::io::Write;
        for (status, mime, length, body, ok) in [
            (200, "application/pdf", 8, b"%PDF-1.7".as_slice(), true),
            (302, "application/pdf", 8, b"%PDF-1.7".as_slice(), false),
            (200, "text/plain", 8, b"%PDF-1.7".as_slice(), false),
            (200, "application/pdf", 8, b"not PDF!".as_slice(), false),
            (
                200,
                "application/pdf",
                crate::pdf_probe::MAX_INPUT + 1,
                b"".as_slice(),
                false,
            ),
        ] {
            let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
            let origin = format!("http://{}", listener.local_addr().unwrap());
            let worker = thread::spawn(move || {
                let (mut stream, _) = listener.accept().unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(2)))
                    .unwrap();
                let mut request = Vec::new();
                while !request.ends_with(b"\r\n\r\n") {
                    let mut b = [0];
                    stream.read_exact(&mut b).unwrap();
                    request.push(b[0]);
                }
                write!(stream,"HTTP/1.1 {status} Test\r\nContent-Type: {mime}\r\nLocation: http://127.0.0.1:1/forbidden\r\nContent-Length: {length}\r\nConnection: close\r\n\r\n").unwrap();
                let _ = stream.write_all(body);
                String::from_utf8(request).unwrap()
            });
            let client = ReadClient {
                canonical_read_only: false,
                creation_store: Default::default(),
                creation_fixture: None,
                http: reqwest::blocking::Client::builder()
                    .no_proxy()
                    .redirect(reqwest::redirect::Policy::none())
                    .timeout(Duration::from_secs(2))
                    .build()
                    .unwrap(),
                origin,
                alive: Arc::new(AtomicBool::new(true)),
                launch_generation: 1,
            };

            assert!(
                client
                    .pdf_asset("https://example.com/a.pdf", "work")
                    .is_err()
            );
            assert_eq!(client.pdf_asset("assets/a.pdf", "").is_ok(), ok);
            let request = worker.join().unwrap();
            assert!(request.contains("GET /api/asset?"));
            assert!(request.contains("vault=&"));
            assert!(request.contains("name=a.pdf"));
        }
    }
    #[test]
    fn raster_transport_scopes_bytes_and_never_follows_redirects() {
        use std::io::Write;
        for (status, thumbnail) in [
            (200, false),
            (302, false),
            (404, false),
            (200, true),
            (302, true),
            (404, true),
        ] {
            let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
            let origin = format!("http://{}", listener.local_addr().unwrap());
            let bytes = crate::raster::tests::png(1, 1);
            let worker = thread::spawn(move || {
                let (mut stream, _) = listener.accept().unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(2)))
                    .unwrap();
                let mut request = Vec::new();
                while !request.ends_with(b"\r\n\r\n") {
                    let mut b = [0];
                    stream.read_exact(&mut b).unwrap();
                    request.push(b[0]);
                }
                write!(stream, "HTTP/1.1 {status} Test\r\nContent-Type: image/png\r\nLocation: http://127.0.0.1:1/forbidden\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", bytes.len()).unwrap();
                stream.write_all(&bytes).unwrap();
                String::from_utf8(request).unwrap()
            });
            let client = ReadClient {
                canonical_read_only: false,
                creation_store: Default::default(),
                creation_fixture: None,
                http: reqwest::blocking::Client::builder()
                    .no_proxy()
                    .redirect(reqwest::redirect::Policy::none())
                    .timeout(Duration::from_secs(2))
                    .build()
                    .unwrap(),
                origin,
                alive: Arc::new(AtomicBool::new(true)),
                launch_generation: 1,
            };
            // Rejections occur before issuing any request.
            assert!(matches!(
                client.raster_asset("assets/../x.png", "work"),
                Err(Error::Config(_))
            ));
            client.alive.store(false, Ordering::Release);
            assert!(matches!(
                client.raster_asset("assets/a.png", "work"),
                Err(Error::Stale)
            ));
            client.alive.store(true, Ordering::Release);
            let result = if thumbnail {
                client.raster_thumbnail("assets/図/a & b.png", "work & 日本語")
            } else {
                client.raster_asset("assets/図/a & b.png", "work & 日本語")
            };
            if status == 200 {
                assert_eq!(result.unwrap().width, 1);
            } else {
                assert!(matches!(result, Err(Error::Http {status: actual,..}) if actual == status));
            }
            let request = worker.join().unwrap();
            assert!(request.starts_with("GET /api/asset?"));
            assert!(request.contains("vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E"));
            assert!(request.contains("name=%E5%9B%B3%2Fa+%26+b.png"));
        }
    }

    #[test]
    fn raster_transport_rejects_oversized_headers_and_retired_inflight_session() {
        use std::io::Write;
        for retired in [false, true] {
            let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
            let origin = format!("http://{}", listener.local_addr().unwrap());
            let alive = Arc::new(AtomicBool::new(true));
            let server_alive = alive.clone();
            let worker = thread::spawn(move || {
                let (mut stream, _) = listener.accept().unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(2)))
                    .unwrap();
                let mut request = Vec::new();
                while !request.ends_with(b"\r\n\r\n") {
                    let mut b = [0];
                    stream.read_exact(&mut b).unwrap();
                    request.push(b[0]);
                }
                if retired {
                    let bytes = crate::raster::tests::png(1, 1);
                    server_alive.store(false, Ordering::Release);
                    write!(stream,"HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", bytes.len()).unwrap();
                    stream.write_all(&bytes).unwrap();
                } else {
                    write!(stream,"HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", crate::raster::MAX_ENCODED+1).unwrap();
                }
            });
            let client = ReadClient {
                canonical_read_only: false,
                creation_store: Default::default(),
                creation_fixture: None,
                http: reqwest::blocking::Client::builder()
                    .no_proxy()
                    .redirect(reqwest::redirect::Policy::none())
                    .timeout(Duration::from_secs(2))
                    .build()
                    .unwrap(),
                origin,
                alive,
                launch_generation: 1,
            };
            let result = client.raster_asset("assets/a.png", "");
            if retired {
                assert!(matches!(result, Err(Error::Stale)));
            } else {
                assert!(matches!(result, Err(Error::Oversized)));
            }
            worker.join().unwrap();
        }
    }

    fn hierarchy_node(id: i64, children: Vec<Value>) -> Value {
        serde_json::json!({"note_id":id,"title":format!("Note {id}"),"file_kind":"note","flags":["CONFIDENTIAL"],"children":children})
    }
    #[test]
    fn hierarchy_transport_preserves_order_scope_flags_and_partial_state() {
        let body = serde_json::json!({"vault":"work & 日本語","hierarchy":[hierarchy_node(3,vec![hierarchy_node(9,vec![]),hierarchy_node(1,vec![])]),hierarchy_node(4,vec![])],"read_state":{"partial":true}});
        let (client, worker) = mock(200, body.to_string());
        let r = client.hierarchy("work & 日本語").unwrap();
        assert_eq!(
            r.rows.iter().map(|n| n.note_id).collect::<Vec<_>>(),
            vec![3, 9, 1, 4]
        );
        assert_eq!(
            r.rows.iter().map(|n| n.parent).collect::<Vec<_>>(),
            vec![None, Some(0), Some(0), None]
        );
        assert_eq!(r.rows[1].depth, 1);
        assert!(r.rows[0].has_children);
        assert_eq!(r.rows[0].flags, vec!["CONFIDENTIAL"]);
        assert_eq!(r.source_vault, "work & 日本語");
        assert_eq!(r.launch_generation, client.launch_generation);
        assert_eq!(r.diagnostics["read_state"]["partial"], true);
        assert!(
            worker
                .join()
                .unwrap()
                .starts_with("GET /api/hierarchy?vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E HTTP/")
        );
    }
    #[test]
    fn hierarchy_accepts_launch_alias_null_children_and_engine_empty_forest() {
        for roots in [
            vec![],
            vec![
                serde_json::json!({"note_id":1,"title":"root","file_kind":"note","flags":null,"children":null}),
            ],
        ] {
            let (client, worker) = mock(
                200,
                serde_json::json!({"vault":"main","hierarchy":roots}).to_string(),
            );
            let r = client.hierarchy("").unwrap();
            assert_eq!(r.vault, "main");
            assert_eq!(r.source_vault, "");
            assert!(
                worker
                    .join()
                    .unwrap()
                    .starts_with("GET /api/hierarchy HTTP/")
            );
        }
    }
    #[test]
    fn hierarchy_rejects_repeated_identity_cycles_depth_and_node_limits() {
        let mut chain = hierarchy_node(33, vec![]);
        for id in (1..33).rev() {
            chain = hierarchy_node(id, vec![chain]);
        }
        for roots in [
            vec![hierarchy_node(1, vec![hierarchy_node(1, vec![])])],
            vec![hierarchy_node(1, vec![]), hierarchy_node(1, vec![])],
            vec![chain],
            (1..=4097).map(|id| hierarchy_node(id, vec![])).collect(),
        ] {
            let (client, worker) = mock(
                200,
                serde_json::json!({"vault":"main","hierarchy":roots}).to_string(),
            );
            assert!(matches!(client.hierarchy("main"), Err(Error::Shape(_))));
            worker.join().unwrap();
        }
        let mut chain = hierarchy_node(32, vec![]);
        for id in (1..32).rev() {
            chain = hierarchy_node(id, vec![chain]);
        }
        let wire: HierarchyNode = serde_json::from_value(chain).unwrap();
        assert_eq!(flatten_hierarchy(vec![wire], "").unwrap().len(), 32);
    }
    #[test]
    fn hierarchy_rejects_foreign_scope_bad_metadata_http_and_stale_session() {
        for (field, value) in [
            ("note_id", serde_json::json!(0)),
            ("vault", serde_json::json!("foreign")),
            ("file_kind", serde_json::json!("")),
            ("file_kind", serde_json::json!("template")),
            ("flags", serde_json::json!([true])),
            ("title", serde_json::json!("x".repeat(65537))),
        ] {
            let mut n = hierarchy_node(1, vec![]);
            n[field] = value;
            let (client, worker) = mock(
                200,
                serde_json::json!({"vault":"work","hierarchy":[n]}).to_string(),
            );
            assert!(client.hierarchy("work").is_err(), "{field}");
            worker.join().unwrap();
        }
        let (client, worker) = mock(200, r#"{"vault":"foreign","hierarchy":[]}"#);
        assert!(matches!(client.hierarchy("work"), Err(Error::Shape(_))));
        worker.join().unwrap();
        let (client, worker) = mock(500, "{}");
        assert!(matches!(
            client.hierarchy("bad\0scope"),
            Err(Error::Config(_))
        ));
        assert!(matches!(
            client.hierarchy("work"),
            Err(Error::Http { status: 500, .. })
        ));
        worker.join().unwrap();
        client.alive.store(false, Ordering::Release);
        assert!(matches!(client.hierarchy("work"), Err(Error::Stale)));
    }
    fn calendar_event_fixture() -> Value {
        serde_json::json!({"version":1,"id":"20000000-0000-4000-8000-000000000001","revision":1,
            "title":"会議","all_day":true,"start":"2026-11-13","end":"2026-11-15",
            "time_zone":"","description":"notes\nline","location":"Room","url":"","note_id":"",
            "status":"confirmed","deleted":false,"updated_at":"2026-11-13T01:00:00Z"})
    }
    #[test]
    fn calendar_events_scoped_read_and_exclusive_civil_ranges() {
        let mut cancelled = calendar_event_fixture();
        cancelled["status"] = serde_json::json!("cancelled");
        let (client, worker) = mock(200,serde_json::json!({"vault":"work & 日本語","events":[cancelled],"read_state":{"partial":true}}).to_string());
        let response = client.calendar_events("work & 日本語").unwrap();
        assert_eq!(response.source_vault, "work & 日本語");
        assert_eq!(response.launch_generation, client.launch_generation);
        assert_eq!(response.diagnostics["read_state"]["partial"], true);
        let e = &response.events[0];
        assert_eq!(e.status, "cancelled");
        for date in ["2026-11-13", "2026-11-14"] {
            assert!(e.all_day_on(date));
        }
        for date in ["2026-11-12", "2026-11-15", "2026-02-30", ""] {
            assert!(!e.all_day_on(date));
        }
        let request = worker.join().unwrap();
        assert!(request.starts_with(
            "GET /api/calendar-events?vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E HTTP/"
        ));
        assert!(!request.contains("deleted="));
    }
    #[test]
    fn calendar_events_preserve_opaque_timed_values_without_day_assignment() {
        let mut e = calendar_event_fixture();
        e["all_day"] = serde_json::json!(false);
        e["start"] = serde_json::json!("2026-11-01T01:30:00.123456789-04:00");
        e["end"] = serde_json::json!("2026-11-01T01:30:00.123456789-05:00");
        e["time_zone"] = serde_json::json!("America/New_York");
        let (client, worker) = mock(
            200,
            serde_json::json!({"vault":"main","events":[e]}).to_string(),
        );
        let response = client.calendar_events("").unwrap();
        assert_eq!(response.source_vault, "");
        assert_eq!(response.vault, "main");
        assert_eq!(response.events.iter().filter(|e| !e.all_day).count(), 1);
        assert!(!response.events[0].all_day_on("2026-11-01"));
        assert!(
            worker
                .join()
                .unwrap()
                .starts_with("GET /api/calendar-events HTTP/")
        );
    }
    #[test]
    fn calendar_events_reject_bad_contract_and_bounds() {
        for (field, value) in [
            ("version", serde_json::json!(2)),
            ("revision", serde_json::json!(0)),
            ("id", serde_json::json!("bad")),
            ("deleted", serde_json::json!(true)),
            ("start", serde_json::json!("2026-02-30")),
            ("start", serde_json::json!("0000-01-01")),
            ("end", serde_json::json!("2026-11-13")),
            ("time_zone", serde_json::json!("UTC")),
            ("note_id", serde_json::json!("001")),
            ("title", serde_json::json!("x".repeat(501))),
            ("status", serde_json::json!("unknown")),
        ] {
            let mut e = calendar_event_fixture();
            e[field] = value;
            let (client, worker) = mock(
                200,
                serde_json::json!({"vault":"work","events":[e]}).to_string(),
            );
            assert!(
                matches!(client.calendar_events("work"), Err(Error::Shape(_))),
                "{field}"
            );
            worker.join().unwrap();
        }
        for body in [
            serde_json::json!({"vault":"other","events":[]}),
            serde_json::json!({"vault":"work","events":[calendar_event_fixture(),calendar_event_fixture()]}),
            serde_json::json!({"vault":"work","events":vec![calendar_event_fixture();4097]}),
        ] {
            let (client, worker) = mock(200, body.to_string());
            assert!(matches!(
                client.calendar_events("work"),
                Err(Error::Shape(_))
            ));
            worker.join().unwrap();
        }
    }
    #[test]
    fn calendar_events_fail_closed_on_status_config_and_stale_session() {
        for status in [404, 500] {
            let (client, worker) = mock(status, "{}");
            assert!(matches!(
                client.calendar_events("bad\0vault"),
                Err(Error::Config(_))
            ));
            assert!(
                matches!(client.calendar_events("work"),Err(Error::Http{status:s,..}) if s==status)
            );
            worker.join().unwrap();
        }
        let (client, worker) = mock(200, r#"{"vault":"","events":[]}"#);
        assert!(client.calendar_events("").unwrap().events.is_empty());
        worker.join().unwrap();
        client
            .alive
            .store(false, std::sync::atomic::Ordering::Release);
        assert!(matches!(client.calendar_events(""), Err(Error::Stale)));
    }
    #[test]
    fn journal_lookup_is_exact_read_only_and_distinguishes_absent_and_unavailable() {
        for scope in ["", "host", "foreign"] {
            let wire = if scope == "foreign" { "foreign" } else { "" };
            let body = serde_json::json!({"vault":wire,"title":"20261117","found":true,"note":{"note_id":42,"file_kind":"journal","title":"20261117"}});
            let (client, worker) = mock(200, body.to_string());
            let response = client.journal_lookup("20261117", scope, "host").unwrap();
            assert_eq!(response.source_vault, scope);
            assert_eq!(response.note.unwrap().note_id, 42);
            let request = worker.join().unwrap();
            assert!(request.starts_with("GET /api/journal/lookup?"));
            assert!(!request.contains("POST"));
        }
        let (client, worker) = mock(
            200,
            r#"{"vault":"","title":"202611","found":false,"note":null}"#,
        );
        assert!(!client.journal_lookup("202611", "", "host").unwrap().found);
        worker.join().unwrap();
        for status in [409, 503, 500] {
            let (client, worker) = mock(status, "{}");
            assert!(
                matches!(client.journal_lookup("202611","","host"),Err(Error::Http{status:s,..}) if s==status)
            );
            worker.join().unwrap();
        }
    }
    #[test]
    fn journal_lookup_rejects_note_shadow_wrong_scope_title_and_inconsistent_found() {
        let original = serde_json::json!({"vault":"foreign","title":"20261117","found":true,"note":{"note_id":42,"file_kind":"journal","title":"20261117"}});
        let mut cases = vec![];
        for (field, value) in [("file_kind", "note"), ("title", "20261118")] {
            let mut bad = original.clone();
            bad["note"][field] = value.into();
            cases.push(bad);
        }
        let mut bad = original.clone();
        bad["vault"] = "".into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["title"] = "20261118".into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["found"] = false.into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["note"] = Value::Null;
        cases.push(bad);
        for body in cases {
            let (client, worker) = mock(200, body.to_string());
            assert!(matches!(
                client.journal_lookup("20261117", "foreign", "host"),
                Err(Error::Shape(_))
            ));
            worker.join().unwrap();
        }
        for invalid in [
            "20260230",
            "202613",
            "000011",
            "999901",
            "99990101",
            "2026-11",
            "２０２６１１",
            "20261117?create=1",
        ] {
            assert!(!journal_title_valid(invalid));
        }
        assert!(journal_title_valid("20240229"));
        assert!(journal_title_valid("202611"));
        assert!(journal_title_valid("000101"));
        assert!(journal_title_valid("99981231"));
    }
    #[test]
    fn journal_final_body_guard_rejects_post_lookup_rename_kind_or_vault_change() {
        let expected = JournalExpectation {
            key: NoteKey {
                vault: "foreign".into(),
                note_id: 42,
            },
            title: "20261117".into(),
            launch_vault: "host".into(),
        };
        let payload = serde_json::json!({"note":{"note_id":42,"vault":"foreign","title":"20261117","file_kind":"journal","path":"display","body":"must not leak on mismatch","etag":"v2","read_only":true}});
        assert!(
            expected
                .verify(serde_json::from_value(payload.clone()).unwrap())
                .is_ok()
        );
        for (field, value) in [
            ("title", "20261118"),
            ("file_kind", "note"),
            ("vault", "host"),
        ] {
            let mut changed = payload.clone();
            changed["note"][field] = value.into();
            assert!(
                expected
                    .verify(serde_json::from_value(changed).unwrap())
                    .is_err()
            );
        }
        let mut changed = payload.clone();
        changed["note"]["note_id"] = 43.into();
        assert!(
            expected
                .verify(serde_json::from_value(changed).unwrap())
                .is_err()
        );
    }
    fn content_fixture() -> Value {
        serde_json::json!({"vault":"work", "since":"2026-11-01", "until":"2026-11-01", "time_zone":"UTC", "today":"2026-11-17",
            "days":[{"date":"2026-11-01", "task_count":1, "tasks":[{"note_id":2,"file_kind":"note","line":1,"text":"Complete", "text_truncated":false,
                "state":"DONE","done":true,"priority":"A","scheduled":"2026-11-01","due":"2026-11-01"}],
                "note_count":1,"notes":[{"note_id":9,"file_kind":"note","title":"Title", "title_truncated":false}]}]})
    }
    #[test]
    fn month_content_adapter_preserves_engine_order_completed_and_scope() {
        let mut payload = content_fixture();
        let mut second = payload["days"][0]["notes"][0].clone();
        second["note_id"] = 1.into();
        payload["days"][0]["notes"]
            .as_array_mut()
            .unwrap()
            .push(second);
        payload["days"][0]["note_count"] = 2.into();
        let (client, worker) = mock(200, payload.to_string());
        let r = client
            .calendar_content("work", "host", "2026-11-01", "2026-11-01", "UTC")
            .unwrap();
        assert_eq!(r.source_vault, "work");
        assert_eq!(r.launch_generation, client.launch_generation);
        assert_eq!(
            r.days[0]
                .notes
                .iter()
                .map(|n| n.note_id)
                .collect::<Vec<_>>(),
            vec![9, 1]
        );
        assert!(r.days[0].tasks[0].done);
        assert_eq!(r.today, "2026-11-17");
        let req = worker.join().unwrap();
        assert!(req.contains("/api/calendar-activity/range?"));
        assert!(req.contains("vault=work"));
    }
    #[test]
    fn month_content_rejects_unknown_coverage_counts_labels_and_membership() {
        let original = content_fixture();
        let mut cases = vec![];
        for (key, value) in [
            ("vault", "foreign"),
            ("since", "2026-10-31"),
            ("time_zone", "Asia/Tokyo"),
            ("today", "2026-02-30"),
        ] {
            let mut bad = original.clone();
            bad[key] = value.into();
            cases.push(bad);
        }
        let mut bad = original.clone();
        bad["days"] = serde_json::json!([]);
        cases.push(bad);
        for field in ["task_count", "note_count"] {
            let mut bad = original.clone();
            bad["days"][0][field] = 16385.into();
            cases.push(bad);
        }
        let mut bad = original.clone();
        bad["days"][0]["tasks"][0]["text"] = "x".repeat(257).into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][0]["tasks"][0]["text_truncated"] = true.into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][0]["tasks"][0]["scheduled"] = "2026-11-02".into();
        bad["days"][0]["tasks"][0]["due"] = "".into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][0]["tasks"][0]["done"] = false.into();
        cases.push(bad);
        let mut bad = original.clone();
        let duplicate = bad["days"][0]["tasks"][0].clone();
        bad["days"][0]["tasks"]
            .as_array_mut()
            .unwrap()
            .push(duplicate);
        bad["days"][0]["task_count"] = 2.into();
        cases.push(bad);
        let mut bad = original.clone();
        let duplicate = bad["days"][0]["notes"][0].clone();
        bad["days"][0]["notes"]
            .as_array_mut()
            .unwrap()
            .push(duplicate);
        bad["days"][0]["note_count"] = 2.into();
        cases.push(bad);
        for bad in cases {
            let r: CalendarContentResponse = serde_json::from_value(bad).unwrap();
            assert!(
                validate_calendar_content(&r, "work", "host", "2026-11-01", "2026-11-01", "UTC")
                    .is_err()
            );
        }
        let mut good = original.clone();
        good["days"][0]["tasks"][0]["text"] = "x…".into();
        good["days"][0]["tasks"][0]["text_truncated"] = true.into();
        let r = serde_json::from_value(good).unwrap();
        assert!(
            validate_calendar_content(&r, "work", "host", "2026-11-01", "2026-11-01", "UTC")
                .is_ok()
        );
    }
    #[test]
    fn month_content_preserves_impossible_optional_date_without_losing_valid_bucket() {
        let mut payload = content_fixture();
        payload["since"] = "2024-02-29".into();
        payload["until"] = "2024-02-29".into();
        payload["days"][0]["date"] = "2024-02-29".into();
        payload["days"][0]["tasks"][0]["scheduled"] = "2024-02-29".into();
        payload["days"][0]["tasks"][0]["due"] = "2024-02-30".into();
        let mut other = payload["days"][0]["tasks"][0].clone();
        other["line"] = 2.into();
        other["due"] = "2024-03-01".into();
        payload["days"][0]["tasks"]
            .as_array_mut()
            .unwrap()
            .push(other);
        payload["days"][0]["task_count"] = 2.into();
        let (client, worker) = mock(200, payload.to_string());
        let result = client
            .calendar_content("work", "host", "2024-02-29", "2024-02-29", "UTC")
            .unwrap();
        worker.join().unwrap();
        assert_eq!(result.days[0].task_count, 2);
        assert_eq!(result.days[0].tasks[0].due, "2024-02-30");
        assert_eq!(result.days[0].tasks[1].due, "2024-03-01");
        assert_eq!(result.days[0].notes.len(), 1);
        for invalid in ["2024-2-30", "２０２４-02-30", "2024-02-30\n", "2024/02/30"] {
            let mut bad = payload.clone();
            bad["days"][0]["tasks"][0]["due"] = invalid.into();
            let response = serde_json::from_value(bad).unwrap();
            assert!(
                validate_calendar_content(
                    &response,
                    "work",
                    "host",
                    "2024-02-29",
                    "2024-02-29",
                    "UTC"
                )
                .is_err()
            );
        }
    }
    #[test]
    fn month_content_unready_and_foreign_alias_do_not_become_empty_success() {
        for status in [400, 500, 503] {
            let (client, worker) = mock(status, "{}");
            assert!(
                matches!(client.calendar_content("work","host","2026-11-01","2026-11-01","UTC"),Err(Error::Http{status:s,..}) if s==status)
            );
            worker.join().unwrap();
        }
        for (request, ok) in [("", true), ("host", true), ("foreign", false)] {
            let mut payload = content_fixture();
            payload["vault"] = "".into();
            let (client, worker) = mock(200, payload.to_string());
            assert_eq!(
                client
                    .calendar_content(request, "host", "2026-11-01", "2026-11-01", "UTC")
                    .is_ok(),
                ok
            );
            worker.join().unwrap();
        }
    }
    fn range_fixture() -> Value {
        serde_json::json!({"vault":"work","since":"2026-11-01","until":"2026-11-02","time_zone":"UTC",
            "days":[{"date":"2026-11-01","count":0,"previews":[]},{"date":"2026-11-02","count":1,"previews":[
                {"id":"00000000-0000-4000-8000-000000000001","revision":1,"title":"Meeting","status":"cancelled"}]}]})
    }
    #[test]
    fn range_adapter_accepts_only_verified_launch_alias_for_empty_wire_label() {
        for requested in ["", "host"] {
            let mut payload = range_fixture();
            payload["vault"] = "".into();
            let (client, worker) = mock(200, payload.to_string());
            let result = client
                .calendar_range(requested, "host", "2026-11-01", "2026-11-02", "UTC")
                .unwrap();
            assert_eq!(result.source_vault, requested);
            assert!(result.vault.is_empty());
            worker.join().unwrap();
        }
        for (requested, wire) in [
            ("foreign", ""),
            ("unknown", ""),
            ("", "foreign"),
            ("host", "foreign"),
        ] {
            let mut payload = range_fixture();
            payload["vault"] = wire.into();
            let (client, worker) = mock(200, payload.to_string());
            assert!(matches!(
                client.calendar_range(requested, "host", "2026-11-01", "2026-11-02", "UTC"),
                Err(Error::Shape(_))
            ));
            worker.join().unwrap();
        }
    }
    #[test]
    fn range_adapter_preserves_engine_membership_and_exact_scoped_query() {
        let (client, worker) = mock(200, range_fixture().to_string());
        let result = client
            .calendar_range("work", "host", "2026-11-01", "2026-11-02", "UTC")
            .unwrap();
        assert_eq!(result.days[0].count, 0);
        assert_eq!(result.days[1].previews[0].status, "cancelled");
        assert_eq!(result.source_vault, "work");
        assert_eq!(result.launch_generation, client.launch_generation);
        let request = worker.join().unwrap();
        for part in [
            "/api/calendar-events/range?",
            "vault=work",
            "since=2026-11-01",
            "until=2026-11-02",
            "time_zone=UTC",
        ] {
            assert!(request.contains(part), "{request}");
        }
    }
    #[test]
    fn range_adapter_rejects_incomplete_duplicate_oversized_and_mismatched_wire() {
        let original = range_fixture();
        let mut cases = vec![];
        for (key, value) in [
            ("vault", "foreign"),
            ("time_zone", "Asia/Tokyo"),
            ("since", "2026-10-31"),
        ] {
            let mut bad = original.clone();
            bad[key] = value.into();
            cases.push(bad);
        }
        let mut bad = original.clone();
        bad["days"].as_array_mut().unwrap().pop();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][1]["date"] = "2026-11-01".into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][1]["count"] = 4097.into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][1]["count"] = 0.into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][1]["previews"][0]["title"] = "あ".repeat(167).into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][1]["previews"][0]["revision"] = 0.into();
        cases.push(bad);
        let mut bad = original.clone();
        bad["days"][1]["previews"][0]["status"] = "deleted".into();
        cases.push(bad);
        let mut bad = original.clone();
        let entry = bad["days"][1]["previews"][0].clone();
        bad["days"][1]["count"] = 2.into();
        bad["days"][1]["previews"]
            .as_array_mut()
            .unwrap()
            .push(entry);
        cases.push(bad);
        for bad in cases {
            let response: CalendarRangeResponse = serde_json::from_value(bad).unwrap();
            assert!(
                validate_calendar_range(
                    &response,
                    "work",
                    "host",
                    "2026-11-01",
                    "2026-11-02",
                    "UTC"
                )
                .is_err()
            );
        }
        assert_eq!(range_dates("2024-02-28", "2024-03-01").unwrap().len(), 3);
        assert_eq!(range_dates("2026-01-01", "2026-02-11").unwrap().len(), 42);
        assert!(range_dates("2026-01-01", "2026-02-12").is_none());
        assert!(range_dates("2026-02-02", "2026-02-01").is_none());
    }
    #[test]
    fn calendar_range_year_domain_is_checked_before_transport() {
        for date in ["0001-01-01", "9998-12-31"] {
            assert_eq!(range_dates(date, date).unwrap(), vec![date]);
            for content in [false, true] {
                let day = if content {
                    serde_json::json!({"date":date,"task_count":0,"tasks":[],"note_count":0,"notes":[]})
                } else {
                    serde_json::json!({"date":date,"count":0,"previews":[]})
                };
                let payload = serde_json::json!({"vault":"work","since":date,"until":date,
                    "time_zone":"UTC","today":date,"days":[day]});
                let (client, worker) = mock(200, payload.to_string());
                for (since, until) in [
                    ("0000-12-31", "0001-01-01"),
                    ("9999-01-01", "9999-01-01"),
                    ("9998-12-31", "9999-01-01"),
                    ("9999-12-31", "9999-12-31"),
                ] {
                    assert!(matches!(
                        client.calendar_range("work", "host", since, until, "UTC"),
                        Err(Error::Config(_))
                    ));
                    assert!(matches!(
                        client.calendar_content("work", "host", since, until, "UTC"),
                        Err(Error::Config(_))
                    ));
                }
                // The mock accepts exactly one request: every rejected input above
                // leaves this valid boundary-year request as its first transport.
                if content {
                    assert!(
                        client
                            .calendar_content("work", "host", date, date, "UTC")
                            .is_ok()
                    );
                } else {
                    assert!(
                        client
                            .calendar_range("work", "host", date, date, "UTC")
                            .is_ok()
                    );
                }
                let request = worker.join().unwrap();
                let endpoint = if content {
                    "/api/calendar-activity/range?"
                } else {
                    "/api/calendar-events/range?"
                };
                assert!(request.contains(endpoint));
                assert!(request.contains(&format!("since={date}&until={date}")));
            }
        }
        assert_eq!(range_dates("9998-11-20", "9998-12-31").unwrap().len(), 42);
        assert!(range_dates("9998-11-19", "9998-12-31").is_none());
    }
    #[test]
    fn range_api_failure_is_not_a_zero_event_snapshot() {
        for status in [400, 404, 500, 503] {
            let (client, worker) = mock(status, "{}");
            assert!(
                matches!(client.calendar_range("work","host","2026-11-01","2026-11-02","UTC"),Err(Error::Http{status:s,..}) if s==status)
            );
            worker.join().unwrap();
        }
    }
    fn calendar_day_fixture() -> Value {
        serde_json::json!({"vault":"work", "date":"2026-11-01", "time_zone":"UTC",
            "day_start":"2026-11-01T00:00:00Z", "day_end":"2026-11-02T00:00:00Z", "events":[]})
    }
    #[test]
    fn agenda_day_verifies_raw_vault_before_normalization() {
        for (requested, launch, wire, accepted) in [
            ("foreign", "host", "", false),
            ("foreign", "host", "foreign", true),
            ("host", "host", "", true),
            ("", "host", "", true),
            ("foreign", "host", "host", false),
        ] {
            let mut body = zoned_calendar_day_fixture();
            body["vault"] = serde_json::json!(wire);
            let (client, worker) = mock(200, body.to_string());
            let result =
                client.calendar_agenda_day(requested, launch, "2026-11-01", "America/New_York");
            worker.join().unwrap();
            assert_eq!(result.is_ok(), accepted, "{requested}/{wire}: {result:?}");
        }
    }
    fn zoned_calendar_day_fixture() -> Value {
        serde_json::json!({"vault":"work", "date":"2026-11-01", "time_zone":"America/New_York",
            "day_start":"2026-11-01T04:00:00Z", "day_end":"2026-11-02T05:00:00Z",
            "day_start_unix":1793505600_i64, "day_end_unix":1793595600_i64, "events":[]})
    }
    #[test]
    fn calendar_named_zone_projection_and_escaped_query() {
        let mut body = zoned_calendar_day_fixture();
        body["events"] = serde_json::json!([calendar_event_fixture()]);
        body["read_state"] = serde_json::json!({"partial":true});
        let (client, worker) = mock(200, body.to_string());
        let result = client
            .calendar_day_events_in_zone("work", "2026-11-01", "America/New_York")
            .unwrap();
        assert_eq!(result.time_zone, "America/New_York");
        assert_eq!(
            result.day_end_unix.unwrap() - result.day_start_unix.unwrap(),
            25 * 3600
        );
        assert_eq!(result.source_vault, "work");
        assert_eq!(result.launch_generation, client.launch_generation);
        assert_eq!(result.events.len(), 1);
        assert_eq!(result.diagnostics["read_state"]["partial"], true);
        assert!(
            worker
                .join()
                .unwrap()
                .contains("time_zone=America%2FNew_York HTTP/")
        );
        // Names are validated authoritatively by the engine, but never injected
        // as query delimiters by the client.
        let (client, worker) = mock(400, "{}");
        assert!(matches!(
            client.calendar_day_events_in_zone("work", "2026-11-01", "Etc/GMT+5&date=x"),
            Err(Error::Http { status: 400, .. })
        ));
        assert!(
            worker
                .join()
                .unwrap()
                .contains("time_zone=Etc%2FGMT%2B5%26date%3Dx HTTP/")
        );
    }
    #[test]
    fn calendar_named_zone_rejects_bad_echo_boundaries_and_records() {
        for (field, value) in [
            ("vault", serde_json::json!("other")),
            ("date", serde_json::json!("2026-11-02")),
            ("time_zone", serde_json::json!("US/Eastern")),
            ("day_start", serde_json::json!("2026-11-01T04:00:00+00:00")),
            ("day_start", serde_json::json!("2026-02-30T04:00:00Z")),
            ("day_start", serde_json::json!("garbage")),
            ("day_start_unix", serde_json::json!(1793505601_i64)),
            ("day_start_unix", serde_json::json!(1793595600_i64)),
            ("day_start_unix", serde_json::json!(-62135596801_i64)),
            ("day_end_unix", serde_json::json!(253402300800_i64)),
            ("day_end_unix", serde_json::json!(1793505600_i64)),
            ("day_end_unix", serde_json::json!(1.5)),
            ("day_end_unix", serde_json::json!("NaN")),
            ("day_end_unix", Value::Null),
            ("events", serde_json::json!([{"id":"bad"}])),
            (
                "events",
                serde_json::json!([calendar_event_fixture(), calendar_event_fixture()]),
            ),
            ("events", Value::Array(vec![calendar_event_fixture(); 4097])),
        ] {
            let mut body = zoned_calendar_day_fixture();
            body[field] = value;
            let (client, worker) = mock(200, body.to_string());
            assert!(
                matches!(
                    client.calendar_day_events_in_zone("work", "2026-11-01", "America/New_York"),
                    Err(Error::Shape(_))
                ),
                "{field}"
            );
            worker.join().unwrap();
        }
        for field in ["day_start_unix", "day_end_unix"] {
            let mut body = zoned_calendar_day_fixture();
            body.as_object_mut().unwrap().remove(field);
            let (client, worker) = mock(200, body.to_string());
            assert!(matches!(
                client.calendar_day_events_in_zone("work", "2026-11-01", "America/New_York"),
                Err(Error::Shape(_))
            ));
            worker.join().unwrap();
        }
    }
    #[test]
    fn calendar_named_zone_input_bounds_and_utc_strictness() {
        let mut body = calendar_day_fixture();
        body["day_start_unix"] = serde_json::json!(1793491200_i64);
        body["day_end_unix"] = serde_json::json!(1793577600_i64);
        let (client, worker) = mock(200, body.to_string());
        for zone in ["", "a\0b", "a\nb", &"x".repeat(129)] {
            assert!(matches!(
                client.calendar_day_events_in_zone("work", "2026-11-01", zone),
                Err(Error::Config(_))
            ));
        }
        assert!(matches!(
            client.calendar_day_events_in_zone("work", "9999-01-01", "UTC"),
            Err(Error::Config(_))
        ));
        assert!(matches!(
            client.calendar_day_events_in_zone("bad\0vault", "2026-11-01", "UTC"),
            Err(Error::Config(_))
        ));
        client
            .calendar_day_events_in_zone("work", "2026-11-01", "UTC")
            .unwrap();
        worker.join().unwrap();
        let mut body = zoned_calendar_day_fixture();
        body["time_zone"] = serde_json::json!("UTC");
        let (client, worker) = mock(200, body.to_string());
        assert!(matches!(
            client.calendar_day_events_in_zone("work", "2026-11-01", "UTC"),
            Err(Error::Shape(_))
        ));
        worker.join().unwrap();
        assert!(calendar_boundary_matches(
            "0001-01-01T00:00:00Z",
            -62135596800
        ));
        assert!(calendar_boundary_matches(
            "9999-12-31T23:59:59Z",
            253402300799
        ));
    }
    #[test]
    fn calendar_day_events_scoped_projection_preserves_labels() {
        let mut e = calendar_event_fixture();
        e["all_day"] = serde_json::json!(false);
        e["start"] = serde_json::json!("2026-11-01T01:30:00.123456789-04:00");
        e["end"] = serde_json::json!("2026-11-01T01:30:00.123456789-05:00");
        e["time_zone"] = serde_json::json!("America/New_York");
        e["status"] = serde_json::json!("cancelled");
        let mut body = calendar_day_fixture();
        body["vault"] = serde_json::json!("work & 日本語");
        body["events"] = serde_json::json!([e]);
        body["read_state"] = serde_json::json!({"partial":true});
        let (client, worker) = mock(200, body.to_string());
        let result = client
            .calendar_day_events("work & 日本語", "2026-11-01")
            .unwrap();
        assert_eq!(result.source_vault, "work & 日本語");
        assert_eq!(result.launch_generation, client.launch_generation);
        assert_eq!(
            result.events[0].start,
            "2026-11-01T01:30:00.123456789-04:00"
        );
        assert_eq!(result.events[0].end, "2026-11-01T01:30:00.123456789-05:00");
        assert_eq!(result.events[0].time_zone, "America/New_York");
        assert_eq!(result.events[0].status, "cancelled");
        assert_eq!(result.diagnostics["read_state"]["partial"], true);
        assert!(worker.join().unwrap().starts_with("GET /api/calendar-events/day?vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E&date=2026-11-01&time_zone=UTC HTTP/"));
    }
    #[test]
    fn calendar_day_events_require_complete_exact_shape() {
        for field in [
            "vault",
            "date",
            "time_zone",
            "day_start",
            "day_end",
            "events",
        ] {
            for replacement in [None, Some(Value::Null), Some(serde_json::json!(123))] {
                let mut body = calendar_day_fixture();
                match replacement {
                    None => {
                        body.as_object_mut().unwrap().remove(field);
                    }
                    Some(value) => body[field] = value,
                }
                let (client, worker) = mock(200, body.to_string());
                assert!(
                    matches!(
                        client.calendar_day_events("work", "2026-11-01"),
                        Err(Error::Shape(_))
                    ),
                    "{field}"
                );
                worker.join().unwrap();
            }
        }
        for (field, value) in [
            ("date", "2026-11-02"),
            ("time_zone", "Etc/UTC"),
            ("day_start", "2026-11-01T00:00:00+00:00"),
            ("day_end", "2026-11-01T23:59:59Z"),
            ("vault", "other"),
        ] {
            let mut body = calendar_day_fixture();
            body[field] = serde_json::json!(value);
            let (client, worker) = mock(200, body.to_string());
            assert!(
                matches!(
                    client.calendar_day_events("work", "2026-11-01"),
                    Err(Error::Shape(_))
                ),
                "{field}"
            );
            worker.join().unwrap();
        }
    }
    #[test]
    fn calendar_day_events_validate_dates_identity_and_bounds() {
        for (date, end) in [
            ("0001-01-01", "0001-01-02"),
            ("2024-02-29", "2024-03-01"),
            ("2026-12-31", "2027-01-01"),
            ("9998-12-31", "9999-01-01"),
        ] {
            assert_eq!(
                utc_day_bounds(date),
                Some((format!("{date}T00:00:00Z"), format!("{end}T00:00:00Z")))
            );
        }
        let (client, worker) = mock(200, calendar_day_fixture().to_string());
        for date in [
            "",
            "0000-01-01",
            "9999-01-01",
            "2026-02-29",
            "2026-11-01 ",
            "２０２６-11-01",
            "2026-1-01",
        ] {
            assert!(matches!(
                client.calendar_day_events("work", date),
                Err(Error::Config(_))
            ));
        }
        assert!(matches!(
            client.calendar_day_events("bad\0vault", "2026-11-01"),
            Err(Error::Config(_))
        ));
        assert!(
            client
                .calendar_day_events("work", "2026-11-01")
                .unwrap()
                .events
                .is_empty()
        );
        worker.join().unwrap();
        for count in [1, 100, 101, 4096, 4097] {
            let mut body = calendar_day_fixture();
            body["events"] = Value::Array(
                (0..count)
                    .map(|n| {
                        let mut e = calendar_event_fixture();
                        e["id"] = serde_json::json!(format!("20000000-0000-4000-8000-{n:012x}"));
                        e
                    })
                    .collect(),
            );
            let (client, worker) = mock(200, body.to_string());
            let result = client.calendar_day_events("work", "2026-11-01");
            if count <= 4096 {
                assert_eq!(result.unwrap().events.len(), count);
            } else {
                assert!(matches!(result, Err(Error::Shape(_))));
            }
            worker.join().unwrap();
        }
        for events in [
            serde_json::json!([calendar_event_fixture(), calendar_event_fixture()]),
            serde_json::json!([{"id":"bad"}]),
        ] {
            let mut body = calendar_day_fixture();
            body["events"] = events;
            let (client, worker) = mock(200, body.to_string());
            assert!(matches!(
                client.calendar_day_events("work", "2026-11-01"),
                Err(Error::Shape(_))
            ));
            worker.join().unwrap();
        }
    }
    #[test]
    fn calendar_day_events_no_fallback_and_launch_alias_stale() {
        for status in [400, 404, 405, 500, 503] {
            let (client, worker) = mock(status, "{}");
            assert!(
                matches!(client.calendar_day_events("work", "2026-11-01"), Err(Error::Http {status:s,..}) if s == status)
            );
            assert!(
                worker
                    .join()
                    .unwrap()
                    .starts_with("GET /api/calendar-events/day?")
            );
        }
        let (client, worker) = mock(200, calendar_day_fixture().to_string());
        let result = client.calendar_day_events("", "2026-11-01").unwrap();
        assert_eq!(result.source_vault, "");
        assert_eq!(result.vault, "work");
        assert!(
            worker
                .join()
                .unwrap()
                .starts_with("GET /api/calendar-events/day?date=2026-11-01&time_zone=UTC HTTP/")
        );
        client.alive.store(false, Ordering::Release);
        assert!(matches!(
            client.calendar_day_events("", "2026-11-01"),
            Err(Error::Stale)
        ));
    }

    #[test]
    fn calendar_day_events_timeout_is_error_without_fallback() {
        let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let worker = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream
                .set_read_timeout(Some(Duration::from_secs(2)))
                .unwrap();
            let mut request = Vec::new();
            let mut byte = [0; 1];
            while !request.ends_with(b"\r\n\r\n") {
                stream.read_exact(&mut byte).unwrap();
                request.push(byte[0]);
            }
            thread::sleep(Duration::from_millis(500));
            listener.set_nonblocking(true).unwrap();
            assert!(
                listener.accept().is_err(),
                "must not retry the legacy endpoint"
            );
            String::from_utf8(request).unwrap()
        });
        let client = ReadClient {
            canonical_read_only: false,
            creation_store: Default::default(),
            creation_fixture: None,
            http: reqwest::blocking::Client::builder()
                .no_proxy()
                .retry(reqwest::retry::never())
                .timeout(Duration::from_millis(200))
                .build()
                .unwrap(),
            origin,
            alive: Arc::new(AtomicBool::new(true)),
            launch_generation: 1,
        };
        assert!(matches!(
            client.calendar_day_events("work", "2026-11-01"),
            Err(Error::Transport(_))
        ));
        assert!(
            worker
                .join()
                .unwrap()
                .starts_with("GET /api/calendar-events/day?")
        );
    }

    fn dated_task_fixture() -> Value {
        serde_json::json!({"note_id":42,"file_kind":"note","title":"日本語",
            "line":3,"state":"TODO","done":false,"priority":"A",
            "scheduled":"2026-11-13","due":"2026-11-14","text":"do the thing"})
    }

    #[test]
    fn open_tasks_use_engine_filter_order_duplicates_and_qualified_scope() {
        let a = dated_task_fixture();
        let mut b = a.clone();
        b["note_id"] = serde_json::json!(9);
        let (client, worker) = mock(
            200,
            serde_json::json!({
                "vault":"work","tasks":[a.clone(),b.clone(),a.clone()],
                "read_state":{"partial":true,"cache_used":true}
            })
            .to_string(),
        );
        let result = client.open_tasks("work", "main").unwrap();
        assert_eq!(
            result.tasks.iter().map(|r| r.note_id).collect::<Vec<_>>(),
            vec![42, 9, 42]
        );
        assert_eq!(result.source_vault, "work");
        assert_eq!(result.diagnostics["read_state"]["partial"], true);
        let request = worker.join().unwrap();
        assert!(request.starts_with("GET /api/tasks?"));
        assert!(request.contains("open=1"));
        assert!(request.contains("vault=work"));
        for (requested, wire, valid) in [
            ("", "", true),
            ("main", "", true),
            ("work", "work", true),
            ("work", "", false),
            ("", "work", false),
        ] {
            let (client, worker) = mock(
                200,
                serde_json::json!({"vault":wire,"tasks":[]}).to_string(),
            );
            assert_eq!(client.open_tasks(requested, "main").is_ok(), valid);
            worker.join().unwrap();
        }
        let (client, worker) = mock(
            200,
            serde_json::json!({"vault":"work","tasks":[a.clone(),a]}).to_string(),
        );
        assert!(client.dated_tasks("work").is_err());
        assert!(!worker.join().unwrap().contains("open="));
        let mut terminal = dated_task_fixture();
        terminal["state"] = serde_json::json!("DONE");
        terminal["done"] = serde_json::json!(true);
        for tasks in [vec![terminal], vec![b; 4097]] {
            let (client, worker) = mock(
                200,
                serde_json::json!({"vault":"work","tasks":tasks}).to_string(),
            );
            assert!(client.open_tasks("work", "main").is_err());
            worker.join().unwrap();
        }
    }
    #[test]
    fn dated_tasks_listing_is_read_only_scoped_and_preserves_all_states() {
        let tasks: Vec<_> = ["TODO", "DOING", "WAITING", "DONE", "CANCELLED"]
            .iter()
            .enumerate()
            .map(|(index, state)| {
                let mut row = dated_task_fixture();
                row["line"] = serde_json::json!(index + 1);
                row["state"] = serde_json::json!(state);
                row["done"] = serde_json::json!(index >= 3);
                if index >= 3 {
                    row["completed"] = serde_json::json!("2026-11-13");
                }
                row
            })
            .collect();
        let body = serde_json::json!({"vault":"work & 日本語","tasks":tasks,
            "read_state":{"partial":true,"cache_used":true,"issues":["offline"]}});
        let (client, task) = mock(200, body.to_string());
        let result = client.dated_tasks("work & 日本語").unwrap();
        assert_eq!(result.vault, "work & 日本語");
        assert_eq!(result.source_vault, "work & 日本語");
        assert_eq!(result.launch_generation, client.launch_generation);
        assert_eq!(result.tasks.len(), 5);
        assert!(result.tasks[4].done);
        assert_eq!(result.tasks[0].path, "");
        assert_eq!(result.tasks[0].completed, "");
        assert_eq!(result.diagnostics["read_state"]["partial"], true);
        assert_eq!(result.diagnostics["read_state"]["cache_used"], true);
        let request = task.join().unwrap();
        assert!(
            request.starts_with("GET /api/tasks?vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E HTTP/")
        );
        assert!(!request.contains("open="));
        assert!(!request.contains("id="));
        assert!(!request.contains("/api/journal"));
    }

    #[test]
    fn dated_tasks_launch_scope_and_empty_results_are_valid() {
        for label in ["", "main"] {
            let (client, task) = mock(
                200,
                serde_json::json!({"vault":label,"tasks":[]}).to_string(),
            );
            let result = client.dated_tasks("").unwrap();
            assert_eq!(result.vault, label);
            assert_eq!(result.source_vault, "");
            assert!(result.tasks.is_empty());
            assert!(task.join().unwrap().starts_with("GET /api/tasks HTTP/"));
        }
    }

    #[test]
    fn dated_tasks_preserve_engine_date_labels_without_hiding_valid_rows() {
        let mut unusual = dated_task_fixture();
        unusual["due"] = serde_json::json!("2026-02-30");
        unusual["completed"] = serde_json::json!("2026-99-00");
        let mut valid = dated_task_fixture();
        valid["note_id"] = serde_json::json!(43);
        let (client, task) = mock(
            200,
            serde_json::json!({"vault":"work","tasks":[unusual, valid]}).to_string(),
        );
        let result = client.dated_tasks("work").unwrap();
        assert_eq!(result.tasks.len(), 2);
        assert_eq!(result.tasks[0].due, "2026-02-30");
        assert_eq!(result.tasks[0].completed, "2026-99-00");
        assert_eq!(result.tasks[0].scheduled, "2026-11-13");
        assert_eq!(result.tasks[1].note_id, 43);
        task.join().unwrap();
        assert!(!calendar_date_valid("2026-02-30"));
        assert!(!date_wire_shape("２０２６-02-30"));
    }

    #[test]
    fn dated_tasks_reject_invalid_rows_and_ambiguous_metadata() {
        for (field, invalid) in [
            ("note_id", serde_json::json!(0)),
            ("note_id", serde_json::json!(-1)),
            ("line", serde_json::json!(0)),
            ("line", serde_json::json!(-1)),
            ("state", serde_json::json!("CUSTOM")),
            ("done", serde_json::json!(true)),
            ("due", serde_json::json!("2026-2-29")),
            ("scheduled", serde_json::json!("tomorrow")),
            ("completed", serde_json::json!("2026-11-XX")),
            ("priority", serde_json::json!("AA")),
            ("title", serde_json::json!("x".repeat(64 * 1024 + 1))),
        ] {
            let mut row = dated_task_fixture();
            row[field] = invalid;
            let (client, task) = mock(
                200,
                serde_json::json!({"vault":"work","tasks":[row]}).to_string(),
            );
            assert!(
                matches!(client.dated_tasks("work"), Err(Error::Shape(_))),
                "{field}"
            );
            task.join().unwrap();
        }
        let mut undated = dated_task_fixture();
        undated["due"] = serde_json::json!("");
        undated["scheduled"] = serde_json::json!("");
        let mut terminal = dated_task_fixture();
        terminal["state"] = serde_json::json!("DONE");
        for body in [
            serde_json::json!({"vault":"work","tasks":[undated]}),
            serde_json::json!({"vault":"work","tasks":[terminal]}),
            serde_json::json!({"vault":"work","tasks":[dated_task_fixture(),dated_task_fixture()]}),
            serde_json::json!({"vault":"work","tasks":vec![dated_task_fixture();4097]}),
            serde_json::json!({"vault":"other","tasks":[]}),
            serde_json::json!({"vault":"work","tasks":null}),
            serde_json::json!({"tasks":[]}),
        ] {
            let (client, task) = mock(200, body.to_string());
            assert!(matches!(client.dated_tasks("work"), Err(Error::Shape(_))));
            task.join().unwrap();
        }
    }

    #[test]
    fn dated_tasks_fail_without_fallback_or_requests_for_invalid_scope() {
        for status in [400, 403, 500, 503] {
            let (client, task) = mock(status, r#"{"error":"selected vault unavailable"}"#);
            for vault in ["x".repeat(1025), "bad\0vault".into()] {
                assert!(matches!(client.dated_tasks(&vault), Err(Error::Config(_))));
            }
            assert!(
                matches!(client.dated_tasks("work"), Err(Error::Http { status: actual, .. }) if actual == status)
            );
            assert!(
                task.join()
                    .unwrap()
                    .starts_with("GET /api/tasks?vault=work HTTP/")
            );
            client.alive.store(false, Ordering::Release);
            assert!(matches!(client.dated_tasks("work"), Err(Error::Stale)));
        }
    }

    #[test]
    fn agenda_is_explicit_read_only_scoped_and_preserves_partial_diagnostics() {
        let (client, task) = mock(
            200,
            r#"{"date":"2024-02-29","notes":[{"note_id":42,"title":"日本語","file_kind":"note","path":"/fixture/42.md"}],"read_state":{"partial":true,"cache_used":true,"issues":["offline"]}}"#,
        );
        let result = client.agenda("2024-02-29", "work & 日本語").unwrap();
        assert_eq!(result.date, "2024-02-29");
        assert_eq!(result.source_vault, "work & 日本語");
        assert_eq!(
            result.notes[0].key(),
            NoteKey {
                vault: "work & 日本語".into(),
                note_id: 42
            }
        );
        assert_eq!(result.launch_generation, client.launch_generation);
        assert_eq!(result.diagnostics["read_state"]["partial"], true);
        assert_eq!(result.diagnostics["read_state"]["cache_used"], true);
        let request = task.join().unwrap();
        assert!(request.starts_with("GET /api/agenda?"));
        assert!(request.contains("vault=work+%26+%E6%97%A5%E6%9C%AC%E8%AA%9E"));
        assert!(request.contains("date=2024-02-29"));
        assert!(!request.contains("/api/journal"));
    }

    #[test]
    fn agenda_empty_and_canonical_launch_scope_are_valid() {
        for body in [
            r#"{"date":"2026-11-13","notes":[]}"#,
            r#"{"date":"2026-11-13","notes":[{"note_id":42,"title":"note","file_kind":"note","path":"/fixture/42.md","vault":"main"}]}"#,
        ] {
            let (client, task) = mock(200, body);
            let result = client.agenda("2026-11-13", "").unwrap();
            assert_eq!(result.source_vault, "");
            if let Some(note) = result.notes.first() {
                assert_eq!(note.vault, "main");
            }
            let request = task.join().unwrap();
            assert!(request.starts_with("GET /api/agenda?date=2026-11-13"));
            assert!(!request.contains("vault="));
        }
    }

    #[test]
    fn agenda_bounds_note_count_and_metadata_without_silent_truncation() {
        let note = serde_json::json!({"note_id":42,"title":"note","file_kind":"note","path":"p"});
        let bodies = [
            serde_json::json!({"date":"2026-11-13","notes":vec![note.clone();4097]}),
            serde_json::json!({"date":"2026-11-13","notes":[{"title":"x".repeat(64*1024+1),"note_id":42,"file_kind":"note","path":"p"}]}),
        ];
        for body in bodies {
            let (client, task) = mock(200, body.to_string());
            assert!(matches!(
                client.agenda("2026-11-13", ""),
                Err(Error::Shape(_))
            ));
            task.join().unwrap();
        }
    }

    #[test]
    fn agenda_rejects_invalid_dates_identity_and_shapes() {
        for body in [
            "{}",
            r#"{"date":"2026-11-13","notes":null}"#,
            r#"{"date":"2026-11-12","notes":[]}"#,
            r#"{"date":"2026-11-13","notes":[{"note_id":0,"title":"note","file_kind":"note","path":"p"}]}"#,
            r#"{"date":"2026-11-13","notes":[{"note_id":-42,"title":"note","file_kind":"note","path":"p"}]}"#,
            r#"{"date":"2026-11-13","notes":[{"note_id":42,"title":"note","file_kind":"note","path":"p","vault":"other"}]}"#,
            r#"{"date":"2026-11-13","notes":[{"note_id":42,"title":"note","file_kind":"note","path":"p"},{"note_id":42,"title":"again","file_kind":"note","path":"p"}]}"#,
        ] {
            let (client, task) = mock(200, body);
            assert!(matches!(
                client.agenda("2026-11-13", "work"),
                Err(Error::Shape(_))
            ));
            task.join().unwrap();
        }
        let (client, task) = mock(
            200,
            r#"{"date":"2026-11-13","notes":[{"note_id":42,"title":"note","file_kind":"note","path":"p","vault":"main"},{"note_id":43,"title":"note","file_kind":"note","path":"q","vault":"other"}]}"#,
        );
        assert!(matches!(
            client.agenda("2026-11-13", ""),
            Err(Error::Shape(_))
        ));
        task.join().unwrap();
        let (client, task) = mock(503, r#"{"error":"selected vault unavailable"}"#);
        for date in [
            "",
            "2026-02-29",
            "2026-11-32",
            "2026-1-01",
            "日本語",
            " 2026-11-13",
        ] {
            assert!(matches!(client.agenda(date, "work"), Err(Error::Config(_))));
        }
        for vault in ["x".repeat(1025), "bad\0vault".into()] {
            assert!(matches!(
                client.agenda("2026-11-13", &vault),
                Err(Error::Config(_))
            ));
        }
        // Local rejection did not consume the fixture's single request. Failure
        // remains an error; there is no journal/default-vault fallback request.
        assert!(matches!(
            client.agenda("2026-11-13", "work"),
            Err(Error::Http { status: 503, .. })
        ));
        assert!(task.join().unwrap().starts_with("GET /api/agenda?"));
        client.alive.store(false, Ordering::Release);
        assert!(matches!(
            client.agenda("2026-11-13", "work"),
            Err(Error::Stale)
        ));
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
            canonical_read_only: false,
            creation_store: Default::default(),
            creation_fixture: None,
            http: reqwest::blocking::Client::new(),
            origin: "http://127.0.0.1:1".into(),
            alive: Arc::new(AtomicBool::new(true)),
            launch_generation: 1,
        };
        let mut server = OwnedServer {
            event_policy: EventPolicy::Sse,
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
        assert!(matches!(OwnedServer::start(config), Err(Error::Config(_))));
        assert!(std::net::TcpStream::connect(listener.local_addr().unwrap()).is_ok());
    }
}

#[cfg(test)]
#[path = "live_save_races.rs"]
mod save_races;

#[cfg(test)]
mod markdown_consume_tests {
    use super::*;
    use std::io::Write;
    fn fixture(
        reply: Option<(u16, &'static str)>,
        delay: bool,
    ) -> (ReadClient, thread::JoinHandle<Vec<String>>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        listener.set_nonblocking(true).unwrap();
        let task = thread::spawn(move || {
            let deadline = Instant::now() + Duration::from_secs(2);
            let mut requests = Vec::new();
            while Instant::now() < deadline {
                if let Ok((mut stream, _)) = listener.accept() {
                    stream.set_nonblocking(false).unwrap();
                    stream
                        .set_read_timeout(Some(Duration::from_secs(1)))
                        .unwrap();
                    let mut bytes = Vec::new();
                    let mut b = [0];
                    while !bytes.ends_with(b"\r\n\r\n") {
                        stream.read_exact(&mut b).unwrap();
                        bytes.push(b[0]);
                    }
                    let header = String::from_utf8(bytes.clone()).unwrap();
                    let size: usize = header
                        .lines()
                        .find_map(|l| {
                            l.to_ascii_lowercase()
                                .strip_prefix("content-length:")
                                .map(str::trim)
                                .map(str::to_string)
                        })
                        .unwrap()
                        .parse()
                        .unwrap();
                    let mut body = vec![0; size];
                    stream.read_exact(&mut body).unwrap();
                    bytes.extend(body);
                    requests.push(String::from_utf8(bytes).unwrap());
                    if delay {
                        thread::sleep(Duration::from_millis(100));
                    }
                    if let Some((status, body)) = reply {
                        let _ = write!(
                            stream,
                            "HTTP/1.1 {status} Test\r\nContent-Length: {}\r\nLocation: /unexpected\r\nConnection: close\r\n\r\n{body}",
                            body.len()
                        );
                    }
                    let end = Instant::now() + Duration::from_millis(60);
                    while Instant::now() < end {
                        if listener.accept().is_ok() {
                            requests.push("unexpected retry".into());
                        }
                        thread::sleep(Duration::from_millis(2));
                    }
                    break;
                }
                thread::sleep(Duration::from_millis(2));
            }
            requests
        });
        (
            ReadClient {
                canonical_read_only: false,
                creation_store: Default::default(),
                creation_fixture: None,
                http: reqwest::blocking::Client::builder()
                    .no_proxy()
                    .retry(reqwest::retry::never())
                    .redirect(reqwest::redirect::Policy::none())
                    .build()
                    .unwrap(),
                origin,
                alive: Arc::new(AtomicBool::new(true)),
                launch_generation: 77,
            },
            task,
        )
    }
    fn capability(c: &ReadClient) -> crate::markdown_handoff::Capability {
        crate::markdown_handoff::Capability::parse(
            &format!("/markdown#handoff={}", "b".repeat(64)),
            c.markdown_origin(),
            c.launch_generation,
        )
        .unwrap()
    }
    #[test]
    fn consume_exactly_one_post_and_never_fallback_or_redirect() {
        for (reply, expected) in [
            (
                Some((
                    200,
                    r#"{"active":"a.md","files":[{"path":"a.md","data":"YQ=="}]}"#,
                )),
                None,
            ),
            (Some((410, "secret")), Some(HandoffError::Expired)),
            (Some((500, "secret")), Some(HandoffError::Http(500))),
            (Some((302, "secret")), Some(HandoffError::Http(302))),
            (None, Some(HandoffError::Uncertain)),
        ] {
            let (c, task) = fixture(reply, false);
            let result = c.consume_markdown(capability(&c));
            if let Some(expected) = expected {
                assert_eq!(result.unwrap_err(), expected);
            } else {
                assert_eq!(result.unwrap().markdown("a.md"), Some("a"));
            }
            let requests = task.join().unwrap();
            assert_eq!(requests.len(), 1);
            assert!(requests[0].starts_with("POST /api/markdown/consume HTTP/1.1"));
            assert!(requests[0].contains("application/json"));
            assert!(!requests[0].contains("handoff="));
        }
    }
    #[test]
    fn timeout_is_uncertain_and_does_not_retry() {
        let (c, task) = fixture(Some((200, "{}")), true);
        assert_eq!(
            c.consume_markdown_with_timeout(capability(&c), Duration::from_millis(25))
                .unwrap_err(),
            HandoffError::Uncertain
        );
        assert_eq!(task.join().unwrap().len(), 1);
    }
    #[test]
    fn launch_binding_and_dead_client_prevent_submission() {
        let (c, task) = fixture(None, false);
        let mut cap = capability(&c);
        cap.launch += 1;
        assert_eq!(c.consume_markdown(cap).unwrap_err(), HandoffError::NotSent);
        let cap = capability(&c);
        c.alive.store(false, Ordering::Release);
        assert_eq!(c.consume_markdown(cap).unwrap_err(), HandoffError::NotSent);
        assert!(task.join().unwrap().is_empty());
    }
    #[test]
    fn stale_response_cannot_publish_and_errors_are_redacted() {
        let (c, task) = fixture(
            Some((
                200,
                r#"{"active":"a.md","files":[{"path":"a.md","data":"YQ=="}]}"#,
            )),
            true,
        );
        let alive = c.alive.clone();
        let stop = thread::spawn(move || {
            thread::sleep(Duration::from_millis(30));
            alive.store(false, Ordering::Release);
        });
        assert_eq!(
            c.consume_markdown(capability(&c)).unwrap_err(),
            HandoffError::Stale
        );
        stop.join().unwrap();
        assert_eq!(task.join().unwrap().len(), 1);
        for error in [
            HandoffError::Uncertain,
            HandoffError::Expired,
            HandoffError::Http(500),
        ] {
            assert!(!error.to_string().contains(&"b".repeat(64)));
        }
    }
}

#[cfg(test)]
mod refresh_namespace_tests {
    #[test]
    fn refresh_requires_raw_foreign_namespace_before_normalization() {
        use super::calendar_range_vault_matches as matches;
        assert!(!matches("foreign", "", "host"));
        assert!(!matches("foreign", "host", "host"));
        assert!(matches("foreign", "foreign", "host"));
        assert!(matches("host", "", "host"));
        assert!(matches("", "host", "host"));
    }
}

#[cfg(test)]
#[path = "live_capability_process.rs"]
mod capability_process_tests;

#[cfg(test)]
#[path = "live_capability_tests.rs"]
mod capability_tests;

#[path = "live_read_inputs.rs"]
mod read_inputs;

#[path = "live_launch_context.rs"]
mod launch_context;

#[path = "live_workspace_provenance.rs"]
mod workspace_provenance;

#[path = "live_launch_protocol.rs"]
mod launch_protocol;

#[path = "live_request_pipe.rs"]
mod request_pipe;

#[path = "live_private_runtime.rs"]
mod private_runtime;

#[cfg(test)]
#[path = "live_private_runtime_contract.rs"]
mod private_runtime_contract_tests;

#[cfg(test)]
#[path = "live_preview_fixture_gate.rs"]
pub(crate) mod preview_fixture_gate_tests;

#[cfg(test)]
#[path = "live_admission_process.rs"]
mod admission_process_tests;

#[path = "live_registry.rs"]
mod registry_contract;

#[cfg(test)]
#[path = "live_bound_fixture.rs"]
mod bound_fixture;

#[cfg(test)]
#[path = "live_graph_contract.rs"]
mod graph_contract_tests;
