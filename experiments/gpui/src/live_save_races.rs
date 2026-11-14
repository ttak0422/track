//! Opt-in headless tests against an explicitly supplied real engine. No production hooks.
use super::*;
use crate::editing::{
    ComparisonMatch, DraftState, EditSession, LeaveAction, LeaveChoice, LeavePrompt, SaveOutcome,
};
use std::{io::Write, net::TcpStream, path::Path, sync::mpsc};

const WAIT: Duration = Duration::from_secs(8);
const LIMIT: usize = 256 * 1024;
struct Fixture(PathBuf);
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
// Every command is direct, bounded and isolated. Discard command output rather than
// logging inherited configuration or source; only synthetic stdin is supplied.
fn create_note(binary: &Path, root: &Path, title: &str, body: &str) {
    let mut child = Command::new(binary)
        .args(["new", "--title", title])
        .env("TRACK_VAULT", root.join("vault"))
        .env("TRACK_CONFIG", root.join("config.yml"))
        .env("TRACK_CACHE_DIR", root.join("cache"))
        .env_remove("TRACK_WEB_DESKTOP_PARENT_PID")
        .env_remove("TRACK_WEB_DESKTOP_READY_TOKEN")
        .stdin(Stdio::piped())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    if let Err(error) = child.stdin.take().unwrap().write_all(body.as_bytes()) {
        let _ = child.kill();
        let _ = child.wait();
        panic!("synthetic stdin failed: {error}");
    }
    let deadline = Instant::now() + WAIT;
    loop {
        if let Some(status) = child.try_wait().unwrap() {
            assert!(status.success(), "synthetic CLI creation failed");
            break;
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            panic!("synthetic CLI creation deadline");
        }
        thread::sleep(Duration::from_millis(10));
    }
}
fn guard(
    server: &mut OwnedServer,
    root: &Path,
    source: &NoteResponse,
    title: &str,
    body: &str,
) -> PathBuf {
    server.check_running().unwrap(); // start() already required this child's exact readiness token.
    for name in ["vault", "cache", "config.yml"] {
        let path = root.join(name);
        assert!(
            !std::fs::symlink_metadata(&path)
                .unwrap()
                .file_type()
                .is_symlink()
        );
        assert_eq!(path.canonicalize().unwrap(), path);
    }
    let vault = root.join("vault");
    let client = server.client();
    let active = client.vaults().unwrap();
    assert_eq!(
        Path::new(active["active"]["path"].as_str().unwrap())
            .canonicalize()
            .unwrap(),
        vault
    );
    let fresh = client.note(&source.note.note.key()).unwrap();
    assert_eq!(fresh.note.note.key(), source.note.note.key());
    assert_eq!(fresh.note.note.title, title);
    assert_eq!(fresh.note.body, body);
    assert_eq!(fresh.note.etag, source.note.etag);
    writable_source(&client.storage().unwrap(), &fresh).unwrap();
    let path = PathBuf::from(&fresh.note.note.path);
    assert!(path.is_absolute());
    assert!(std::fs::symlink_metadata(&path).unwrap().is_file());
    let canonical = path.canonicalize().unwrap();
    assert_eq!(canonical, path, "note path must not traverse symlinks");
    assert!(canonical.starts_with(&vault) && canonical != vault);
    canonical
}
struct Gate {
    stop: Arc<AtomicBool>,
    worker: Option<thread::JoinHandle<()>>,
    ready: mpsc::Receiver<()>,
    release: mpsc::Sender<()>,
    puts: Arc<AtomicU64>,
    port: u16,
}
impl Drop for Gate {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Release);
        // Wake a held PUT on panic; never forward without an explicit release.
        if let Some(worker) = self.worker.take() {
            let result = worker.join();
            if !thread::panicking() {
                result.expect("relay failed");
            }
        }
    }
}
fn relay(upstream: &ReadClient, request: &SaveRequest, drop_ack: bool) -> (ReadClient, Gate) {
    let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
    listener.set_nonblocking(true).unwrap();
    let port = listener.local_addr().unwrap().port();
    let stop = Arc::new(AtomicBool::new(false));
    let puts = Arc::new(AtomicU64::new(0));
    let (ready_tx, ready) = mpsc::channel();
    let (release, release_rx) = mpsc::channel();
    let mut client = upstream.clone();
    client.origin = format!("http://127.0.0.1:{port}");
    let upstream = upstream.clone();
    let expected = request.clone();
    let stopping = stop.clone();
    let counts = puts.clone();
    let worker = thread::spawn(move || {
        let deadline = Instant::now() + Duration::from_secs(30);
        let mut requests = 0;
        while !stopping.load(Ordering::Acquire) && Instant::now() < deadline {
            let (mut socket, _) = match listener.accept() {
                Ok(value) => value,
                Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                    thread::sleep(Duration::from_millis(2));
                    continue;
                }
                Err(error) => panic!("relay accept: {error}"),
            };
            requests += 1;
            assert!(requests <= 12, "unexpected retry/request flood");
            socket.set_nonblocking(false).unwrap();
            socket.set_read_timeout(Some(WAIT)).unwrap();
            socket.set_write_timeout(Some(WAIT)).unwrap();
            let (method, target, body) = read_request(&mut socket);
            let url = reqwest::Url::parse(&format!("{}{}", upstream.origin, target)).unwrap();
            assert_eq!(
                url.origin(),
                reqwest::Url::parse(&upstream.origin).unwrap().origin()
            );
            let mut query = url.query_pairs().collect::<Vec<_>>();
            query.sort();
            let mut expected_query = vault_query(&expected.key.vault)
                .into_iter()
                .map(|(k, v)| (k.to_string(), v))
                .collect::<Vec<_>>();
            expected_query.push(("id".into(), expected.key.note_id.to_string()));
            expected_query.sort();
            match url.path() {
                "/api/storage" => {
                    assert_eq!(method, "GET");
                    assert!(query.is_empty());
                }
                "/api/note" => assert_eq!(
                    query
                        .iter()
                        .map(|(k, v)| (k.to_string(), v.to_string()))
                        .collect::<Vec<_>>(),
                    expected_query
                ),
                _ => panic!("unexpected relay route"),
            }
            assert!(method == "GET" || method == "PUT");
            if method == "PUT" {
                assert_eq!(
                    requests, 4,
                    "native storage/source/storage preflight precedes PUT"
                );
                assert_eq!(
                    counts.fetch_add(1, Ordering::AcqRel),
                    0,
                    "no repeated PUT allowed"
                );
                assert_eq!(url.path(), "/api/note");
                let supplied: Value = serde_json::from_slice(&body).unwrap();
                assert_eq!(
                    supplied,
                    serde_json::json!({"body":expected.body,"etag":expected.etag})
                );
                ready_tx.send(()).unwrap();
                let until = Instant::now() + WAIT;
                loop {
                    if stopping.load(Ordering::Acquire) {
                        return;
                    }
                    match release_rx.recv_timeout(Duration::from_millis(10)) {
                        Ok(()) => break,
                        Err(mpsc::RecvTimeoutError::Timeout) => {
                            assert!(Instant::now() < until, "gate release deadline")
                        }
                        Err(_) => return,
                    }
                }
            }
            assert!(upstream.alive.load(Ordering::Acquire));
            let mut response = upstream
                .http
                .request(reqwest::Method::from_bytes(method.as_bytes()).unwrap(), url)
                .header("Content-Type", "application/json")
                .header("Cache-Control", "no-cache, no-store")
                .body(body)
                .send()
                .unwrap();
            let status = response.status().as_u16();
            let mut bytes = Vec::new();
            (&mut response)
                .take((LIMIT + 1) as u64)
                .read_to_end(&mut bytes)
                .unwrap();
            assert!(bytes.len() <= LIMIT);
            if method == "PUT" && drop_ack {
                assert_eq!(status, 200, "real upstream must commit before dropping ack");
                let ack: SaveAck = serde_json::from_slice(&bytes).unwrap();
                assert!(ack.saved && !ack.etag.is_empty() && ack.etag != expected.etag);
                assert_eq!(ack.note_id, expected.key.note_id);
                assert_eq!(ack.vault, expected.key.vault);
                drop(socket);
            } else {
                write!(socket, "HTTP/1.1 {status} Result\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", bytes.len()).unwrap();
                socket.write_all(&bytes).unwrap();
            }
        }
    });
    (
        client,
        Gate {
            stop,
            worker: Some(worker),
            ready,
            release,
            puts,
            port,
        },
    )
}
fn read_request(socket: &mut TcpStream) -> (String, String, Vec<u8>) {
    let mut header = Vec::new();
    let deadline = Instant::now() + WAIT;
    while !header.ends_with(b"\r\n\r\n") {
        assert!(header.len() < 8192 && Instant::now() < deadline);
        let mut byte = [0];
        socket.read_exact(&mut byte).unwrap();
        header.push(byte[0]);
    }
    let text = std::str::from_utf8(&header).unwrap();
    let mut lines = text.split("\r\n");
    let first = lines.next().unwrap().split_whitespace().collect::<Vec<_>>();
    assert_eq!(first.len(), 3);
    assert!(first[1].starts_with("/api/") && !first[1].contains('#'));
    let mut length = None;
    for line in lines.filter(|s| !s.is_empty()) {
        let (name, value) = line.split_once(':').unwrap();
        assert!(!name.eq_ignore_ascii_case("transfer-encoding"));
        if name.eq_ignore_ascii_case("content-length") {
            assert!(length.is_none());
            length = Some(value.trim().parse::<usize>().unwrap());
        }
    }
    let length = length.unwrap_or(0);
    assert!(length <= LIMIT);
    let mut body = vec![0; length];
    socket.read_exact(&mut body).unwrap();
    (first[0].into(), first[1].into(), body)
}

