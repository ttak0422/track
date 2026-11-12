//! Read-only CLI adapter. Run these blocking operations on a background worker, not GPUI's UI thread.
//! `track notes` can refresh derived caches; neither operation writes note bodies or sidecars.

use serde::Deserialize;
use std::collections::BTreeMap;
use std::fmt;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::thread;
use std::time::{Duration, Instant};

const CLI_TIMEOUT: Duration = Duration::from_secs(30);

#[derive(Clone, Debug, Deserialize)]
pub struct Note {
    pub note_id: i64,
    pub title: String,
    #[serde(default)]
    pub tags: Vec<String>,
    #[serde(default)]
    pub path: PathBuf,
    #[serde(default)]
    pub file_kind: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct Snapshot {
    pub notes: Vec<Note>,
    #[serde(default)]
    pub read_state: Option<ReadState>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct ReadState {
    pub partial: bool,
    #[serde(default)]
    pub cache_used: bool,
    #[serde(default)]
    pub checked_at: i64,
    #[serde(default)]
    pub issues: Vec<serde_json::Value>,
    #[serde(default)]
    pub omitted: usize,
    #[serde(flatten)]
    pub additional: BTreeMap<String, serde_json::Value>,
}

/// Successful commands can carry warnings on stderr; keep them available to the UI.
#[derive(Debug)]
pub struct Response<T> {
    pub value: T,
    pub warnings: String,
}

#[derive(Debug)]
pub enum BackendError {
    InvalidConfig(String),
    Io(std::io::Error),
    Exit {
        operation: &'static str,
        code: Option<i32>,
        stdout: String,
        stderr: String,
    },
    Timeout {
        operation: &'static str,
        stdout: String,
        stderr: String,
    },
    Decode {
        operation: &'static str,
        detail: String,
        stderr: String,
    },
}

impl fmt::Display for BackendError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidConfig(message) => write!(f, "{message}"),
            Self::Io(error) => write!(f, "could not execute Track CLI: {error}"),
            Self::Exit {
                operation,
                code,
                stdout,
                stderr,
            } => write!(
                f,
                "track {operation} failed (exit {code:?}): {}{}{}",
                stderr.trim(),
                if stderr.is_empty() || stdout.is_empty() {
                    ""
                } else {
                    "\n"
                },
                stdout.trim()
            ),
            Self::Timeout {
                operation,
                stdout,
                stderr,
            } => write!(
                f,
                "track {operation} exceeded its execution deadline: {}{}{}",
                stderr.trim(),
                if stderr.is_empty() || stdout.is_empty() {
                    ""
                } else {
                    "\n"
                },
                stdout.trim()
            ),
            Self::Decode {
                operation,
                detail,
                stderr,
            } => write!(
                f,
                "invalid track {operation} response: {detail}{}{}",
                if stderr.is_empty() { "" } else { "\n" },
                stderr.trim()
            ),
        }
    }
}

impl std::error::Error for BackendError {}

#[derive(Clone, Debug)]
pub struct Backend {
    executable: PathBuf,
    vault: PathBuf,
    config: Option<PathBuf>,
    cache: Option<PathBuf>,
}

impl Backend {
    /// Require explicit absolute paths: never silently select the user's default vault or PATH executable.
    pub fn new(executable: PathBuf, vault: PathBuf) -> Result<Self, BackendError> {
        require_absolute("Track executable", &executable)?;
        require_absolute("vault", &vault)?;
        Ok(Self {
            executable,
            vault,
            config: None,
            cache: None,
        })
    }

    /// Optional machine configuration. The explicit vault still overrides the config's default vault.
    pub fn with_config(mut self, config: PathBuf) -> Result<Self, BackendError> {
        require_absolute("configuration", &config)?;
        self.config = Some(config);
        Ok(self)
    }

    /// Useful for disposable fixture tests and for separating derived caches from the regular app.
    pub fn with_cache(mut self, cache: PathBuf) -> Result<Self, BackendError> {
        require_absolute("cache", &cache)?;
        self.cache = Some(cache);
        Ok(self)
    }

    pub fn notes(&self) -> Result<Response<Snapshot>, BackendError> {
        let response = self.run("notes", &[])?;
        let value =
            serde_json::from_str(&response.value).map_err(|error| BackendError::Decode {
                operation: "notes",
                detail: error.to_string(),
                stderr: response.warnings.clone(),
            })?;
        Ok(Response {
            value,
            warnings: response.warnings,
        })
    }

