//! Test-only preview correlation around independent synthetic fixture authority.
//! No ordinary grant or serve route. Two-phase checks are neither continuous nor
//! atomic authorization against a hostile filesystem or concurrently changed UI.
use super::launch_context::ResolutionContextRequest;
use super::private_runtime::{CleanupOutcome, PrivateRuntime};
use super::*;
use crate::workspace_read_request::{Decision, EventStamp, RequestDialog, WorkspaceRequest};
use std::ffi::OsString;
use std::os::unix::process::CommandExt;
use std::path::Path;

fn rejected() -> Error {
    Error::Config("Fixture preview review rejected; new review required".into())
}
fn entropy() -> String {
    let mut bytes = [0; 32];
    std::fs::File::open("/dev/urandom")
        .unwrap()
        .read_exact(&mut bytes)
        .unwrap();
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}
fn context(command: &Command) -> Result<ResolutionContextRequest, Error> {
    let entries = command
        .get_envs()
        .filter_map(|(key, value)| value.map(|value| (key.to_owned(), value.to_owned())))
        .collect::<Vec<_>>();
    ResolutionContextRequest::capture(command.get_current_dir().ok_or_else(rejected)?, &entries)
        .map_err(|_| rejected())
}
#[derive(PartialEq, Eq)]
struct CommandShape {
    program: OsString,
    args: Vec<OsString>,
    env: Vec<(OsString, Option<OsString>)>,
    cwd: Option<PathBuf>,
}
impl CommandShape {
    fn capture(c: &Command) -> Self {
        Self {
            program: c.get_program().to_owned(),
            args: c.get_args().map(|a| a.to_owned()).collect(),
            env: c
                .get_envs()
                .map(|(k, v)| (k.to_owned(), v.map(|v| v.to_owned())))
                .collect(),
            cwd: c.get_current_dir().map(|p| p.to_owned()),
        }
    }
}
fn build(
    proof: &read_inputs::SyntheticCanonical,
    root: &Path,
    runtime: &PrivateRuntime,
) -> Result<Command, Error> {
    let mut source = Command::new(&proof.launch().executable);
    proof.configure(&mut source).map_err(Error::Config)?;
    // Copy only explicit fixture bindings, never the ambient environment or the
    // source command's CLI selector. The bridge consumes the bounded pipe request.
    let mut command = Command::new(source.get_program());
    command
        .env_clear()
        .current_dir(source.get_current_dir().unwrap());
    for (key, value) in source.get_envs() {
        if let Some(value) = value {
            command.env(key, value);
        }
    }
    command
        .arg("-test.run=^TestNativeBoundFixtureBridge$")
        .env("GO_TRACK_BOUND_MODE", "capture")
        .env("GO_TRACK_BOUND_ROOT", root)
        .env("GO_TRACK_BOUND_SELECTED", "work")
        .env("GO_TRACK_BOUND_PREFLIGHT_PRIVATE_ROOT", runtime.path())
        .env(
            "TRACK_WEB_DESKTOP_PARENT_PID",
            std::process::id().to_string(),
        )
        .env("TRACK_WEB_DESKTOP_READY_TOKEN", entropy())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .process_group(0);
    Ok(command)
}
fn expected(root: &Path) -> WorkspaceRequest {
    WorkspaceRequest::new(
        "work".into(),
        root.join("home/work").to_str().unwrap().into(),
        ["main", "work"]
            .into_iter()
            .map(|n| (n.into(), root.join("home").join(n).to_str().unwrap().into()))
            .collect(),
    )
    .unwrap()
}
#[derive(PartialEq, Eq)]
enum State {
    Reviewed,
    Rejected,
    Complete,
}
struct FixturePreflightReview {
    proof: read_inputs::SyntheticCanonical,
    runtime: PrivateRuntime,
    command: Command,
    shape: CommandShape,
    context: ResolutionContextRequest,
    request: WorkspaceRequest,
    stamp: EventStamp,
    state: State,
    spawn_attempts: usize,
}
impl FixturePreflightReview {
    fn new(
        proof: read_inputs::SyntheticCanonical,
        runtime: PrivateRuntime,
        root: &Path,
        dialog: &RequestDialog,
    ) -> Result<Self, Error> {
        proof.verify().map_err(Error::Config)?;
        runtime.verify()?;
        let launch = proof.launch();
        if dialog.decision() != Decision::Pending
            || dialog.request() != &expected(root)
            || launch.config != Some(root.join("config"))
            || launch.cache != Some(root.join("private"))
            || !matches!(launch.vault, Vault::Registered(ref name) if name == "work")
            || runtime.path().parent() != Some(root.join("private").as_path())
        {
            return Err(rejected());
        }
        let command = build(&proof, root, &runtime)?;
        Ok(Self {
            context: context(&command)?,
            shape: CommandShape::capture(&command),
            command,
            proof,
            runtime,
            request: dialog.request().clone(),
            stamp: dialog.stamp(),
            state: State::Reviewed,
            spawn_attempts: 0,
        })
    }
    fn check(&self, dialog: &RequestDialog, canceled: &AtomicBool) -> Result<(), Error> {
        if canceled.load(Ordering::Acquire)
            || dialog.stamp() != self.stamp
            || dialog.request() != &self.request
            || dialog.decision() != Decision::PreviewConfirmed
            || context(&self.command)? != self.context
            || CommandShape::capture(&self.command) != self.shape
            || self.command.get_program() != self.proof.launch().executable.as_os_str()
        {
            return Err(rejected());
        }
        self.proof.verify().map_err(Error::Config)?;
        self.runtime.verify()?;
        Ok(())
    }
    fn capture(&mut self, dialog: &RequestDialog, canceled: &AtomicBool) -> Result<Value, Error> {
        if self.state != State::Reviewed {
            return Err(rejected());
        }
        // Fail closed before any fallible operation; only the successful result
        // advances to Complete. Restoring an input never revives this review.
        self.state = State::Rejected;
        self.check(dialog, canceled)?;
        let request = launch_protocol::BoundLaunchRequest::new(
            self.context.clone(),
            workspace_provenance::RequestedSelector::Registered("work".into()),
            self.runtime.path().to_str().unwrap().into(),
            entropy(),
        )
        .map_err(|_| rejected())?;
        let payload = request.preflight().map_err(|_| rejected())?;
        if canceled.load(Ordering::Acquire) {
            return Err(rejected());
        }
        self.spawn_attempts += 1;
        let outcome = bound_fixture::capture_exchange(
            &mut self.command,
            Duration::from_secs(10),
            canceled,
            Some(payload),
        );
        self.check(dialog, canceled)?;
        let bytes = outcome?;
        let text = std::str::from_utf8(&bytes).map_err(|_| rejected())?;
        let mut lines = text
            .lines()
            .filter_map(|s| s.strip_prefix("TRACK_BOUND_MANIFEST:"));
        let raw = lines.next().ok_or_else(rejected)?;
        if lines.next().is_some() {
            return Err(rejected());
        }
        let observation = workspace_provenance::LaunchManifestObservation::parse(raw.as_bytes())
            .map_err(|_| rejected())?;
        let scope = workspace_provenance::WorkspaceScopeRequest::new(
            workspace_provenance::RequestedSelector::Registered("work".into()),
            workspace_provenance::PeerScopeRequest::ExplicitRequestedSet(
                self.request
                    .rows()
                    .map(|(n, p, _)| (n.into(), p.into()))
                    .collect(),
            ),
        )
        .map_err(|_| rejected())?;
        observation
            .check_requested_scope(&scope)
            .map_err(|_| rejected())?;
        let value: Value = serde_json::from_str(raw).map_err(|_| rejected())?;
        if !value["private_identity"]
            .as_str()
            .is_some_and(|identity| self.runtime.matches_observed_identity(identity))
            || value["private_root"] != self.runtime.path().to_str().unwrap()
            || value["active"]["display_path"] != self.request.active().1
        {
            return Err(rejected());
        }
        self.check(dialog, canceled)?;
        self.state = State::Complete;
        Ok(value)
    }
    fn cleanup_empty(&mut self) -> std::io::Result<CleanupOutcome> {
        self.runtime.cleanup_empty()
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::workspace_read_request::Event;
    use std::os::unix::fs::PermissionsExt;
    fn setup(
        binary: &Path,
    ) -> (
        admission_process_tests::Fixture,
        FixturePreflightReview,
        RequestDialog,
    ) {
        let (fixture, proof) = admission_process_tests::fixture(binary);
        let runtime = PrivateRuntime::create(&fixture.0.join("private")).unwrap();
        let dialog = RequestDialog::new(expected(&fixture.0));
        let review = FixturePreflightReview::new(proof, runtime, &fixture.0, &dialog).unwrap();
        (fixture, review, dialog)
    }
    fn confirm(dialog: &mut RequestDialog) {
        assert!(dialog.apply(&dialog.stamp(), Event::ExplicitConfirm));
    }
    fn denied(review: &mut FixturePreflightReview, dialog: &RequestDialog, canceled: &AtomicBool) {
        assert!(review.capture(dialog, canceled).is_err());
        assert_eq!(review.spawn_attempts, 0);
        assert!(review.state == State::Rejected);
    }
    #[test]
    fn unconfirmed_canceled_stale_or_other_dialog_never_spawn_and_do_not_revive() {
        for scenario in 0..7 {
            let (fixture, mut review, mut dialog) = setup(&std::env::current_exe().unwrap());
            let canceled = AtomicBool::new(false);
            match scenario {
                0 => {}
                1 => {
                    dialog.apply(&dialog.stamp(), Event::Cancel);
                }
                2 => {
                    dialog.replace(expected(&fixture.0)).unwrap();
                    confirm(&mut dialog);
                }
                3 => {
                    dialog = RequestDialog::new(expected(&fixture.0));
                    confirm(&mut dialog);
                }
                4 => {
                    confirm(&mut dialog);
                    canceled.store(true, Ordering::Release);
                }
                5 => {
                    dialog
                        .replace(
                            WorkspaceRequest::new(
                                "main".into(),
                                fixture.0.join("home/main").to_str().unwrap().into(),
                                expected(&fixture.0)
                                    .rows()
                                    .map(|(n, p, _)| (n.into(), p.into()))
                                    .collect(),
                            )
                            .unwrap(),
                        )
                        .unwrap();
                    confirm(&mut dialog);
                }
                _ => {
                    dialog
                        .replace(
                            WorkspaceRequest::new(
                                "work".into(),
                                fixture.0.join("home/work").to_str().unwrap().into(),
                                vec![(
                                    "work".into(),
                                    fixture.0.join("home/work").to_str().unwrap().into(),
                                )],
                            )
                            .unwrap(),
                        )
                        .unwrap();
                    confirm(&mut dialog);
                }
            }
            denied(&mut review, &dialog, &canceled);
            canceled.store(false, Ordering::Release);
            if dialog.decision() == Decision::Pending {
                confirm(&mut dialog);
            }
            denied(&mut review, &dialog, &canceled);
            assert_eq!(review.cleanup_empty().unwrap(), CleanupOutcome::Removed);
        }
    }
    #[test]
    fn changed_final_context_or_command_is_sticky_before_spawn() {
        for key in [
            "HOME",
            "XDG_CACHE_HOME",
            "XDG_CONFIG_HOME",
            "TRACK_CONFIG",
            "TRACK_WEB_DESKTOP_READY_TOKEN",
            "PWD",
            "cwd",
            "args",
            "program",
            "GO_TRACK_BOUND_PREFLIGHT_PRIVATE_ROOT",
        ] {
            let (fixture, mut review, mut dialog) = setup(&std::env::current_exe().unwrap());
            confirm(&mut dialog);
            let original = std::mem::replace(
                &mut review.command,
                build(&review.proof, &fixture.0, &review.runtime).unwrap(),
            );
            // Start from exact same reviewed explicit env, including owner token.
            review.command = Command::new(original.get_program());
            review
                .command
                .env_clear()
                .args(original.get_args())
                .current_dir(original.get_current_dir().unwrap());
            for (k, v) in original.get_envs() {
                if let Some(v) = v {
                    review.command.env(k, v);
                }
            }
            match key {
                "cwd" => {
                    review.command.current_dir(fixture.0.join("home"));
                }
                "args" => {
                    review.command.arg("unreviewed");
                }
                "program" => {
                    review.command = Command::new("/not-the-owned-executable");
                }
                _ => {
                    review.command.env(key, "/changed-fixture-input");
                }
            }
            denied(&mut review, &dialog, &AtomicBool::new(false));
            review.command = original;
            denied(&mut review, &dialog, &AtomicBool::new(false));
            assert_eq!(review.cleanup_empty().unwrap(), CleanupOutcome::Removed);
        }
    }
    #[test]
    fn proof_or_owned_root_revalidation_failure_is_sticky() {
        for change_root in [false, true] {
            let (fixture, mut review, mut dialog) = setup(&std::env::current_exe().unwrap());
            confirm(&mut dialog);
            if change_root {
                std::fs::set_permissions(
                    review.runtime.path(),
                    std::fs::Permissions::from_mode(0o755),
                )
                .unwrap();
                denied(&mut review, &dialog, &AtomicBool::new(false));
                std::fs::set_permissions(
                    review.runtime.path(),
                    std::fs::Permissions::from_mode(0o700),
                )
                .unwrap();
            } else {
                let path = fixture.0.join("config");
                let original = std::fs::read(&path).unwrap();
                std::fs::write(&path, b"changed fixture config").unwrap();
                denied(&mut review, &dialog, &AtomicBool::new(false));
                std::fs::write(&path, original).unwrap();
            }
            denied(&mut review, &dialog, &AtomicBool::new(false));
            assert_eq!(review.cleanup_empty().unwrap(), CleanupOutcome::Removed);
        }
    }
    #[test]
    fn cleanup_retains_unknown_contents_and_owned_identity_is_not_a_path_claim() {
        let (_fixture, mut review, _dialog) = setup(&std::env::current_exe().unwrap());
        assert!(
            !review
                .runtime
                .matches_observed_identity("different:identity")
        );
        let sentinel = review.runtime.path().join("unknown-content");
        std::fs::write(&sentinel, b"owned fixture sentinel").unwrap();
        assert_eq!(
            review.cleanup_empty().unwrap(),
            CleanupOutcome::RetainedNonEmpty
        );
        assert_eq!(std::fs::read(&sentinel).unwrap(), b"owned fixture sentinel");
        // Explicit fixture owner removes only the file it created, never gate cleanup.
        std::fs::remove_file(sentinel).unwrap();
        assert_eq!(review.cleanup_empty().unwrap(), CleanupOutcome::Removed);
    }
    #[test]
    #[ignore = "explicit Go fixture bridge binary and synthetic-only opt-in"]
    fn confirmed_fixture_preflight_is_one_shot_without_ordinary_admission() {
        assert_eq!(
            std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
            Ok("1")
        );
        let binary = PathBuf::from(std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").unwrap());
        assert!(binary.is_absolute());
        let (fixture, mut review, mut dialog) = setup(&binary);
        let before = admission_process_tests::protected_inventory(&fixture.0);
        confirm(&mut dialog);
        let result = review.capture(&dialog, &AtomicBool::new(false)).unwrap();
        assert_eq!(result["active"]["name"], "work");
        assert_eq!(review.spawn_attempts, 1);
        assert!(review.capture(&dialog, &AtomicBool::new(false)).is_err());
        assert_eq!(review.spawn_attempts, 1);
        assert_eq!(
            admission_process_tests::protected_inventory(&fixture.0),
            before
        );
        assert_eq!(std::fs::read_dir(review.runtime.path()).unwrap().count(), 0);
        assert!(OwnedServer::start(review.proof.launch()).is_err());
        assert_eq!(review.cleanup_empty().unwrap(), CleanupOutcome::Removed);
    }
}
