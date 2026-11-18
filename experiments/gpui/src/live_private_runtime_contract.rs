//! Fixture-only ownership -> bounded Go preflight integration. Neither agreement
//! with an explicit workspace request nor a fresh owned directory grants ordinary
//! admission. No serve, real vault, GUI, or hostile-filesystem claim is made.
use super::private_runtime::{CleanupOutcome, PrivateRuntime};
use super::workspace_provenance::{
    LaunchManifestObservation, PeerScopeRequest, RequestedSelector, WorkspaceScopeRequest,
};
use super::*;
use std::os::unix::{fs::MetadataExt, process::CommandExt};
use std::path::Path;

fn binary() -> PathBuf {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY")
            .expect("explicit Go fixture test binary"),
    );
    assert!(binary.is_absolute());
    binary
}
fn scope(root: &Path, selected: &str, peers: &[&str]) -> WorkspaceScopeRequest {
    WorkspaceScopeRequest::new(
        RequestedSelector::Registered(selected.into()),
        PeerScopeRequest::ExplicitRequestedSet(
            peers
                .iter()
                .map(|name| {
                    (
                        (*name).into(),
                        root.join("home").join(name).to_str().unwrap().into(),
                    )
                })
                .collect(),
        ),
    )
    .unwrap()
}
fn entropy() -> String {
    let mut bytes = [0u8; 32];
    std::fs::File::open("/dev/urandom")
        .unwrap()
        .read_exact(&mut bytes)
        .unwrap();
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}
fn capture(
    proof: &read_inputs::SyntheticCanonical,
    root: &Path,
    runtime: &PrivateRuntime,
    requested_root: &Path,
    selector: &str,
) -> Result<Value, Error> {
    let requested_scope = scope(root, "work", &["main", "work"]);
    runtime.verify()?;
    proof.verify().map_err(Error::Config)?;
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
    let environment = command
        .get_envs()
        .filter_map(|(key, value)| value.map(|value| (key.to_owned(), value.to_owned())))
        .collect::<Vec<_>>();
    let context = launch_context::ResolutionContextRequest::capture(
        command.get_current_dir().unwrap(),
        &environment,
    )
    .unwrap();
    let request = launch_protocol::BoundLaunchRequest::new(
        context,
        RequestedSelector::Registered(selector.into()),
        requested_root.to_str().unwrap().into(),
        entropy(),
    )
    .unwrap();
    let outcome = super::capture::capture_exchange(
        &mut command,
        Duration::from_secs(10),
        &AtomicBool::new(false),
        Some(request.preflight().unwrap()),
    );
    // Recheck even when the owned child rejected the request or failed.
    runtime.verify()?;
    let bytes = outcome?;
    let text = std::str::from_utf8(&bytes)
        .map_err(|_| Error::Shape("non-UTF8 fixture response".into()))?;
    let mut lines = text
        .lines()
        .filter_map(|line| line.strip_prefix("TRACK_BOUND_MANIFEST:"));
    let manifest = lines
        .next()
        .ok_or_else(|| Error::Shape("missing fixture manifest".into()))?;
    assert!(lines.next().is_none());
    let observation = LaunchManifestObservation::parse(manifest.as_bytes()).unwrap();
    observation.check_requested_scope(&requested_scope).unwrap();
    Ok(serde_json::from_str(manifest).unwrap())
}
fn identity(path: &Path) -> (u64, u64, u32, u32) {
    let metadata = std::fs::symlink_metadata(path).unwrap();
    (
        metadata.dev(),
        metadata.ino(),
        metadata.uid(),
        metadata.mode(),
    )
}