    /// Portable Markdown for reading only, NOT source for editing/saving: export flattens Track links.
    /// The CLI does not expose read_state on this text response; failure and stderr remain preserved.
    pub fn export_markdown(&self, note_id: i64) -> Result<Response<String>, BackendError> {
        if note_id <= 0 {
            return Err(BackendError::InvalidConfig(
                "note id must be positive".into(),
            ));
        }
        self.run("export", &["--id", &note_id.to_string()])
    }

    fn command(&self, operation: &'static str, args: &[&str]) -> Command {
        let mut command = Command::new(&self.executable);
        command
            .arg(operation)
            .args(args)
            .env("TRACK_VAULT", &self.vault);
        if let Some(config) = &self.config {
            command.env("TRACK_CONFIG", config);
        }
        if let Some(cache) = &self.cache {
            command.env("TRACK_CACHE_DIR", cache);
        }
        command
    }

    fn run(
        &self,
        operation: &'static str,
        args: &[&str],
    ) -> Result<Response<String>, BackendError> {
        let output = run_with_deadline(&mut self.command(operation, args), operation, CLI_TIMEOUT)?;
        decode_output(operation, output)
    }
}

/// Kill and reap only the process created by this adapter, including on early I/O errors.
struct OwnedChild(Child);

impl Drop for OwnedChild {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn run_with_deadline(
    command: &mut Command,
    operation: &'static str,
    timeout: Duration,
) -> Result<Output, BackendError> {
    let started = Instant::now();
    let mut child = OwnedChild(
        command
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(BackendError::Io)?,
    );
    let stdout = child.0.stdout.take().expect("stdout was piped");
    let stderr = child.0.stderr.take().expect("stderr was piped");
    // Drain both pipes immediately; waiting for exit before reading either can deadlock
    // once a CLI fills the pipe buffer. Track's read commands do not spawn descendants.
    let stdout = thread::spawn(move || drain(stdout));
    let stderr = thread::spawn(move || drain(stderr));
    let mut timed_out = false;
    let status = loop {
        if let Some(status) = child.0.try_wait().map_err(BackendError::Io)? {
            break status;
        }
        if started.elapsed() >= timeout {
            timed_out = true;
            // A process that exits between try_wait and kill is already reaped by wait.
            let _ = child.0.kill();
            break child.0.wait().map_err(BackendError::Io)?;
        }
        thread::sleep(Duration::from_millis(10).min(timeout.saturating_sub(started.elapsed())));
    };
    let stdout = join_drain(stdout)?;
    let stderr = join_drain(stderr)?;
    if timed_out {
        return Err(BackendError::Timeout {
            operation,
            stdout: String::from_utf8_lossy(&stdout).into_owned(),
            stderr: String::from_utf8_lossy(&stderr).into_owned(),
        });
    }
    Ok(Output {
        status,
        stdout,
        stderr,
    })
}

fn drain(mut pipe: impl Read) -> std::io::Result<Vec<u8>> {
    let mut bytes = Vec::new();
    pipe.read_to_end(&mut bytes)?;
    Ok(bytes)
}

fn join_drain(
    reader: thread::JoinHandle<std::io::Result<Vec<u8>>>,
) -> Result<Vec<u8>, BackendError> {
    reader
        .join()
        .map_err(|_| BackendError::Io(std::io::Error::other("CLI output reader panicked")))?
        .map_err(BackendError::Io)
}

fn require_absolute(label: &str, path: &Path) -> Result<(), BackendError> {
    if !path.is_absolute() {
        return Err(BackendError::InvalidConfig(format!(
            "{label} must be an absolute path"
        )));
    }
    Ok(())
}

fn decode_output(
    operation: &'static str,
    output: Output,
) -> Result<Response<String>, BackendError> {
    let stderr = String::from_utf8_lossy(&output.stderr).into_owned();
    if !output.status.success() {
        return Err(BackendError::Exit {
            operation,
            code: output.status.code(),
            stdout: String::from_utf8_lossy(&output.stdout).into_owned(),
            stderr,
        });
    }
    let value = String::from_utf8(output.stdout).map_err(|error| BackendError::Decode {
        operation,
        detail: error.to_string(),
        stderr: stderr.clone(),
    })?;
    Ok(Response {
        value,
        warnings: stderr,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::OsStr;

    #[test]
    fn explicit_paths_and_arguments_are_not_shell_interpreted() {
        let backend = Backend::new(
            "/tmp/track executable".into(),
            "/tmp/vault; $(touch unsafe)".into(),
        )
        .unwrap();
        let command = backend.command("export", &["--id", "42"]);
        assert_eq!(command.get_program(), OsStr::new("/tmp/track executable"));
        assert_eq!(
            command.get_args().collect::<Vec<_>>(),
            vec!["export", "--id", "42"]
        );
        assert!(command.get_envs().any(
            |(k, v)| k == "TRACK_VAULT" && v == Some(OsStr::new("/tmp/vault; $(touch unsafe)"))
        ));
        assert!(Backend::new("track".into(), "/tmp/vault".into()).is_err());
        assert!(Backend::new("/tmp/track".into(), "vault".into()).is_err());
    }

    #[test]
    fn partial_state_preserves_issue_details_and_future_fields() {
        let snapshot: Snapshot = serde_json::from_str(r#"{"notes":[{"note_id":42,"title":"日本語"}],"read_state":{"partial":true,"cache_used":true,"checked_at":123,"issues":[{"path":"note/42.md","reason":"unreadable","cached":true}],"omitted":2,"future":"value"}}"#).unwrap();
        assert_eq!(snapshot.notes[0].title, "日本語");
        assert!(snapshot.notes[0].tags.is_empty());
        let state = snapshot.read_state.unwrap();
        assert!(state.partial && state.cache_used);
        assert_eq!(state.checked_at, 123);
        assert_eq!(state.omitted, 2);
        assert_eq!(state.issues[0]["reason"], "unreadable");
        assert_eq!(state.additional["future"], "value");
    }

    #[test]
    fn empty_snapshot_and_invalid_snapshot() {
        let snapshot: Snapshot = serde_json::from_str(r#"{"notes":[]}"#).unwrap();
        assert!(snapshot.notes.is_empty());
        assert!(snapshot.read_state.is_none());
        assert!(serde_json::from_str::<Snapshot>(r#"{}"#).is_err());
    }

    #[cfg(unix)]
    fn output(code: i32, stdout: &[u8], stderr: &[u8]) -> Output {
        use std::os::unix::process::ExitStatusExt;
        Output {
            status: std::process::ExitStatus::from_raw(code << 8),
            stdout: stdout.into(),
            stderr: stderr.into(),
        }
    }

    #[test]
    #[cfg(unix)]
    fn failed_process_retains_both_streams() {
        let error =
            decode_output("notes", output(2, b"structured error", b"read failed")).unwrap_err();
        match error {
            BackendError::Exit {
                code,
                stdout,
                stderr,
                ..
            } => {
                assert_eq!(code, Some(2));
                assert_eq!(stdout, "structured error");
                assert_eq!(stderr, "read failed");
            }
            other => panic!("unexpected error: {other}"),
        }
    }

    #[test]
    #[cfg(unix)]
    fn successful_export_retains_warnings_and_rejects_bad_utf8() {
        let response = decode_output("export", output(0, b"# Title\n", b"export warning")).unwrap();
        assert_eq!(response.value, "# Title\n");
        assert_eq!(response.warnings, "export warning");
        assert!(matches!(
            decode_output("export", output(0, &[0xff], b"")),
            Err(BackendError::Decode { .. })
        ));
    }
    #[test]
    #[cfg(unix)]
    fn deadline_kills_and_reaps_owned_child_and_preserves_output() {
        let mut command = Command::new("/bin/sh");
        // exec replaces the mock shell rather than leaving an unowned descendant.
        command.args(["-c", "printf ready; printf warning >&2; exec /bin/sleep 5"]);
        let started = Instant::now();
        let result = run_with_deadline(&mut command, "notes", Duration::from_millis(100));
        assert!(
            started.elapsed() < Duration::from_secs(2),
            "deadline did not terminate the child promptly"
        );
        match result.unwrap_err() {
            BackendError::Timeout { stdout, stderr, .. } => {
                assert_eq!(stdout, "ready");
                assert_eq!(stderr, "warning");
            }
            other => panic!("unexpected error: {other}"),
        }
    }

    #[test]
    #[cfg(unix)]
    fn drains_both_pipes_while_command_runs() {
        let mut command = Command::new("/bin/sh");
        command.args(["-c", "i=0; while [ \"$i\" -lt 10000 ]; do printf 12345678; printf abcdefgh >&2; i=$((i+1)); done"]);
        let output = run_with_deadline(&mut command, "export", Duration::from_secs(5)).unwrap();
        assert!(output.status.success());
        assert_eq!(output.stdout.len(), 80000);
        assert_eq!(output.stderr.len(), 80000);
    }
}
