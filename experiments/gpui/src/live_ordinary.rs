//! Explicit workspace review, one-shot admission and the production bound CLI.
use super::launch_context::ResolutionContextRequest;
use super::launch_protocol::BoundLaunchRequest;
use super::private_runtime::PrivateRuntime;
use super::workspace_provenance::{LaunchManifestObservation, RequestedSelector};
use super::*;
use crate::workspace_read_request::{Decision, EventStamp, RequestDialog, WorkspaceRequest};
use std::ffi::OsString;
use std::os::unix::{fs::MetadataExt, process::CommandExt};
use std::path::Path;

fn rejected() -> Error {
    Error::Config(
        "Read-only workspace review changed or could not be verified. Restart and review again."
            .into(),
    )
}
fn entropy() -> Result<String, Error> {
    let mut bytes = [0u8; 32];
    std::fs::File::open("/dev/urandom")?.read_exact(&mut bytes)?;
    Ok(bytes.iter().map(|v| format!("{v:02x}")).collect())
}
#[derive(PartialEq, Eq)]
struct Executable {
    path: PathBuf,
    device: u64,
    inode: u64,
    size: u64,
    mode: u32,
    uid: u32,
    modified: std::time::SystemTime,
}
impl Executable {
    fn capture(path: &Path) -> Result<Self, Error> {
        let meta = std::fs::symlink_metadata(path)?;
        if !path.is_absolute()
            || path.canonicalize()?.as_os_str() != path.as_os_str()
            || !meta.is_file()
            || meta.nlink() != 1
            || meta.mode() & 0o022 != 0
            || meta.mode() & 0o111 == 0
        {
            return Err(rejected());
        }
        Ok(Self {
            path: path.to_owned(),
            device: meta.dev(),
            inode: meta.ino(),
            size: meta.len(),
            mode: meta.mode(),
            uid: meta.uid(),
            modified: meta.modified()?,
        })
    }
    fn verify(&self) -> Result<(), Error> {
        if *self != Self::capture(&self.path)? {
            return Err(rejected());
        }
        Ok(())
    }
}