#[test]
#[ignore = "explicit actual Go test binary and synthetic-only opt-in"]
fn owned_private_runtime_preflight_preserves_scope_identity_and_canonical_inventory() {
    let (fixture, proof) = admission_process_tests::fixture(&binary());
    let protected = admission_process_tests::protected_inventory(&fixture.0);
    let mut runtime = PrivateRuntime::create(&fixture.0.join("private")).unwrap();
    let before = identity(runtime.path());
    let manifest = capture(&proof, &fixture.0, &runtime, runtime.path(), "work").unwrap();
    assert_eq!(manifest["private_root"], runtime.path().to_str().unwrap());
    assert_eq!(manifest["private_mode"].as_u64().unwrap() & 0o777, 0o700);
    // The Unix Go manifest encodes dev:ino; correlate it with the independently
    // held Rust directory identity, not merely a second self-observation.
    assert_eq!(
        manifest["private_identity"],
        format!("{}:{}", before.0, before.1)
    );
    assert_eq!(manifest["active"]["name"], "work");
    assert_eq!(
        manifest["context"]["home"],
        fixture.0.join("home").to_str().unwrap()
    );
    assert_eq!(
        manifest["context"]["cwd"],
        fixture.0.join("cwd").to_str().unwrap()
    );
    assert_eq!(
        manifest["context"]["xdg_cache_home"],
        fixture.0.join("xdg").to_str().unwrap()
    );
    assert_eq!(
        manifest["context"]["xdg_config_home"],
        fixture.0.join("private").to_str().unwrap()
    );
    for peer in manifest["peers"].as_array().unwrap() {
        assert!(Path::new(peer["private_db"].as_str().unwrap()).starts_with(runtime.path()));
    }
    let again = capture(&proof, &fixture.0, &runtime, runtime.path(), "work").unwrap();
    assert_eq!(manifest["private_identity"], again["private_identity"]);
    assert_eq!(identity(runtime.path()), before);
    runtime.verify().unwrap();
    proof.verify_bindings().unwrap();
    assert_eq!(
        admission_process_tests::protected_inventory(&fixture.0),
        protected
    );
    // Preflight must not create indexes, namespaces, lockfiles, or temp artifacts.
    assert_eq!(std::fs::read_dir(runtime.path()).unwrap().count(), 0);
    assert_eq!(
        std::fs::read_dir(fixture.0.join("private"))
            .unwrap()
            .count(),
        1
    );
    let observation =
        LaunchManifestObservation::parse(&serde_json::to_vec(&manifest).unwrap()).unwrap();
    assert!(
        observation
            .check_requested_scope(&scope(&fixture.0, "work", &["work"]))
            .is_err()
    );
    assert!(
        observation
            .check_requested_scope(&scope(&fixture.0, "main", &["main", "work"]))
            .is_err()
    );
    assert_eq!(runtime.cleanup_empty().unwrap(), CleanupOutcome::Removed);
    assert!(OwnedServer::start(proof.launch()).is_err());
}

#[test]
#[ignore = "explicit actual Go test binary and synthetic-only opt-in"]
fn owned_private_runtime_rejects_request_mismatch_and_retains_unknown_contents() {
    let (fixture, proof) = admission_process_tests::fixture(&binary());
    let protected = admission_process_tests::protected_inventory(&fixture.0);
    let mut runtime = PrivateRuntime::create(&fixture.0.join("private")).unwrap();
    let before = identity(runtime.path());
    assert!(
        capture(
            &proof,
            &fixture.0,
            &runtime,
            &fixture.0.join("private"),
            "work"
        )
        .is_err()
    );
    assert!(capture(&proof, &fixture.0, &runtime, runtime.path(), "main").is_err());
    runtime.verify().unwrap();
    assert_eq!(identity(runtime.path()), before);
    assert_eq!(std::fs::read_dir(runtime.path()).unwrap().count(), 0);
    assert_eq!(
        admission_process_tests::protected_inventory(&fixture.0),
        protected
    );
    let sentinel = runtime.path().join("unknown-caller-owned-content");
    std::fs::write(&sentinel, b"retain me").unwrap();
    assert_eq!(
        runtime.cleanup_empty().unwrap(),
        CleanupOutcome::RetainedNonEmpty
    );
    assert_eq!(std::fs::read(&sentinel).unwrap(), b"retain me");
    runtime.verify().unwrap();
    // Only this fixture's explicit owner removes its known sentinel, not helper.
    std::fs::remove_file(sentinel).unwrap();
    assert_eq!(runtime.cleanup_empty().unwrap(), CleanupOutcome::Removed);
}
