//! Test-only v2 integration. Never an ordinary launch proof or UI route.
//! The independently generated fixture remains the authority; the manifest is a
//! bounded observation correlated with a separately owned serving process.
use super::capture::{capture_bytes, capture_exchange};
use super::*;
use std::os::unix::process::CommandExt;

/// Exercise real reader/input callbacks after the normal synthetic v2 admission.
#[cfg(feature = "ui-tests")]
pub(crate) fn with_keyboard_fixture(run: impl FnOnce(Session)) {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY")
            .expect("explicit test-only Go bridge binary"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    for id in [42, 43] {
        let root = fixture.0.join("home/work");
        let body = (0..80)
            .map(|n| format!("Keyboard fixture {id}, paragraph {n}.\n\n"))
            .collect::<String>();
        std::fs::write(root.join(format!("note/{id}.md")), body).unwrap();
        std::fs::write(
            root.join(format!(".track/notes/{id}.yaml")),
            format!(
                "version: 1\ntitle: {}\n",
                if id == 43 {
                    "DestinationOnly".into()
                } else {
                    format!("Keyboard fixture {id}")
                }
            ),
        )
        .unwrap();
    }
    let before = admission_process_tests::protected_inventory(&fixture.0);
    let bound = SyntheticBound::new(proof, fixture.0.clone()).unwrap();
    let owner = OwnedServer::start_admitted(
        bound.proof.launch(),
        &AtomicBool::new(false),
        StartupAdmission::SyntheticBound(bound),
    )
    .unwrap();
    let session = Session::from_owned(owner);
    run(session.clone());
    session.shutdown().unwrap();
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0),
        "virtual keyboard interactions must not alter canonical files or configuration"
    );
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Manifest {
    version: u32,
    selector: Selector,
    private_root: PathBuf,
    private_mode: u32,
    private_identity: String,
    context: ResolutionContext,
    active: ManifestVault,
    peers: Vec<ManifestVault>,
    configurations: Vec<Provenance>,
    revision: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Selector {
    name: String,
    path: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ResolutionContext {
    home: PathBuf,
    cwd: PathBuf,
    xdg_cache_home: PathBuf,
    xdg_config_home: PathBuf,
    environment_revision: String,
}
#[derive(Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
struct ManifestVault {
    name: String,
    display_path: PathBuf,
    canonical_path: PathBuf,
    root_identity: String,
    ordinary_db: PathBuf,
    resolved_ordinary_db: PathBuf,
    ordinary_anchor: PathBuf,
    ordinary_identity: String,
    private_db: PathBuf,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Provenance {
    path: PathBuf,
    canonical_path: PathBuf,
    present: bool,
    identity: String,
    size: i64,
    mode: u32,
    modified_ns: i64,
    sha256: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Capability {
    protocol: String,
    version: u32,
    canonical_read_only: bool,
    manifest_revision: String,
    process_nonce: String,
}
fn digest(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn identity(value: &str) -> bool {
    !value.is_empty() && value.len() <= 256 && !value.chars().any(char::is_control)
}
fn cache_path(path: &std::path::Path, root: &std::path::Path) -> bool {
    let Some(file) = path.file_name().and_then(|v| v.to_str()) else {
        return false;
    };
    let Some(version) = file
        .strip_prefix("index-v")
        .and_then(|v| v.strip_suffix(".db"))
    else {
        return false;
    };
    let Some(key) = path
        .parent()
        .and_then(|p| p.file_name())
        .and_then(|v| v.to_str())
    else {
        return false;
    };
    !version.is_empty()
        && version.bytes().all(|b| b.is_ascii_digit())
        && key.len() == 16
        && key
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
        && path.as_os_str() == root.join(key).join(file).as_os_str()
}
fn invalid() -> Error {
    Error::Shape("Invalid synthetic bound-startup contract".into())
}

pub(super) struct SyntheticBound {
    pub(super) proof: read_inputs::SyntheticCanonical,
    root: PathBuf,
    nonce: String,
    revision: Option<String>,
    serve_request: Option<Vec<u8>>,
    transport: SyntheticTransport,
}
enum SyntheticTransport {
    TestBridge,
    InternalCli,
}
impl SyntheticBound {
    fn new(proof: read_inputs::SyntheticCanonical, root: PathBuf) -> Result<Self, Error> {
        let mut entropy = [0; 32];
        std::fs::File::open("/dev/urandom")?.read_exact(&mut entropy)?;
        let result = Self {
            proof,
            root,
            nonce: entropy.iter().map(|b| format!("{b:02x}")).collect(),
            revision: None,
            serve_request: None,
            transport: SyntheticTransport::TestBridge,
        };
        result.verify()?;
        Ok(result)
    }
    pub(super) fn verify(&self) -> Result<(), Error> {
        self.proof.verify().map_err(Error::Config)?;
        let launch = self.proof.launch();
        if launch.config != Some(self.root.join("config"))
            || launch.cache != Some(self.root.join("private"))
            || !matches!(launch.vault, Vault::Registered(ref name) if name == "work")
        {
            return Err(invalid());
        }
        Ok(())
    }
    pub(super) fn configure(&self, command: &mut Command) -> Result<(), Error> {
        self.verify()?;
        let mut source = Command::new(command.get_program());
        self.proof.configure(&mut source).map_err(Error::Config)?;
        command
            .env_clear()
            .current_dir(source.get_current_dir().ok_or_else(invalid)?);
        for (key, value) in source.get_envs() {
            if let Some(value) = value {
                command.env(key, value);
            }
        }
        let addr = format!("127.0.0.1:{}", self.proof.launch().port);
        match self.transport {
            SyntheticTransport::TestBridge => {
                command
                    .arg("-test.run=^TestNativeBoundFixtureBridge$")
                    .env("GO_TRACK_BOUND_MODE", "serve")
                    .env("GO_TRACK_BOUND_ROOT", &self.root)
                    .env("GO_TRACK_BOUND_SELECTED", "work")
                    .env("GO_TRACK_BOUND_ADDR", addr);
            }
            SyntheticTransport::InternalCli => {
                command.args(["__native-read", "--addr", &addr]);
            }
        }
        Ok(())
    }
    pub(super) fn capture(
        &mut self,
        command: &mut Command,
        timeout: Duration,
        canceled: &AtomicBool,
    ) -> Result<(), Error> {
        // This command has already been env-cleared by configure and received
        // the final owned parent/token values. Capture both phases from these
        // supplied values, not from the child's self-observed environment.
        let environment = command
            .get_envs()
            .filter_map(|(key, value)| value.map(|value| (key.to_owned(), value.to_owned())))
            .collect::<Vec<_>>();
        let context = launch_context::ResolutionContextRequest::capture(
            command.get_current_dir().ok_or_else(invalid)?,
            &environment,
        )
        .map_err(|_| invalid())?;
        let protocol = launch_protocol::BoundLaunchRequest::new(
            context,
            workspace_provenance::RequestedSelector::Registered("work".into()),
            self.root
                .join("private")
                .to_str()
                .ok_or_else(invalid)?
                .to_owned(),
            self.nonce.clone(),
        )
        .map_err(|_| invalid())?;
        let preflight = protocol.preflight().map_err(|_| invalid())?;
        let mut capture = Command::new(command.get_program());
        capture
            .env_clear()
            .current_dir(command.get_current_dir().ok_or_else(invalid)?);
        for (key, value) in command.get_envs() {
            if let Some(value) = value {
                capture.env(key, value);
            }
        }
        match self.transport {
            SyntheticTransport::TestBridge => {
                capture
                    .args(command.get_args())
                    .env("GO_TRACK_BOUND_MODE", "capture");
            }
            SyntheticTransport::InternalCli => {
                capture.arg("__native-read");
            }
        }
        capture
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .process_group(0);
        let bytes = capture_exchange(
            &mut capture,
            timeout.min(Duration::from_secs(10)),
            canceled,
            Some(preflight),
        )?;
        let text = std::str::from_utf8(&bytes).map_err(|_| invalid())?;
        let raw = match self.transport {
            SyntheticTransport::TestBridge => {
                let mut lines = text
                    .lines()
                    .filter_map(|line| line.strip_prefix("TRACK_BOUND_MANIFEST:"));
                let raw = lines.next().ok_or_else(invalid)?;
                if lines.next().is_some() {
                    return Err(invalid());
                }
                raw
            }
            SyntheticTransport::InternalCli => text.trim_end(),
        };
        // Exercise the inactive production-shaped decoder against actual Go
        // output. Scope comes from the independent generated fixture, never
        // from the response being compared. This adds no launch authority;
        // the original fixture proof and stricter checks below still apply.
        let observation = workspace_provenance::LaunchManifestObservation::parse(raw.as_bytes())
            .map_err(|_| invalid())?;
        let requested = workspace_provenance::WorkspaceScopeRequest::new(
            workspace_provenance::RequestedSelector::Registered("work".into()),
            workspace_provenance::PeerScopeRequest::ExplicitRequestedSet(
                ["main", "work"]
                    .into_iter()
                    .map(|name| {
                        (
                            name.to_owned(),
                            self.root
                                .join("home")
                                .join(name)
                                .to_str()
                                .unwrap()
                                .to_owned(),
                        )
                    })
                    .collect(),
            ),
        )
        .map_err(|_| invalid())?;
        observation
            .check_requested_scope(&requested)
            .map_err(|_| invalid())?;
        let manifest: Manifest = serde_json::from_str(raw).map_err(|_| invalid())?;
        self.validate_manifest(&manifest)?;
        self.proof.verify_bindings().map_err(Error::Config)?;
        self.serve_request = Some(protocol.serve(&manifest.revision).map_err(|_| invalid())?);
        self.revision = Some(manifest.revision);
        Ok(())
    }
    pub(super) fn take_serve_request(&mut self) -> Result<Vec<u8>, Error> {
        self.serve_request.take().ok_or_else(invalid)
    }
    fn validate_manifest(&self, m: &Manifest) -> Result<(), Error> {
        if m.version != 1
            || !digest(&m.revision)
            || m.selector.name != "work"
            || !m.selector.path.is_empty()
            || m.private_root != self.root.join("private")
            || m.private_mode != ((1 << 31) | 0o700)
            || !identity(&m.private_identity)
            || m.context.home != self.root.join("home")
            || m.context.cwd != self.root.join("cwd")
            || m.context.xdg_cache_home != self.root.join("xdg")
            || m.context.xdg_config_home != self.root.join("private")
            || !digest(&m.context.environment_revision)
            || m.peers.len() != 2
            || m.configurations.len() != 3
        {
            return Err(invalid());
        }
        let mut names = std::collections::BTreeSet::new();
        for v in &m.peers {
            if !cache_path(&v.ordinary_db, &self.root.join("home/normal"))
                || !cache_path(
                    &v.private_db,
                    &self.root.join("private/canonical-read-only-v1"),
                )
                || !matches!(v.name.as_str(), "main" | "work")
                || !names.insert(&v.name)
                || v.display_path != self.root.join("home").join(&v.name)
                || v.canonical_path != v.display_path
                || !identity(&v.root_identity)
                || !identity(&v.ordinary_identity)
                || v.ordinary_db != v.resolved_ordinary_db
                || v.ordinary_anchor != self.root.join("home/normal")
                || v.ordinary_db.parent().and_then(|p| p.parent())
                    != Some(self.root.join("home/normal").as_path())
                || v.private_db.parent().and_then(|p| p.parent())
                    != Some(self.root.join("private/canonical-read-only-v1").as_path())
                || v.ordinary_db.file_name() != v.private_db.file_name()
                || v.ordinary_db.parent().and_then(|p| p.file_name())
                    != v.private_db.parent().and_then(|p| p.file_name())
            {
                return Err(invalid());
            }
        }
        if m.peers.iter().find(|v| v.name == "work") != Some(&m.active) {
            return Err(invalid());
        }
        let expected = std::collections::BTreeSet::from([
            self.root.join("config"),
            self.root.join("home/main/.track/config.yml"),
            self.root.join("home/work/.track/config.yml"),
        ]);
        let mut seen = std::collections::BTreeSet::new();
        for p in &m.configurations {
            if !expected.contains(&p.path)
                || !seen.insert(p.path.clone())
                || p.canonical_path != p.path
                || !p.present
                || !identity(&p.identity)
                || !(0..=1024 * 1024).contains(&p.size)
                || p.mode & !0o777 != 0
                || p.modified_ns <= 0
                || !digest(&p.sha256)
            {
                return Err(invalid());
            }
        }
        Ok(())
    }
    pub(super) fn capability(&self, client: &ReadClient, timeout: Duration) -> Result<(), Error> {
        if !client.is_alive() {
            return Err(Error::Stale);
        }
        let mut response = client
            .http
            .get(format!("{}/api/capabilities", client.origin))
            .header("Cache-Control", "no-cache, no-store")
            .timeout(timeout)
            .send()
            .map_err(|e| Error::Transport(e.to_string()))?;
        if response.status().as_u16() != 200
            || !response
                .headers()
                .get(reqwest::header::CONTENT_TYPE)
                .and_then(|v| v.to_str().ok())
                .and_then(|v| v.split(';').next())
                .is_some_and(|v| v.trim().eq_ignore_ascii_case("application/json"))
        {
            return Err(invalid());
        }
        let mut bytes = Vec::new();
        (&mut response).take(4097).read_to_end(&mut bytes)?;
        if bytes.len() > 4096 {
            return Err(Error::Oversized);
        }
        if !client.is_alive() {
            return Err(Error::Stale);
        }
        self.validate_capability(&bytes)
    }
    fn validate_capability(&self, bytes: &[u8]) -> Result<(), Error> {
        let c: Capability = serde_json::from_slice(bytes).map_err(|_| invalid())?;
        if c.protocol != "track-canonical-read-only"
            || c.version != 2
            || !c.canonical_read_only
            || self.revision.as_ref() != Some(&c.manifest_revision)
            || c.process_nonce != self.nonce
        {
            return Err(invalid());
        }
        Ok(())
    }
    pub(super) fn registry(&self, value: &Value) -> Result<bool, Error> {
        self.proof.verify_registry(value).map_err(Error::Config)
    }
}

#[test]
fn bound_capability_requires_exact_version_nonce_and_revision() {
    let (fixture, proof) = admission_process_tests::fixture(&std::env::current_exe().unwrap());
    let mut bound = SyntheticBound::new(proof, fixture.0.clone()).unwrap();
    bound.revision = Some("a".repeat(64));
    let good = serde_json::json!({"protocol":"track-canonical-read-only","version":2,
        "canonical_read_only":true,"manifest_revision":"a".repeat(64),"process_nonce":bound.nonce});
    assert!(
        bound
            .validate_capability(&serde_json::to_vec(&good).unwrap())
            .is_ok()
    );
    for (key, value) in [
        ("version", serde_json::json!(1)),
        ("canonical_read_only", serde_json::json!(false)),
        ("manifest_revision", serde_json::json!("b".repeat(64))),
        ("process_nonce", serde_json::json!("other")),
        ("protocol", serde_json::json!("other")),
        ("unknown", serde_json::json!(true)),
    ] {
        let mut bad = good.clone();
        bad[key] = value;
        assert!(
            bound
                .validate_capability(&serde_json::to_vec(&bad).unwrap())
                .is_err()
        );
    }
    let raw = serde_json::to_string(&good).unwrap();
    let duplicate = format!("{{\"version\":2,{}", &raw[1..]);
    assert!(bound.validate_capability(duplicate.as_bytes()).is_err());
    assert!(OwnedServer::start(bound.proof.launch()).is_err());
}

#[test]
fn capture_is_bounded_and_cancelable() {
    let run = |script: &str, timeout, canceled: &AtomicBool| {
        let mut command = Command::new("/bin/sh");
        command
            .args(["-c", script])
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .process_group(0);
        capture_bytes(&mut command, timeout, canceled)
    };
    assert_eq!(
        run(
            "printf marker",
            Duration::from_secs(2),
            &AtomicBool::new(false)
        )
        .unwrap(),
        b"marker"
    );
    assert!(matches!(
        run(
            "exec sleep 3",
            Duration::from_millis(20),
            &AtomicBool::new(false)
        ),
        Err(Error::Startup(_))
    ));
    assert!(matches!(
        run("exit 0", Duration::from_secs(2), &AtomicBool::new(true)),
        Err(Error::Stale)
    ));
    assert!(matches!(
        run(
            "head -c 131073 /dev/zero",
            Duration::from_secs(2),
            &AtomicBool::new(false)
        ),
        Err(Error::Oversized)
    ));
}

#[test]
fn capture_duplex_request_uses_one_bounded_exchange() {
    let run = |script: &str, timeout, canceled: &AtomicBool| {
        let mut command = Command::new("/bin/sh");
        command
            .args(["-c", script])
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .process_group(0);
        capture_exchange(
            &mut command,
            timeout,
            canceled,
            Some(vec![b'x'; 192 * 1024]),
        )
    };
    // The child fills stdout before consuming stdin. A blocking write_all
    // before output draining would deadlock once both pipe buffers are full.
    let bytes = run(
        "head -c 65536 /dev/zero; cat >/dev/null; printf complete",
        Duration::from_secs(3),
        &AtomicBool::new(false),
    )
    .unwrap();
    assert_eq!(bytes.len(), 65536 + 8);
    assert!(bytes[..65536].iter().all(|byte| *byte == 0));
    assert_eq!(&bytes[65536..], b"complete");
    assert!(matches!(
        run(
            "exec sleep 3",
            Duration::from_millis(30),
            &AtomicBool::new(false)
        ),
        Err(Error::Startup(_))
    ));
    assert!(
        run(
            "printf early-exit",
            Duration::from_secs(2),
            &AtomicBool::new(false)
        )
        .is_err()
    );
    assert!(matches!(
        run("exit 0", Duration::from_secs(2), &AtomicBool::new(true)),
        Err(Error::Stale)
    ));
}

#[test]
#[ignore = "explicit test-only Go bridge binary and synthetic-only opt-in"]
fn synthetic_bound_manifest_owns_reads_and_latches_drift() {
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    bound_manifest_owns_reads_and_latches_drift(binary, SyntheticTransport::TestBridge);
}

#[test]
#[ignore = "explicit actual Go CLI binary and synthetic-only opt-in"]
fn synthetic_internal_cli_owns_reads_and_latches_drift() {
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_TEST_BINARY").expect("explicit actual Go CLI"),
    );
    bound_manifest_owns_reads_and_latches_drift(binary, SyntheticTransport::InternalCli);
}

fn bound_manifest_owns_reads_and_latches_drift(binary: PathBuf, transport: SyntheticTransport) {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    let before = admission_process_tests::protected_inventory(&fixture.0);
    let mut bound = SyntheticBound::new(proof, fixture.0.clone()).unwrap();
    bound.transport = transport;
    let mut owner = OwnedServer::start_admitted(
        bound.proof.launch(),
        &AtomicBool::new(false),
        StartupAdmission::SyntheticBound(bound),
    )
    .unwrap();
    assert_eq!(owner.initial_vaults["active"]["name"], "work");
    let client = owner.client();
    for (name, label) in [("main", "Active"), ("work", "Foreign")] {
        let key = NoteKey {
            vault: name.into(),
            note_id: 42,
        };
        let response = client.note(&key).unwrap();
        assert_eq!(response.note.body, format!("{label} sentinel body"));
        let state = client.storage().unwrap();
        assert!(matches!(
            client.save(&SaveRequest {
                key,
                body: "not saved".into(),
                etag: response.note.etag,
                launch_generation: client.launch_generation,
                storage_generation: state.generation
            }),
            Err(Error::SaveNotSent(_))
        ));
    }
    // The finite v2 client does not start the ordinary SSE subscriber.
    let events = client
        .http
        .get(format!("{}/api/events", client.origin))
        .send()
        .unwrap();
    assert_eq!(events.status().as_u16(), 403);
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
    let path = fixture.0.join("config");
    let original = std::fs::read(&path).unwrap();
    let mut changed = original.clone();
    changed.extend_from_slice(b"\n");
    std::fs::write(&path, &changed).unwrap();
    assert!(matches!(
        client.get::<Value>("/api/capabilities", &[]),
        Err(Error::Stale)
    ));
    assert!(!client.is_alive());
    std::fs::write(&path, &original).unwrap();
    assert!(matches!(
        client.get::<Value>("/api/capabilities", &[]),
        Err(Error::Stale)
    ));
    // Ignore only the test's deliberate config metadata change, not other paths.
    let mut after = admission_process_tests::protected_inventory(&fixture.0);
    let mut expected = before;
    assert_eq!(
        after[std::path::Path::new("config")].4,
        expected[std::path::Path::new("config")].4
    );
    after.remove(std::path::Path::new("config"));
    expected.remove(std::path::Path::new("config"));
    assert_eq!(after, expected);
    let before_shutdown = admission_process_tests::protected_inventory(&fixture.0);
    owner.shutdown().unwrap();
    assert!(!client.is_alive());
    assert_eq!(
        before_shutdown,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}

#[test]
fn capture_reaps_leader_and_stops_descendants_before_returning() {
    for pipe in ["", ">/dev/null 2>&1"] {
        let root = std::env::temp_dir().join(format!(
            "track-capture-child-{}-{}",
            std::process::id(),
            NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir(&root).unwrap();
        let marker = root.join("escaped");
        let ready = root.join("redirected");
        let mut command = Command::new("/bin/sh");
        command
            .args([
                "-c",
                &format!("(printf ready > \"$2\"; sleep 1; printf escaped > \"$1\") {pipe} & while [ ! -f \"$2\" ]; do sleep 0.01; done; printf marker"),
                "fixture",
            ])
            .arg(&marker)
            .arg(&ready)
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .process_group(0);
        let start = Instant::now();
        let result = capture_bytes(
            &mut command,
            Duration::from_secs(2),
            &AtomicBool::new(false),
        );
        assert!(start.elapsed() < Duration::from_secs(1));
        assert!(ready.exists(), "background redirection was not established");
        if pipe.is_empty() {
            assert!(result.is_err());
        } else {
            assert_eq!(result.unwrap(), b"marker");
        }
        thread::sleep(Duration::from_millis(1200));
        assert!(!marker.exists(), "descendant escaped owned capture cleanup");
        std::fs::remove_dir_all(root).unwrap();
    }
}

#[test]
fn bound_manifest_checks_independent_fixture_shape() {
    let (fixture, proof) = admission_process_tests::fixture(&std::env::current_exe().unwrap());
    let bound = SyntheticBound::new(proof, fixture.0.clone()).unwrap();
    let root = &fixture.0;
    let peer = |name: &str| {
        serde_json::json!({"name":name,"display_path":root.join("home").join(name),
        "canonical_path":root.join("home").join(name),"root_identity":"1:2","ordinary_identity":"1:3",
        "ordinary_db":root.join("home/normal/0123456789abcdef/index-v11.db"),
        "resolved_ordinary_db":root.join("home/normal/0123456789abcdef/index-v11.db"),
        "ordinary_anchor":root.join("home/normal"),"private_db":root.join("private/canonical-read-only-v1/0123456789abcdef/index-v11.db")})
    };
    let configs: Vec<_> = [
        "config",
        "home/main/.track/config.yml",
        "home/work/.track/config.yml",
    ]
    .into_iter()
    .map(|rel| {
        serde_json::json!({"path":root.join(rel),"canonical_path":root.join(rel),"present":true,
            "identity":"1:4","size":10,"mode":0o600,"modified_ns":1,"sha256":"a".repeat(64)})
    })
    .collect();
    let good = serde_json::json!({"version":1,"selector":{"name":"work","path":""},
        "private_root":root.join("private"),"private_mode":(1u32<<31)|0o700,"private_identity":"1:1",
        "context":{"home":root.join("home"),"cwd":root.join("cwd"),"xdg_cache_home":root.join("xdg"),
            "xdg_config_home":root.join("private"),"environment_revision":"a".repeat(64)},
        "active":peer("work"),"peers":[peer("main"),peer("work")],"configurations":configs,"revision":"b".repeat(64)});
    let validate = |value: Value| -> Result<(), Error> {
        let typed: Manifest =
            serde_json::from_slice(&serde_json::to_vec(&value).unwrap()).map_err(|_| invalid())?;
        bound.validate_manifest(&typed)
    };
    assert!(validate(good.clone()).is_ok());
    for (pointer, value) in [
        ("/selector/name", serde_json::json!("main")),
        ("/private_root", serde_json::json!("/outside")),
        ("/private_mode", serde_json::json!((1u32 << 31) | 0o755)),
        ("/context/home", serde_json::json!("/outside")),
        ("/active/name", serde_json::json!("main")),
        ("/peers/0/name", serde_json::json!("work")),
        ("/configurations/0/present", serde_json::json!(false)),
        ("/configurations/0/path", serde_json::json!("/outside")),
    ] {
        let mut bad = good.clone();
        *bad.pointer_mut(pointer).unwrap() = value;
        assert!(validate(bad).is_err(), "{pointer}");
    }
    let mut bad = good.clone();
    bad["peers"][0]["ordinary_db"] = serde_json::json!(root.join("home/normal/../index-v11.db"));
    bad["peers"][0]["resolved_ordinary_db"] = bad["peers"][0]["ordinary_db"].clone();
    bad["peers"][0]["private_db"] =
        serde_json::json!(root.join("private/canonical-read-only-v1/../index-v11.db"));
    assert!(validate(bad).is_err());
    let mut unknown = good;
    unknown["extra"] = serde_json::json!(true);
    assert!(validate(unknown).is_err());
}

#[test]
#[ignore = "explicit test-only Go bridge binary and synthetic-only opt-in"]
fn synthetic_bound_wrong_revision_exits_without_cache_or_note_writes() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    let before = admission_process_tests::protected_inventory(&fixture.0);
    let mut bound = SyntheticBound::new(proof, fixture.0.clone()).unwrap();
    let mut cmd = Command::new(binary);
    bound.configure(&mut cmd).unwrap();
    cmd.env(
        "TRACK_WEB_DESKTOP_PARENT_PID",
        std::process::id().to_string(),
    )
    .env("TRACK_WEB_DESKTOP_READY_TOKEN", "c".repeat(64));
    bound
        .capture(&mut cmd, Duration::from_secs(5), &AtomicBool::new(false))
        .unwrap();
    assert_eq!(
        std::fs::read_dir(fixture.0.join("private"))
            .unwrap()
            .count(),
        0
    );
    let mut wrong: Value = serde_json::from_slice(&bound.take_serve_request().unwrap()).unwrap();
    wrong["revision"] = serde_json::json!("0".repeat(64));
    let payload = serde_json::to_vec(&wrong).unwrap();
    cmd.stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .process_group(0);
    assert!(
        capture_exchange(
            &mut cmd,
            Duration::from_secs(5),
            &AtomicBool::new(false),
            Some(payload)
        )
        .is_err()
    );
    assert_eq!(
        std::fs::read_dir(fixture.0.join("private"))
            .unwrap()
            .count(),
        0
    );
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}

#[test]
#[ignore = "explicit test-only Go bridge binary and synthetic-only opt-in"]
fn synthetic_bound_session_is_finite_only_and_reaps_its_owner() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    let before = admission_process_tests::protected_inventory(&fixture.0);
    let bound = SyntheticBound::new(proof, fixture.0.clone()).unwrap();
    let owner = OwnedServer::start_admitted(
        bound.proof.launch(),
        &AtomicBool::new(false),
        StartupAdmission::SyntheticBound(bound),
    )
    .unwrap();
    let pid = owner.child.as_ref().unwrap().id();
    let session = Session::from_owned(owner);
    let snapshot = session.events();
    assert!(snapshot.unavailable);
    assert!(!snapshot.connected && !snapshot.ever_connected);
    assert_eq!(
        (
            snapshot.change,
            snapshot.data,
            snapshot.follow,
            snapshot.gaps,
            snapshot.initial_failures
        ),
        (0, 0, 0, 0, 0)
    );
    let client = session.client();
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
    assert!(session.failure().is_none());
    session.shutdown().unwrap();
    assert!(!client.is_alive());
    assert_eq!(session.events(), snapshot);
    let mut status = 0;
    assert_eq!(
        unsafe { libc::waitpid(pid as i32, &mut status, libc::WNOHANG) },
        -1
    );
    assert_eq!(
        std::io::Error::last_os_error().raw_os_error(),
        Some(libc::ECHILD)
    );
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}

#[path = "live_bound_workflows.rs"]
mod workflows;

#[path = "live_bound_calendar.rs"]
mod calendar_workflows;

#[path = "live_bound_render.rs"]
mod render_workflows;

#[path = "live_bound_assets.rs"]
mod asset_workflows;

#[path = "live_bound_navigation.rs"]
mod navigation_workflows;
