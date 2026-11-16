//! Opt-in actual-Go tests. No GUI, personal vault, or default CLI invocation.
use super::*;
use std::os::unix::fs::{MetadataExt, PermissionsExt};
use std::path::Path;

type Inventory = BTreeMap<PathBuf, (u32, u64, i64, i64, Vec<u8>)>;
fn inventory(root: &Path) -> Inventory {
    let mut result = BTreeMap::new();
    let mut pending = vec![root.to_owned()];
    while let Some(path) = pending.pop() {
        let metadata = std::fs::symlink_metadata(&path).unwrap();
        assert!(metadata.is_file() || metadata.is_dir());
        let bytes = if metadata.is_dir() {
            pending.extend(std::fs::read_dir(&path).unwrap().map(|e| e.unwrap().path()));
            Vec::new()
        } else {
            std::fs::read(&path).unwrap()
        };
        result.insert(
            path.strip_prefix(root).unwrap().to_owned(),
            (
                metadata.mode(),
                metadata.len(),
                metadata.mtime(),
                metadata.mtime_nsec(),
                bytes,
            ),
        );
    }
    result
}
struct Fixture(PathBuf);
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
fn fixture() -> Fixture {
    let root = std::env::temp_dir().canonicalize().unwrap().join(format!(
        "track-owned-read-sentinel-{}-{}",
        std::process::id(),
        NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed)
    ));
    std::fs::create_dir(&root).unwrap();
    std::fs::create_dir_all(root.join("vault/note")).unwrap();
    std::fs::create_dir_all(root.join("vault/.track/notes")).unwrap();
    std::fs::create_dir(root.join("cache")).unwrap();
    std::fs::set_permissions(root.join("cache"), std::fs::Permissions::from_mode(0o700)).unwrap();
    std::fs::write(root.join("config"), "{}\n").unwrap();
    std::fs::write(
        root.join("vault/.track-native-edit-fixture"),
        "disposable-fixture-only\n",
    )
    .unwrap();
    std::fs::write(
        root.join("vault/note/42.md"),
        "Read sentinel\n- [ ] untouched task\n",
    )
    .unwrap();
    std::fs::write(
        root.join("vault/.track/notes/42.yaml"),
        "version: 1\ntitle: Read sentinel\n",
    )
    .unwrap();
    std::fs::File::open(root.join("vault/note/42.md"))
        .unwrap()
        .set_modified(std::time::UNIX_EPOCH + Duration::from_secs(1577880000))
        .unwrap();
    Fixture(root)
}
fn launch(binary: &Path, f: &Fixture) -> LaunchConfig {
    let vault = f.0.join("vault");
    let config = f.0.join("config");
    let cache = f.0.join("cache");
    let port = (24000..35000)
        .find(|p| {
            TcpListener::bind(("127.0.0.1", *p)).is_ok()
                && TcpListener::bind(("127.0.0.1", *p + 1)).is_ok()
        })
        .unwrap();
    let mut launch = LaunchConfig::new(binary.to_owned(), Vault::Path(vault.clone()), port);
    launch.config = Some(config.clone());
    launch.cache = Some(cache.clone());
    launch.read_fixture =
        Some(ReadFixture::capture(&vault, &vault, Some(&config), Some(&cache)).unwrap());
    assert!(launch.creation_fixture.is_none());
    launch
}
fn wait_ready(client: &ReadClient) {
    let deadline = Instant::now() + Duration::from_secs(20);
    loop {
        let state = client.storage().unwrap();
        if state.phase == "ready" {
            return;
        }
        assert!(
            Instant::now() < deadline,
            "storage never verified: {state:?}"
        );
        thread::sleep(Duration::from_millis(25));
    }
}
#[test]
#[ignore = "explicit TRACK_NATIVE_TEST_BINARY + TRACK_NATIVE_TEST_SYNTHETIC_ONLY=1; actual Go process"]
fn owned_canonical_read_fixture_preserves_bytes_and_metadata() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary =
        PathBuf::from(std::env::var_os("TRACK_NATIVE_TEST_BINARY").expect("explicit Go binary"));
    assert!(binary.is_absolute());
    let f = fixture();
    let vault = f.0.join("vault");
    let before = inventory(&vault);
    let config_before = inventory(&f.0.join("config"));
    let mut server = OwnedServer::start(launch(&binary, &f)).unwrap();
    let client = server.client();
    let capability: Value = client.get("/api/capabilities", &[]).unwrap();
    assert_eq!(
        capability,
        serde_json::json!({"protocol":"track-canonical-read-only","version":1,"canonical_read_only":true})
    );
    assert!(!client.creation_available());
    wait_ready(&client);
    let listing = client.notes("").unwrap();
    let key = listing
        .notes
        .iter()
        .find(|n| n.note_id == 42)
        .expect("fixture note")
        .key();
    assert!(
        client
            .note(&key)
            .unwrap()
            .note
            .body
            .contains("Read sentinel")
    );
    assert_eq!(before, inventory(&vault));
    // Explicit fixture edit changes only this expected body entry. Never
    // resnapshot sidecars after the write: a buggy watcher could add Days.
    let mut expected = before;
    let body = b"Read sentinel externally updated\n";
    let row = expected.get_mut(Path::new("note/42.md")).unwrap();
    row.1 = body.len() as u64;
    row.2 = 1577966400;
    row.3 = 0;
    row.4 = body.to_vec();
    std::fs::write(vault.join("note/42.md"), body).unwrap();
    std::fs::File::open(vault.join("note/42.md"))
        .unwrap()
        .set_modified(std::time::UNIX_EPOCH + Duration::from_secs(1577966400))
        .unwrap();
    assert!(
        client
            .note(&key)
            .unwrap()
            .note
            .body
            .contains("externally updated")
    );
    let deadline = Instant::now() + Duration::from_secs(20);
    loop {
        if client
            .search("externally", "")
            .unwrap()
            .results
            .iter()
            .any(|n| n.note_id == 42)
        {
            break;
        }
        assert!(Instant::now() < deadline, "updated body never indexed");
        thread::sleep(Duration::from_millis(25));
    }
    assert_eq!(expected, inventory(&vault));
    assert_eq!(config_before, inventory(&f.0.join("config")));
    assert!(
        inventory(&f.0.join("cache"))
            .keys()
            .any(|p| p.extension().is_some_and(|e| e == "pid")),
        "private PID lease missing"
    );
    server.shutdown().unwrap();
    assert!(
        !inventory(&f.0.join("cache"))
            .keys()
            .any(|p| p.extension().is_some_and(|e| e == "pid")),
        "PID lease survived shutdown"
    );
    assert_eq!(expected, inventory(&vault));
    assert_eq!(config_before, inventory(&f.0.join("config")));
    assert!(
        !std::fs::read_dir(f.0.join("cache"))
            .unwrap()
            .collect::<Vec<_>>()
            .is_empty()
    );
}