#[test]
#[ignore = "explicit TRACK_NATIVE_TEST_BINARY and TRACK_NATIVE_TEST_SYNTHETIC_ONLY=1; synthetic notes only"]
fn owned_engine_post_preflight_conflict_and_lost_ack() {
    let executable =
        PathBuf::from(std::env::var_os("TRACK_NATIVE_TEST_BINARY").expect("explicit real binary"));
    assert!(executable.is_absolute());
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1"),
        "explicit consent to fresh synthetic-only fixture required"
    );
    let unique = SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    let root = std::env::temp_dir().canonicalize().unwrap().join(format!(
        "track-native-races-{}-{unique}",
        std::process::id()
    ));
    std::fs::create_dir(&root).unwrap();
    let fixture = Fixture(root);
    // Public CLI supports a pre-existing empty directory, without manual note/sidecar formats.
    std::fs::create_dir(fixture.0.join("vault")).unwrap();
    std::fs::create_dir(fixture.0.join("cache")).unwrap();
    std::fs::write(fixture.0.join("config.yml"), "{}\n").unwrap();
    let titles = [
        format!("Native synthetic conflict {unique}"),
        format!("Native synthetic lost ack {unique}"),
    ];
    const BASE: &str = "Synthetic baseline 日本語";
    for title in &titles {
        create_note(&executable, &fixture.0, title, BASE);
    }
    let port = (30000..35000)
        .find(|p| {
            TcpListener::bind(("127.0.0.1", *p)).is_ok()
                && TcpListener::bind(("127.0.0.1", *p + 1)).is_ok()
        })
        .unwrap();
    let mut config = LaunchConfig::new(executable, Vault::Path(fixture.0.join("vault")), port);
    config.config = Some(fixture.0.join("config.yml"));
    config.cache = Some(fixture.0.join("cache"));
    config.request_timeout = WAIT;
    let mut server = OwnedServer::start(config).expect("owned readiness");
    let pid = server.child.as_ref().unwrap().id();
    let client = server.client();
    let launch = client.launch_generation;
    let deadline = Instant::now() + Duration::from_secs(30);
    let storage = loop {
        server.check_running().unwrap();
        let storage = client.storage().unwrap();
        if storage.phase == "ready" && !storage.read_only {
            break storage;
        }
        assert!(
            Instant::now() < deadline,
            "fixture storage did not become writable: {storage:?}"
        );
        thread::sleep(Duration::from_millis(20));
    };
    for (case, title) in titles.iter().enumerate() {
        let listing = client.notes("").unwrap();
        let notes = listing
            .notes
            .iter()
            .filter(|n| n.title == *title)
            .collect::<Vec<_>>();
        assert_eq!(notes.len(), 1);
        let source = client.note(&notes[0].key()).unwrap();
        let path = guard(&mut server, &fixture.0, &source, title, BASE);
        let authored = "Native retained draft 日本語 [[Link]]\n\n";
        let competitor = "Deliberate competitor 日本語";
        let mut edit = EditSession::from_note(launch, &storage, &source).unwrap();
        edit.replace_text(authored.into()).unwrap();
        let LeaveAction::Save(prepared) = edit
            .choose_leave(LeaveChoice::Save, launch, &storage)
            .unwrap()
        else {
            panic!()
        };
        let (relay_client, gate) = relay(&client, &prepared.request, case == 1);
        let attempt = prepared.attempt;
        let worker = thread::spawn(move || relay_client.save(&prepared.request));
        gate.ready.recv_timeout(WAIT).unwrap(); // Complete primary PUT held AFTER all native preflight GETs.
        assert_eq!(edit.leave_prompt(), LeavePrompt::WaitForSave);
        assert!(edit.prepare_save(launch, &storage).is_err());
        assert!(edit.replace_text("blocked".into()).is_err());
        assert_eq!(edit.draft(), authored);
        guard(&mut server, &fixture.0, &source, title, BASE);
        if case == 0 {
            let request =
                SaveRequest::from_source(launch, &storage, &source, competitor.into()).unwrap();
            let ack = client.save(&request).unwrap();
            assert_ne!(ack.etag, source.note.etag);
            let committed = client.note(&request.key).unwrap();
            assert_eq!(committed.note.etag, ack.etag);
            guard(&mut server, &fixture.0, &committed, title, competitor);
        }
        server.check_running().unwrap();
        gate.release.send(()).unwrap();
        let error = worker.join().unwrap().unwrap_err();
        if case == 0 {
            assert!(
                matches!(&error, Error::Http { status: 409, body } if body.contains("note changed on disk since it was loaded; reload before saving"))
            );
        } else {
            assert_eq!(error.save_disposition(), SaveDisposition::Uncertain);
        }
        assert_eq!(
            edit.finish_save(attempt, launch, storage.generation, Err(error)),
            SaveOutcome::Retained
        );
        assert!(if case == 0 {
            matches!(edit.state(), DraftState::Conflict(_))
        } else {
            matches!(edit.state(), DraftState::Uncertain(_))
        });
        assert_eq!(edit.leave_prompt(), LeavePrompt::AskSaveDiscardCancel);
        assert!(edit.prepare_save(launch, &storage).is_err());
        let latest = client.note(edit.key()).unwrap(); // Explicit comparison only, never retry.
        let expected = if case == 0 {
            competitor
        } else {
            authored.trim_end_matches('\n')
        };
        assert_eq!(latest.note.body, expected);
        assert_ne!(latest.note.etag, source.note.etag);
        assert_eq!(latest.note.note.key(), source.note.note.key());
        assert_eq!(latest.note.note.title, *title);
        assert_eq!(latest.note.note.tags, source.note.note.tags);
        assert_eq!(
            std::fs::read_to_string(path).unwrap(),
            if case == 0 {
                format!("{competitor}\n")
            } else {
                authored.into()
            }
        );
        edit.inspect_latest(launch, &storage, &latest).unwrap();
        if case == 1 {
            assert_eq!(
                edit.comparison_match(),
                Some(ComparisonMatch::TerminalLfUnobservable)
            );
            assert!(edit.accept_matching_comparison().is_err());
        }
        assert_eq!(edit.draft(), authored);
        assert_eq!(edit.baseline(), BASE);
        assert_eq!(edit.etag(), source.note.etag);
        assert!(edit.dirty());
        assert!(matches!(
            edit.choose_leave(LeaveChoice::Cancel, launch, &storage)
                .unwrap(),
            LeaveAction::Stay
        ));
        assert_eq!(gate.puts.load(Ordering::Acquire), 1);
        let relay_port = gate.port;
        drop(gate);
        assert!(TcpListener::bind(("127.0.0.1", relay_port)).is_ok());
    }
    server.shutdown().unwrap();
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
        "real engine: post-preflight 409 + committed/lost-ack uncertainty; retained drafts, metadata/LF, no retry, owned reap/listener release verified"
    );
}
use std::time::SystemTime;