/// A launch request records intent to review the selected workspace and all its
/// registered peers. It cannot spawn a serving process or read note storage.
/// Neither this request nor an observed manifest is an access grant.
pub(crate) struct ReadRequest {
    config: LaunchConfig,
    executable: Executable,
    parent: FileIdentity,
    context: ResolutionContextRequest,
    environment: Vec<(OsString, OsString)>,
    token: String,
    nonce: String,
}
impl ReadRequest {
    pub(crate) fn capture(mut config: LaunchConfig) -> Result<Self, Error> {
        config.validate()?;
        if config.read_fixture.is_some() || config.creation_fixture.is_some() {
            return Err(rejected());
        }
        config.executable = config.executable.canonicalize()?;
        let parent = config.cache.as_ref().ok_or_else(rejected)?;
        verify_parent(parent)?;
        let token = entropy()?;
        let environment = child_environment(&config, &token);
        let context = capture_context(&environment)?;
        let request = Self {
            executable: Executable::capture(&config.executable)?,
            parent: FileIdentity::capture(parent).map_err(|_| rejected())?,
            config,
            context,
            environment,
            token,
            nonce: entropy()?,
        };
        request.protocol(parent_path(&request), &request.nonce)?;
        Ok(request)
    }
    fn verify(&self) -> Result<(), Error> {
        self.executable.verify()?;
        verify_parent(parent_path(self))?;
        if !self.parent.unchanged()
            || capture_context(&child_environment(&self.config, &self.token))? != self.context
        {
            return Err(rejected());
        }
        Ok(())
    }
    fn protocol(&self, root: &Path, nonce: &str) -> Result<BoundLaunchRequest, Error> {
        let selector = match &self.config.vault {
            Vault::Path(p) => RequestedSelector::Path(p.to_str().ok_or_else(rejected)?.into()),
            Vault::Registered(n) => RequestedSelector::Registered(n.clone()),
        };
        BoundLaunchRequest::new(
            self.context.clone(),
            selector,
            root.to_str().ok_or_else(rejected)?.into(),
            nonce.into(),
        )
        .map_err(|_| rejected())
    }
    fn command(&self, root: &Path) -> Result<Command, Error> {
        self.verify()?;
        let mut cmd = Command::new(&self.config.executable);
        cmd.env_clear()
            .env("PATH", "/usr/bin:/bin")
            .env("TMPDIR", root)
            .current_dir(self.context.cwd());
        for (k, v) in &self.environment {
            cmd.env(k, v);
        }
        cmd.arg("__native-read");
        Ok(cmd)
    }
    fn observe(
        &self,
        root: &Path,
        canceled: &AtomicBool,
    ) -> Result<LaunchManifestObservation, Error> {
        self.verify()?;
        let expected_root = FileIdentity::capture(root).map_err(|_| rejected())?;
        let expected_meta = std::fs::metadata(root)?;
        let expected_identity = format!(
            "{}:{}",
            expected_meta.dev() as libc::dev_t,
            expected_meta.ino()
        );
        let payload = self
            .protocol(root, &self.nonce)?
            .preflight()
            .map_err(|_| rejected())?;
        let mut command = self.command(root)?;
        command
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .process_group(0);
        let raw = super::capture::capture_exchange(
            &mut command,
            self.config.startup_timeout,
            canceled,
            Some(payload),
        )?;
        self.verify()?;
        if canceled.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        let observation = LaunchManifestObservation::parse(&raw).map_err(|_| rejected())?;
        if !expected_root.unchanged()
            || !observation
                .private_binding_matches(root.to_str().ok_or_else(rejected)?, &expected_identity)
        {
            return Err(rejected());
        }
        // The bounded typed decoder retains the selector. No request may
        // silently discover a different active workspace.
        let scope = super::workspace_provenance::WorkspaceScopeRequest::new(
            match &self.config.vault {
                Vault::Path(p) => RequestedSelector::Path(p.to_str().ok_or_else(rejected)?.into()),
                Vault::Registered(n) => RequestedSelector::Registered(n.clone()),
            },
            super::workspace_provenance::PeerScopeRequest::ExplicitRequestedSet(
                observation.peers(),
            ),
        )
        .map_err(|_| rejected())?;
        observation
            .check_requested_scope(&scope)
            .map_err(|_| rejected())?;
        Ok(observation)
    }
    pub(crate) fn review(self, canceled: &AtomicBool) -> Result<ReadReview, Error> {
        let observed = self.observe(parent_path(&self), canceled)?;
        let presentation = presentation(&observed)?;
        Ok(ReadReview {
            request: self,
            observed,
            presentation,
            stamp: None,
        })
    }
}
fn parent_path(request: &ReadRequest) -> &Path {
    &request.parent.path
}
fn verify_parent(path: &Path) -> Result<(), Error> {
    let meta = std::fs::symlink_metadata(path)?;
    // SAFETY: geteuid takes no pointers and only observes this process identity.
    let uid = unsafe { libc::geteuid() };
    if !path.is_absolute()
        || path.canonicalize()?.as_os_str() != path.as_os_str()
        || !meta.is_dir()
        || meta.uid() != uid
        || meta.mode() & 0o7777 != 0o700
    {
        return Err(rejected());
    }
    Ok(())
}
fn child_environment(config: &LaunchConfig, token: &str) -> Vec<(OsString, OsString)> {
    let mut values: BTreeMap<OsString, OsString> = std::env::vars_os()
        .filter(|(k, _)| {
            let k = k.as_encoded_bytes();
            k == b"HOME" || k == b"PWD" || k.starts_with(b"TRACK_") || k.starts_with(b"XDG_")
        })
        .collect();
    if let Some(path) = &config.config {
        values.insert("TRACK_CONFIG".into(), path.as_os_str().to_owned());
    }
    values.insert(
        "TRACK_WEB_DESKTOP_PARENT_PID".into(),
        std::process::id().to_string().into(),
    );
    values.insert("TRACK_WEB_DESKTOP_READY_TOKEN".into(), token.into());
    values.into_iter().collect()
}
fn capture_context(
    environment: &[(OsString, OsString)],
) -> Result<ResolutionContextRequest, Error> {
    let physical = std::env::current_dir()?;
    let mut cwd = physical.clone();
    if let Some((_, pwd)) = environment.iter().find(|(k, _)| k == "PWD") {
        let candidate = Path::new(pwd);
        if candidate.is_absolute() && candidate.canonicalize().ok() == physical.canonicalize().ok()
        {
            cwd = candidate.to_owned();
        }
    }
    ResolutionContextRequest::capture(&cwd, environment).map_err(|_| rejected())
}
fn presentation(observation: &LaunchManifestObservation) -> Result<WorkspaceRequest, Error> {
    let (name, path) = observation.active();
    WorkspaceRequest::new(
        name.into(),
        path.into(),
        observation
            .peers()
            .into_iter()
            .filter(|(name, _)| !name.is_empty())
            .collect(),
    )
    .map_err(|_| rejected())
}

