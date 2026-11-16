//! Synthetic registered-peer integration only. Production ordinary gate stays closed.
//! Absolute generated config paths only: HOME/tilde resolution is covered by the
//! separate Go process matrix, not claimed by these native integration tests.
//! Missing-peer coverage here is proof rejection before spawn, not engine
//! partial-registry behavior. No UI, hostile-filesystem sandbox, or Mac claim.
use super::read_inputs::{CanonicalLaunchInputs, SyntheticCanonical};
use super::*;
use std::os::unix::fs::{MetadataExt, PermissionsExt};
use std::path::Path;

type Inventory = BTreeMap<PathBuf, (u32, u64, i64, i64, Vec<u8>)>;
fn inventory(root: &Path) -> Inventory {
    inventory_skipping(root, None)
}
fn inventory_skipping(root: &Path, excluded: Option<&Path>) -> Inventory {
    let mut result = BTreeMap::new();
    let mut pending = vec![root.to_owned()];
    while let Some(path) = pending.pop() {
        // Prune the known runtime subtree before even inspecting its volatile
        // SQLite files. Root metadata and every unexpected sibling stay covered.
        if excluded == Some(path.as_path()) {
            continue;
        }
        let meta = std::fs::symlink_metadata(&path).unwrap();
        assert!(meta.is_file() || meta.is_dir());
        let bytes = if meta.is_dir() {
            pending.extend(std::fs::read_dir(&path).unwrap().map(|e| e.unwrap().path()));
            Vec::new()
        } else {
            std::fs::read(&path).unwrap()
        };
        result.insert(
            path.strip_prefix(root).unwrap().to_owned(),
            (
                meta.mode(),
                meta.len(),
                meta.mtime(),
                meta.mtime_nsec(),
                bytes,
            ),
        );
    }
    result
}
fn protected_inventory(root: &Path) -> Inventory {
    inventory_skipping(root, Some(&root.join("private")))
}
struct Fixture(PathBuf);
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
fn fixture(binary: &Path) -> (Fixture, SyntheticCanonical) {
    let root = std::env::temp_dir().canonicalize().unwrap().join(format!(
        "track-native-registry-{}-{}",
        std::process::id(),
        NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed)
    ));
    std::fs::create_dir(&root).unwrap();
    std::fs::write(
        root.join(".track-native-read-fixture"),
        "disposable-fixture-only\n",
    )
    .unwrap();
    for dir in ["home", "home/normal", "private", "xdg", "cwd"] {
        std::fs::create_dir(root.join(dir)).unwrap();
    }
    std::fs::set_permissions(root.join("private"), std::fs::Permissions::from_mode(0o700)).unwrap();
    let mut registry = BTreeMap::new();
    for (name, label) in [("main", "Active"), ("work", "Foreign")] {
        let vault = root.join("home").join(name);
        std::fs::create_dir_all(vault.join("note")).unwrap();
        std::fs::create_dir_all(vault.join(".track/notes")).unwrap();
        std::fs::write(vault.join(".track/config.yml"), "link_format: id-v1\n").unwrap();
        std::fs::write(vault.join("note/42.md"), format!("{label} sentinel body\n")).unwrap();
        std::fs::write(
            vault.join(".track/notes/42.yaml"),
            format!("version: 1\ntitle: {label} sentinel\n"),
        )
        .unwrap();
        std::fs::File::open(vault.join("note/42.md"))
            .unwrap()
            .set_modified(std::time::UNIX_EPOCH + Duration::from_secs(1577880000))
            .unwrap();
        registry.insert(name.into(), vault);
    }
    let normal = root.join("home/normal");
    std::fs::write(
        root.join("config"),
        SyntheticCanonical::config_bytes(&registry, &normal),
    )
    .unwrap();
    let port = (24000..35000)
        .find(|p| {
            TcpListener::bind(("127.0.0.1", *p)).is_ok()
                && TcpListener::bind(("127.0.0.1", *p + 1)).is_ok()
        })
        .unwrap();
    // Select work while generated default_vault remains main: omitting --vault
    // must fail the exact active identity assertions below.
    let mut launch = LaunchConfig::new(binary.to_owned(), Vault::Registered("work".into()), port);
    launch.config = Some(root.join("config"));
    launch.cache = Some(root.join("private"));
    let inputs = CanonicalLaunchInputs::capture(
        &launch,
        &root.join("xdg"),
        &root.join("home"),
        &root.join("cwd"),
    )
    .unwrap();
    let proof = SyntheticCanonical::capture(&root, inputs, registry, &normal).unwrap();
    (Fixture(root), proof)
}
fn binary() -> PathBuf {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let path = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_TEST_BINARY").expect("explicit actual Go binary"),
    );
    assert!(path.is_absolute());
    path
}
fn wait_for(mut predicate: impl FnMut() -> bool, label: &str) {
    let deadline = Instant::now() + Duration::from_secs(20);
    while !predicate() {
        assert!(Instant::now() < deadline, "timed out: {label}");
        thread::sleep(Duration::from_millis(25));
    }
}
#[test]
#[ignore = "explicit actual Go binary and synthetic-only opt-in"]
fn synthetic_registered_peers_preserve_canonical_and_normal_cache() {
    let (f, proof) = fixture(&binary());
    let protected_before = protected_inventory(&f.0);
    let home_before = inventory(&f.0.join("home"));
    let config_before = inventory(&f.0.join("config"));
    let xdg_before = inventory(&f.0.join("xdg"));
    let cwd_before = inventory(&f.0.join("cwd"));
    let mut server = OwnedServer::start_synthetic_canonical(proof).unwrap();
    assert_eq!(server.initial_vaults["active"]["name"], "work");
    assert_eq!(
        server.initial_vaults["active"]["path"],
        f.0.join("home/work").to_str().unwrap()
    );
    assert_eq!(server.initial_vaults["unavailable"], serde_json::json!([]));
    let registry = server.initial_vaults["vaults"].as_array().unwrap();
    assert_eq!(registry.len(), 2);
    for name in ["main", "work"] {
        let entry = registry.iter().find(|entry| entry["name"] == name).unwrap();
        assert_eq!(entry["path"], f.0.join("home").join(name).to_str().unwrap());
        assert_eq!(entry["active"], name == "work");
    }
    let client = server.client();
    assert!(!client.creation_available());
    assert!(matches!(
        client.create_note("Must not be created", "", client.launch_generation),
        Err(CreateError::NotSent(_))
    ));
    let capability: Value = client.get("/api/capabilities", &[]).unwrap();
    assert_eq!(
        capability,
        serde_json::json!({"protocol":"track-canonical-read-only","version":1,"canonical_read_only":true})
    );
    wait_for(
        || client.storage().unwrap().phase == "ready",
        "active verification",
    );
    for (vault, label) in [("main", "Active"), ("work", "Foreign")] {
        let key = NoteKey {
            vault: vault.into(),
            note_id: 42,
        };
        wait_for(|| client.note(&key).is_ok(), "peer read availability");
        let source = client.note(&key).unwrap();
        let storage = client.storage().unwrap();
        let attempted = SaveRequest {
            key: key.clone(),
            body: "Must not be saved\n".into(),
            etag: source.note.etag.clone(),
            launch_generation: client.launch_generation,
            storage_generation: storage.generation,
        };
        assert!(matches!(
            client.save(&attempted),
            Err(Error::SaveNotSent(_))
        ));
        assert_eq!(source.note.note.note_id, 42);
        assert_eq!(source.note.body, format!("{label} sentinel body"));
        assert_eq!(
            source.note.note.path,
            f.0.join("home")
                .join(vault)
                .join("note/42.md")
                .to_str()
                .unwrap()
        );
    }
    assert_eq!(protected_before, protected_inventory(&f.0));
    assert_eq!(home_before, inventory(&f.0.join("home")));
    assert_eq!(config_before, inventory(&f.0.join("config")));
    assert_eq!(xdg_before, inventory(&f.0.join("xdg")));
    assert_eq!(cwd_before, inventory(&f.0.join("cwd")));
    assert!(
        inventory(&f.0.join("private"))
            .keys()
            .any(|p| p.extension().is_some_and(|e| e == "pid"))
    );
    server.shutdown().unwrap();
    assert_eq!(protected_before, protected_inventory(&f.0));
    assert_eq!(home_before, inventory(&f.0.join("home")));
    assert_eq!(config_before, inventory(&f.0.join("config")));
    assert_eq!(xdg_before, inventory(&f.0.join("xdg")));
    assert_eq!(cwd_before, inventory(&f.0.join("cwd")));
    assert!(
        !inventory(&f.0.join("private"))
            .keys()
            .any(|p| p.extension().is_some_and(|e| e == "pid"))
    );
}
#[test]
#[ignore = "explicit actual Go binary and synthetic-only opt-in"]
fn synthetic_missing_peer_is_rejected_without_runtime_files() {
    let (f, proof) = fixture(&binary());
    // Deliberate removal is fixture setup, before the rejection snapshot. The
    // narrow proof must reject, rather than widening admission to missing peers.
    std::fs::remove_dir_all(f.0.join("home/main")).unwrap();
    let before = inventory(&f.0);
    match OwnedServer::start_synthetic_canonical(proof) {
        Err(Error::Config(message)) => assert_eq!(
            message,
            "Launch input must be an existing canonical absolute path"
        ),
        Err(other) => panic!("expected missing-binding proof rejection, got {other:?}"),
        Ok(_) => panic!("missing peer unexpectedly admitted"),
    }
    assert_eq!(before, inventory(&f.0));
    assert!(
        std::fs::read_dir(f.0.join("private"))
            .unwrap()
            .next()
            .is_none()
    );
}

#[test]
#[ignore = "explicit actual Go binary and synthetic-only opt-in"]
fn synthetic_changed_config_is_rejected_without_runtime_files() {
    let (f, proof) = fixture(&binary());
    // Exact generated bytes are authority, not arbitrary configuration located
    // within the temp root. Deliberate invalidation precedes the snapshot.
    std::fs::write(f.0.join("config"), b"{}\n").unwrap();
    let before = inventory(&f.0);
    match OwnedServer::start_synthetic_canonical(proof) {
        Err(Error::Config(message)) => assert_eq!(message, "Configuration bytes changed"),
        Err(other) => panic!("expected config-byte proof rejection, got {other:?}"),
        Ok(_) => panic!("changed configuration unexpectedly admitted"),
    }
    assert_eq!(before, inventory(&f.0));
    assert!(
        std::fs::read_dir(f.0.join("private"))
            .unwrap()
            .next()
            .is_none()
    );
}