pub(crate) struct ReadReview {
    request: ReadRequest,
    observed: LaunchManifestObservation,
    presentation: WorkspaceRequest,
    stamp: Option<EventStamp>,
}
impl ReadReview {
    pub(crate) fn dialog(&mut self) -> RequestDialog {
        let dialog = RequestDialog::new(self.presentation.clone());
        self.stamp = Some(dialog.stamp());
        dialog
    }
    pub(crate) fn cache_parent(&self) -> &Path {
        parent_path(&self.request)
    }
    /// Only the explicit current dialog decision can mint a one-shot grant.
    pub(crate) fn authorize(self, dialog: &RequestDialog) -> Result<ReadGrant, Error> {
        if self.stamp.as_ref() != Some(&dialog.stamp())
            || dialog.request() != &self.presentation
            || dialog.decision() != Decision::PreviewConfirmed
        {
            return Err(rejected());
        }
        Ok(ReadGrant { review: self })
    }
}
pub(crate) struct ReadGrant {
    review: ReadReview,
}
pub(super) struct AdmittedRead {
    request: ReadRequest,
    pub(super) manifest: LaunchManifestObservation,
    runtime: Option<Arc<ReadRuntime>>,
}
impl ReadGrant {
    pub(super) fn prepare(self, canceled: &AtomicBool) -> Result<AdmittedRead, Error> {
        let ReadReview {
            request, observed, ..
        } = self.review;
        let current = request.observe(parent_path(&request), canceled)?;
        if !observed.same_provenance(&current) {
            return Err(rejected());
        }
        if canceled.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        let runtime = PrivateRuntime::create(parent_path(&request)).map_err(|error| match error
            .retained_path()
        {
            Some(path) => retained_error(rejected(), path),
            None => rejected(),
        })?;
        let outcome = (|| {
            runtime.verify()?;
            let manifest = request.observe(runtime.path(), canceled)?;
            if !observed.same_workspace(&manifest) {
                return Err(rejected());
            }
            runtime.verify()?;
            Ok(AdmittedRead {
                request,
                manifest,
                runtime: None,
            })
        })();
        match outcome {
            Ok(mut admitted) => {
                admitted.runtime = Some(Arc::new(ReadRuntime(runtime)));
                Ok(admitted)
            }
            Err(error) => {
                let mut runtime = runtime;
                let path = runtime.path().to_owned();
                if matches!(error, Error::Shutdown(_)) {
                    std::mem::forget(runtime);
                    return Err(retained_error(error, &path));
                }
                let outcome = runtime.cleanup_empty();
                if matches!(
                    outcome,
                    Ok(private_runtime::CleanupOutcome::Removed
                        | private_runtime::CleanupOutcome::AlreadyRemoved)
                ) {
                    Err(error)
                } else {
                    Err(retained_error(error, &path))
                }
            }
        }
    }
}
impl AdmittedRead {
    pub(super) fn verify_launch(&self, config: &LaunchConfig) -> Result<(), Error> {
        let expected = self.config();
        let vault_matches = match (&config.vault, &expected.vault) {
            (Vault::Path(a), Vault::Path(b)) => a.as_os_str() == b.as_os_str(),
            (Vault::Registered(a), Vault::Registered(b)) => a == b,
            _ => false,
        };
        if !vault_matches
            || config.port != expected.port
            || config.executable != expected.executable
            || config.cache != expected.cache
            || config.config != expected.config
            || config.startup_timeout != expected.startup_timeout
            || config.request_timeout != expected.request_timeout
            || config.creation_fixture.is_some()
            || config.read_fixture.is_some()
        {
            return Err(rejected());
        }
        self.verify()
    }
    pub(super) fn config(&self) -> LaunchConfig {
        let mut cfg = self.request.config.clone();
        cfg.cache = Some(self.runtime.as_ref().unwrap().path().to_owned());
        cfg
    }
    pub(super) fn token(&self) -> &str {
        &self.request.token
    }
    pub(super) fn verify(&self) -> Result<(), Error> {
        self.request.verify()?;
        self.runtime.as_ref().ok_or_else(rejected)?.verify()?;
        Ok(())
    }
    pub(super) fn configure(&self, cmd: &mut Command) -> Result<(), Error> {
        self.verify()?;
        let root = self.runtime.as_ref().ok_or_else(rejected)?.path();
        *cmd = self.request.command(root)?;
        cmd.args(["--addr", &format!("127.0.0.1:{}", self.request.config.port)]);
        Ok(())
    }
    pub(super) fn payload(&self) -> Result<Vec<u8>, Error> {
        self.request
            .protocol(
                self.runtime.as_ref().ok_or_else(rejected)?.path(),
                &self.request.nonce,
            )?
            .serve(self.manifest.revision())
            .map_err(|_| rejected())
    }
    pub(super) fn capability(&self, client: &ReadClient, timeout: Duration) -> Result<(), Error> {
        let bytes = client.bound_capability_bytes(timeout)?;
        #[derive(Deserialize)]
        #[serde(deny_unknown_fields)]
        struct Capability {
            protocol: String,
            version: u32,
            canonical_read_only: bool,
            manifest_revision: String,
            process_nonce: String,
        }
        let c: Capability = serde_json::from_slice(&bytes).map_err(|_| rejected())?;
        if c.protocol != "track-canonical-read-only"
            || c.version != 2
            || !c.canonical_read_only
            || c.manifest_revision != self.manifest.revision()
            || c.process_nonce != self.request.nonce
        {
            return Err(rejected());
        }
        Ok(())
    }
    pub(super) fn registry(&self, value: &Value) -> Result<bool, Error> {
        let (selected, path) = self.manifest.active();
        let mut paths: BTreeMap<_, _> = self.manifest.peers().into_iter().collect();
        paths.insert(selected.into(), path.into());
        let expected = super::registry_contract::ExpectedRegistry::new(selected.into(), paths)
            .map_err(Error::Config)?;
        match expected
            .observe(&serde_json::to_vec(value).map_err(|_| rejected())?)
            .map_err(Error::Config)?
        {
            super::registry_contract::RegistryObservation::Initializing => Ok(false),
            super::registry_contract::RegistryObservation::Matched { .. } => Ok(true),
        }
    }
    pub(super) fn runtime(&self) -> Option<Arc<ReadRuntime>> {
        self.runtime.clone()
    }
}
pub(super) struct ReadRuntime(PrivateRuntime);
impl ReadRuntime {
    pub(super) fn path(&self) -> &Path {
        self.0.path()
    }
    pub(super) fn verify(&self) -> std::io::Result<()> {
        self.0.verify()
    }
}
impl Drop for ReadRuntime {
    fn drop(&mut self) {
        let _ = self.0.cleanup_empty();
    }
}
pub(super) fn retained_error(error: Error, path: &Path) -> Error {
    let message = format!(
        "{error}; private runtime may be retained at {}. Do not remove unverified contents automatically.",
        path.display()
    );
    if matches!(error, Error::Shutdown(_)) {
        Error::Shutdown(message)
    } else {
        Error::Startup(message)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    fn isolated(name: &str) -> bool {
        let exact = format!("live::ordinary::tests::{name}");
        if std::env::var("GO_ORDINARY_CASE").as_deref() == Ok(exact.as_str()) {
            return false;
        }
        assert_eq!(
            std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
            Ok("1")
        );
        let binary = std::env::var_os("TRACK_NATIVE_TEST_BINARY")
            .expect("explicit actual candidate Go binary");
        let status = Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                &exact,
                "--ignored",
                "--test-threads=1",
                "--nocapture",
            ])
            .env_clear()
            .env("PATH", "/usr/bin:/bin")
            .env("TMPDIR", std::env::temp_dir())
            .env("GO_ORDINARY_CASE", exact.clone())
            .env("GO_ORDINARY_BINARY", binary)
            .status()
            .unwrap();
        assert!(
            status.success(),
            "isolated ordinary read case failed: {name}"
        );
        true
    }
    fn fixture() -> (admission_process_tests::Fixture, LaunchConfig) {
        let binary = PathBuf::from(std::env::var_os("GO_ORDINARY_BINARY").unwrap());
        let (f, proof) = admission_process_tests::fixture(&binary);
        std::fs::remove_file(f.0.join(".track-native-read-fixture")).unwrap();
        // SAFETY: this test runs alone in a fresh subprocess, before workers.
        unsafe {
            std::env::set_var("HOME", f.0.join("home"));
            std::env::set_var("PWD", f.0.join("cwd"));
            std::env::set_var("XDG_CONFIG_HOME", f.0.join("home"));
            std::env::set_var("XDG_CACHE_HOME", f.0.join("xdg"));
            std::env::set_var("TRACK_CONFIG", f.0.join("config"));
        }
        std::env::set_current_dir(f.0.join("cwd")).unwrap();
        let cfg = proof.launch();
        (f, cfg)
    }
    fn review(cfg: LaunchConfig) -> ReadReview {
        ReadRequest::capture(cfg)
            .unwrap()
            .review(&AtomicBool::new(false))
            .unwrap()
    }
    fn confirm(mut review: ReadReview) -> ReadGrant {
        let mut dialog = review.dialog();
        assert!(dialog.apply(
            &dialog.stamp(),
            crate::workspace_read_request::Event::ExplicitConfirm
        ));
        review.authorize(&dialog).unwrap()
    }
    fn start(grant: ReadGrant) -> Session {
        let owner = LifecycleHandle::default();
        owner.begin_start().unwrap();
        owner.start_read(grant).unwrap()
    }
    fn wait(mut condition: impl FnMut() -> bool) {
        let end = Instant::now() + Duration::from_secs(20);
        while !condition() {
            assert!(Instant::now() < end, "ordinary fixture condition timed out");
            thread::sleep(Duration::from_millis(20));
        }
    }

    #[test]
    #[ignore = "explicit Go CLI and synthetic-only opt-in"]
    fn ordinary_review_requires_current_explicit_confirmation() {
        if isolated("ordinary_review_requires_current_explicit_confirmation") {
            return;
        }
        let (f, cfg) = fixture();
        let before = admission_process_tests::protected_inventory(&f.0);
        let mut pending = review(cfg.clone());
        let dialog = pending.dialog();
        assert!(pending.authorize(&dialog).is_err());
        let mut denied = review(cfg.clone());
        let mut dialog = denied.dialog();
        dialog.apply(
            &dialog.stamp(),
            crate::workspace_read_request::Event::Cancel,
        );
        assert!(denied.authorize(&dialog).is_err());
        let mut first = review(cfg.clone());
        let _ = first.dialog();
        let mut foreign = RequestDialog::new(first.presentation.clone());
        foreign.apply(
            &foreign.stamp(),
            crate::workspace_read_request::Event::ExplicitConfirm,
        );
        assert!(first.authorize(&foreign).is_err());
        let mut replaced = review(cfg);
        let mut dialog = replaced.dialog();
        let old = dialog.stamp();
        dialog.replace(dialog.request().clone()).unwrap();
        assert!(!dialog.apply(&old, crate::workspace_read_request::Event::ExplicitConfirm));
        dialog.apply(
            &dialog.stamp(),
            crate::workspace_read_request::Event::ExplicitConfirm,
        );
        assert!(replaced.authorize(&dialog).is_err());
        assert_eq!(before, admission_process_tests::protected_inventory(&f.0));
        assert_eq!(std::fs::read_dir(f.0.join("private")).unwrap().count(), 0);
    }
    #[test]
    #[ignore = "explicit Go CLI and synthetic-only opt-in"]
    fn ordinary_changed_config_context_executable_and_overlap_never_admit() {
        if isolated("ordinary_changed_config_context_executable_and_overlap_never_admit") {
            return;
        }
        let (f, mut cfg) = fixture();
        let grant = confirm(review(cfg.clone()));
        std::fs::write(f.0.join("config"), b"{}\n").unwrap();
        let before = admission_process_tests::protected_inventory(&f.0);
        assert!(grant.prepare(&AtomicBool::new(false)).is_err());
        assert_eq!(before, admission_process_tests::protected_inventory(&f.0));
        // Restore generated configuration as an explicit fixture setup.
        let paths = BTreeMap::from([
            ("main".into(), f.0.join("home/main")),
            ("work".into(), f.0.join("home/work")),
        ]);
        std::fs::write(
            f.0.join("config"),
            read_inputs::SyntheticCanonical::config_bytes(&paths, &f.0.join("home/normal")),
        )
        .unwrap();
        let grant = confirm(review(cfg.clone()));
        // SAFETY: isolated process; no serving session or background reader exists.
        unsafe {
            std::env::set_var("TRACK_CONTEXT_CHANGED", "1");
        }
        assert!(grant.prepare(&AtomicBool::new(false)).is_err());
        // SAFETY: the same isolated exact-test process still has no running workers.
        unsafe {
            std::env::remove_var("TRACK_CONTEXT_CHANGED");
        }
        let copy = f.0.join("candidate-engine");
        std::fs::copy(&cfg.executable, &copy).unwrap();
        std::fs::set_permissions(&copy, std::fs::Permissions::from_mode(0o700)).unwrap();
        cfg.executable = copy.clone();
        let grant = confirm(review(cfg.clone()));
        use std::io::Write;
        std::fs::OpenOptions::new()
            .append(true)
            .open(copy)
            .unwrap()
            .write_all(b"changed")
            .unwrap();
        assert!(grant.prepare(&AtomicBool::new(false)).is_err());
        cfg.cache = Some(f.0.join("home/work"));
        std::fs::set_permissions(
            cfg.cache.as_ref().unwrap(),
            std::fs::Permissions::from_mode(0o700),
        )
        .unwrap();
        assert!(
            ReadRequest::capture(cfg)
                .unwrap()
                .review(&AtomicBool::new(false))
                .is_err()
        );
        assert_eq!(std::fs::read_dir(f.0.join("private")).unwrap().count(), 0);
    }
    #[test]
    #[ignore = "explicit Go CLI and synthetic-only opt-in"]
    fn ordinary_owned_read_is_finite_partial_and_sticky_invalidated() {
        if isolated("ordinary_owned_read_is_finite_partial_and_sticky_invalidated") {
            return;
        }
        let (f, cfg) = fixture();
        let work = f.0.join("home/work");
        std::fs::write(
            work.join("note/42 (conflicted copy).md"),
            b"not the original",
        )
        .unwrap();
        std::fs::write(work.join("note/99.md"), b"bad metadata body").unwrap();
        std::fs::write(work.join(".track/notes/99.yaml"), b"version: 1\ntitle: [\n").unwrap();
        let before = admission_process_tests::protected_inventory(&f.0);
        let session = start(confirm(review(cfg)));
        let client = session.client();
        assert!(session.finite_only());
        assert!(session.events().unavailable);
        assert!(client.follow().is_err());
        assert!(!client.creation_available());
        assert_eq!(session.initial_vaults["active"]["name"], "work");
        wait(|| {
            client
                .note(&NoteKey {
                    vault: "work".into(),
                    note_id: 42,
                })
                .is_ok()
        });
        let invalid_path = work.join("note/99.md").display().to_string();
        let has_file_issue = |fields: &std::collections::BTreeMap<String, Value>| {
            fields
                .get("read_state")
                .and_then(|state| state.get("issues"))
                .and_then(Value::as_array)
                .is_some_and(|issues| issues.iter().any(|issue| issue["path"] == invalid_path))
        };
        wait(|| {
            client
                .notes("work")
                .is_ok_and(|result| has_file_issue(&result.diagnostics))
        });
        let source = client
            .note(&NoteKey {
                vault: "work".into(),
                note_id: 42,
            })
            .unwrap();
        assert_eq!(source.note.body, "Foreign sentinel body");
        assert!(has_file_issue(&source.related_and_diagnostics));
        assert!(matches!(
            client.save(&SaveRequest {
                key: source.note.note.key(),
                body: "not saved".into(),
                etag: source.note.etag,
                launch_generation: client.launch_generation,
                storage_generation: session.initial_storage.as_ref().unwrap().generation
            }),
            Err(Error::SaveNotSent(_))
        ));
        let cache = session.runtime_cache.clone().unwrap();
        assert!(cache.starts_with(f.0.join("private")));
        assert_ne!(cache, f.0.join("private"));
        assert_eq!(before, admission_process_tests::protected_inventory(&f.0));
        // Trigger drift via a custom render transport, not only generic GET.
        let original = std::fs::read(f.0.join("config")).unwrap();
        let mut changed = original.clone();
        changed.push(b'\n');
        std::fs::write(f.0.join("config"), changed).unwrap();
        assert!(matches!(
            client.included_notes("work", "body"),
            Err(Error::Stale)
        ));
        assert!(!client.is_alive());
        std::fs::write(f.0.join("config"), original).unwrap();
        assert!(matches!(client.notes("work"), Err(Error::Stale)));
        session.shutdown().unwrap();
        assert!(cache.exists());
        assert!(std::fs::read_dir(cache).unwrap().count() > 0);
        let mut expected = before;
        let mut after = admission_process_tests::protected_inventory(&f.0);
        expected.remove(Path::new("config"));
        after.remove(Path::new("config"));
        assert_eq!(expected, after);
    }
    #[test]
    #[ignore = "explicit Go CLI and synthetic-only opt-in"]
    fn ordinary_cancelled_review_reaps_capture_without_cache_admission() {
        if isolated("ordinary_cancelled_review_reaps_capture_without_cache_admission") {
            return;
        }
        let (f, mut cfg) = fixture();
        let fake = f.0.join("slow-engine");
        std::fs::write(
            &fake,
            b"#!/bin/sh\nprintf '%s' $$ > \"$TMPDIR/capture-pid\"\nexec /bin/sleep 30\n",
        )
        .unwrap();
        std::fs::set_permissions(&fake, std::fs::Permissions::from_mode(0o700)).unwrap();
        cfg.executable = fake;
        let request = ReadRequest::capture(cfg).unwrap();
        let owner = Arc::new(LifecycleHandle::default());
        owner.begin_start().unwrap();
        let worker = owner.clone();
        let task = thread::spawn(move || worker.review_read(request));
        wait(|| f.0.join("private/capture-pid").exists());
        let pid: i32 = std::fs::read_to_string(f.0.join("private/capture-pid"))
            .unwrap()
            .parse()
            .unwrap();
        owner.cancel_startup();
        owner.shutdown().unwrap();
        assert!(matches!(task.join().unwrap(), Err(Error::Stale)));
        // SAFETY: signal 0 observes the known fixture PID and sends no signal.
        assert_eq!(unsafe { libc::kill(pid, 0) }, -1);
        assert_eq!(std::fs::read_dir(f.0.join("private")).unwrap().count(), 1);
    }
    #[test]
    #[ignore = "explicit Go CLI and synthetic-only opt-in"]
    fn ordinary_unavailable_peer_does_not_become_active_or_invalidate_healthy_reads() {
        if isolated("ordinary_unavailable_peer_does_not_become_active_or_invalidate_healthy_reads")
        {
            return;
        }
        let (f, mut cfg) = fixture();
        cfg.vault = Vault::Registered("main".into());
        let before = admission_process_tests::protected_inventory(&f.0);
        let proof = confirm(review(cfg))
            .prepare(&AtomicBool::new(false))
            .unwrap();
        let db = PathBuf::from(proof.manifest.private_db("work").unwrap());
        std::fs::create_dir_all(db.parent().unwrap()).unwrap();
        std::fs::write(db, b"not SQLite").unwrap();
        let config = proof.config();
        let server = OwnedServer::start_admitted(
            config,
            &AtomicBool::new(false),
            StartupAdmission::Ordinary(Box::new(proof)),
        )
        .unwrap();
        let session = Session::from_owned(server);
        let client = session.client();
        wait(|| {
            client.vaults().ok().is_some_and(|v| {
                v["unavailable"]
                    .as_array()
                    .is_some_and(|rows| rows.iter().any(|v| v["name"] == "work"))
            })
        });
        wait(|| {
            client
                .note(&NoteKey {
                    vault: "main".into(),
                    note_id: 42,
                })
                .is_ok()
        });
        assert_eq!(
            client
                .note(&NoteKey {
                    vault: "main".into(),
                    note_id: 42
                })
                .unwrap()
                .note
                .body,
            "Active sentinel body"
        );
        assert!(
            client
                .note(&NoteKey {
                    vault: "work".into(),
                    note_id: 42
                })
                .is_err()
        );
        assert!(client.is_alive());
        assert_eq!(client.vaults().unwrap()["active"]["name"], "main");
        session.shutdown().unwrap();
        assert_eq!(before, admission_process_tests::protected_inventory(&f.0));
    }
    #[test]
    fn every_read_transport_distinguishes_bound_invalidation_from_local_unavailability() {
        const SPEC: &str = r#"{"version":2,"mark":"line","data":{"kind":"metric","records":[{"version":1,"name":"score","time":"A","value":0}]},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}"#;
        for family in 0..6 {
            for invalid in [false, true] {
                let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
                let origin = format!("http://{}", listener.local_addr().unwrap());
                let worker = thread::spawn(move || {
                    let (mut socket, _) = listener.accept().unwrap();
                    socket
                        .set_read_timeout(Some(Duration::from_secs(3)))
                        .unwrap();
                    let mut request = Vec::new();
                    let mut byte = [0];
                    while !request.ends_with(b"\r\n\r\n") {
                        socket.read_exact(&mut byte).unwrap();
                        request.push(byte[0]);
                    }
                    let headers = String::from_utf8(request).unwrap();
                    let length = headers
                        .lines()
                        .find_map(|line| {
                            line.split_once(':')
                                .filter(|(name, _)| name.eq_ignore_ascii_case("content-length"))
                                .map(|(_, value)| value.trim().parse::<usize>().unwrap())
                        })
                        .unwrap_or(0);
                    let mut body = vec![0; length];
                    socket.read_exact(&mut body).unwrap();
                    use std::io::Write;
                    let header = if invalid {
                        "X-Track-Bound-Invalid: 1\r\n"
                    } else {
                        ""
                    };
                    write!(socket,"HTTP/1.1 503 Unavailable\r\n{header}Content-Type: application/json\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{{}}").unwrap();
                });
                let client = ReadClient {
                    finite_only: true,
                    canonical_read_only: true,
                    creation_store: Default::default(),
                    creation_fixture: None,
                    http: reqwest::blocking::Client::builder()
                        .no_proxy()
                        .build()
                        .unwrap(),
                    origin,
                    alive: Arc::new(AtomicBool::new(true)),
                    launch_generation: 1,
                };
                match family {
                    0 => {
                        assert!(client.notes("").is_err());
                    }
                    1 => {
                        assert!(client.included_notes("", "body").is_err());
                    }
                    2 => {
                        assert!(client.raster_asset("assets/a.png", "").is_err());
                    }
                    3 => {
                        assert!(client.pdf_asset("assets/a.pdf", "").is_err());
                    }
                    4 => {
                        assert!(client.inline_viewspec("", SPEC).is_err());
                    }
                    _ => {
                        let cap = crate::markdown_handoff::Capability::parse(
                            &format!("/markdown#handoff={}", "a".repeat(64)),
                            &client.origin,
                            1,
                        )
                        .unwrap();
                        assert!(client.consume_markdown(cap).is_err());
                    }
                }
                worker.join().unwrap();
                assert_eq!(
                    client.is_alive(),
                    !invalid,
                    "family {family}, invalid {invalid}"
                );
            }
        }
    }
    #[test]
    #[ignore = "explicit Go CLI and synthetic-only opt-in"]
    fn ordinary_wrong_preflight_root_or_identity_rejects_before_mkdir() {
        if isolated("ordinary_wrong_preflight_root_or_identity_rejects_before_mkdir") {
            return;
        }
        let (f, cfg) = fixture();
        for field in ["private_root", "private_identity"] {
            let mut request = ReadRequest::capture(cfg.clone()).unwrap();
            let mut command = request.command(parent_path(&request)).unwrap();
            command.stdout(Stdio::piped()).stderr(Stdio::null());
            let payload = request
                .protocol(parent_path(&request), &request.nonce)
                .unwrap()
                .preflight()
                .unwrap();
            let raw = super::super::capture::capture_exchange(
                &mut command,
                Duration::from_secs(20),
                &AtomicBool::new(false),
                Some(payload),
            )
            .unwrap();
            let mut value: Value = serde_json::from_slice(&raw).unwrap();
            value[field] = if field == "private_root" {
                serde_json::json!("/incorrect-private-root")
            } else {
                serde_json::json!("incorrect-identity")
            };
            std::fs::write(
                f.0.join("preflight-answer"),
                serde_json::to_vec(&value).unwrap(),
            )
            .unwrap();
            let fake = f.0.join("incorrect-engine");
            std::fs::write(
                &fake,
                b"#!/bin/sh\ncat >/dev/null\ncat \"$TMPDIR/../preflight-answer\"\n",
            )
            .unwrap();
            std::fs::set_permissions(&fake, std::fs::Permissions::from_mode(0o700)).unwrap();
            request.config.executable = fake.clone();
            request.executable = Executable::capture(&fake).unwrap();
            let before = admission_process_tests::protected_inventory(&f.0);
            assert!(matches!(
                request.review(&AtomicBool::new(false)),
                Err(Error::Config(_))
            ));
            assert_eq!(before, admission_process_tests::protected_inventory(&f.0));
            assert_eq!(std::fs::read_dir(f.0.join("private")).unwrap().count(), 0);
        }
    }
    #[test]
    #[ignore = "explicit Go CLI and synthetic-only opt-in"]
    fn ordinary_alias_pwd_tilde_and_explicit_config_keep_actual_resolution_context() {
        if isolated("ordinary_alias_pwd_tilde_and_explicit_config_keep_actual_resolution_context") {
            return;
        }
        let (f, mut cfg) = fixture();
        let alias = f.0.join("home/work-alias");
        std::os::unix::fs::symlink(f.0.join("home/work"), &alias).unwrap();
        let cwd_alias = f.0.join("cwd-alias");
        std::os::unix::fs::symlink(f.0.join("cwd"), &cwd_alias).unwrap();
        std::fs::write(f.0.join("ignored-config"), b"{}\n").unwrap();
        std::fs::write(
            f.0.join("config"),
            b"default_vault: main\ncache_dir: ~/normal\nvaults:\n  main: ~/main\n  work: ~/work\n",
        )
        .unwrap();
        // SAFETY: isolated exact-test process; no worker/session is running yet.
        unsafe {
            std::env::set_var("PWD", &cwd_alias);
            std::env::set_var("TRACK_CONFIG", f.0.join("ignored-config"));
            std::env::set_var("XDG_CONFIG_HOME", "");
        }
        cfg.vault = Vault::Path(alias.clone());
        let before = admission_process_tests::protected_inventory(&f.0);
        let session = start(confirm(review(cfg)));
        let client = session.client();
        assert_eq!(session.initial_vaults["active"]["name"], "work");
        assert_eq!(
            session.initial_vaults["active"]["path"],
            alias.to_str().unwrap()
        );
        wait(|| {
            client
                .note(&NoteKey {
                    vault: "work".into(),
                    note_id: 42,
                })
                .is_ok()
        });
        assert_eq!(
            client
                .note(&NoteKey {
                    vault: "work".into(),
                    note_id: 42
                })
                .unwrap()
                .note
                .body,
            "Foreign sentinel body"
        );
        session.shutdown().unwrap();
        assert_eq!(before, admission_process_tests::protected_inventory(&f.0));
    }
}
