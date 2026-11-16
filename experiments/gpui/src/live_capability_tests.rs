use super::*;
use std::io::Write;
use std::os::unix::fs::PermissionsExt;

const GOOD: &str =
    r#"{"protocol":"track-canonical-read-only","version":1,"canonical_read_only":true}"#;
struct Fixture(PathBuf);
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
fn fixture() -> (Fixture, LaunchConfig) {
    let root = std::env::temp_dir().join(format!(
        "track-capability-{}-{}",
        std::process::id(),
        NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed)
    ));
    std::fs::create_dir_all(root.join("vault")).unwrap();
    let root = root.canonicalize().unwrap();
    std::fs::create_dir(root.join("cache")).unwrap();
    std::fs::set_permissions(root.join("cache"), std::fs::Permissions::from_mode(0o700)).unwrap();
    std::fs::write(root.join("config"), "{}\n").unwrap();
    std::fs::write(
        root.join("vault/.track-native-edit-fixture"),
        "disposable-fixture-only\n",
    )
    .unwrap();
    let port_base = 31000 + (NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed) % 100) as u16 * 100;
    let port = (port_base..port_base + 90)
        .find(|p| {
            TcpListener::bind(("127.0.0.1", *p)).is_ok()
                && TcpListener::bind(("127.0.0.1", *p + 1)).is_ok()
        })
        .unwrap();
    let mut cfg = LaunchConfig::new(
        root.join("fake-engine"),
        Vault::Path(root.join("vault")),
        port,
    );
    cfg.config = Some(root.join("config"));
    cfg.cache = Some(root.join("cache"));
    cfg.read_fixture = Some(
        ReadFixture::capture(
            &root.join("vault"),
            &root.join("vault"),
            cfg.config.as_deref(),
            cfg.cache.as_deref(),
        )
        .unwrap(),
    );
    cfg.startup_timeout = Duration::from_secs(2);
    cfg.request_timeout = Duration::from_millis(100);
    (Fixture(root), cfg)
}

fn fake_case(
    status: u16,
    body: &str,
    mime: &str,
    token: bool,
    stall: bool,
    cancel: bool,
) -> (bool, Vec<String>, String) {
    let (fixture, mut cfg) = fixture();
    if !token {
        cfg.startup_timeout = Duration::from_millis(250);
    }
    let script = format!(
        "#!/bin/sh\nprintf 'spawn\\n' >> \"$HOME/../spawns\"\nprintf '%s\\n' \"$@\" > \"$HOME/../args\"\nprintf '%s\\n' $$ > \"$HOME/../pid\"\nwhile [ ! -f \"$HOME/../http-ready\" ]; do sleep 0.01; done\nprintf 'TRACK_DESKTOP_READY:%s\\n' {} >&2\nexec /bin/sleep 30\n",
        if token {
            "\"$TRACK_WEB_DESKTOP_READY_TOKEN\""
        } else {
            "wrong-token"
        }
    );
    std::fs::write(&cfg.executable, script).unwrap();
    std::fs::set_permissions(&cfg.executable, std::fs::Permissions::from_mode(0o700)).unwrap();
    let done = Arc::new(AtomicBool::new(false));
    let stop = done.clone();
    let canceled = Arc::new(AtomicBool::new(false));
    let cancel_worker = canceled.clone();
    let root = fixture.0.clone();
    let port = cfg.port;
    let body = body.to_string();
    let mime = mime.to_string();
    let http = thread::spawn(move || {
        let deadline = Instant::now() + Duration::from_secs(4);
        while !root.join("spawns").exists() {
            assert!(Instant::now() < deadline);
            thread::sleep(Duration::from_millis(2));
        }
        let listener = TcpListener::bind(("127.0.0.1", port)).unwrap();
        listener.set_nonblocking(true).unwrap();
        std::fs::write(root.join("http-ready"), "ready").unwrap();
        let mut requests = Vec::new();
        while !stop.load(Ordering::Acquire) && Instant::now() < deadline {
            match listener.accept() {
                Ok((mut stream, _)) => {
                    stream.set_nonblocking(false).unwrap();
                    stream
                        .set_read_timeout(Some(Duration::from_secs(1)))
                        .unwrap();
                    let mut request = Vec::new();
                    let mut b = [0];
                    while !request.ends_with(b"\r\n\r\n") {
                        if stream.read_exact(&mut b).is_err() {
                            break;
                        };
                        request.push(b[0]);
                        if request.len() > 8192 {
                            break;
                        }
                    }
                    let line = String::from_utf8_lossy(&request)
                        .lines()
                        .next()
                        .unwrap_or("")
                        .to_string();
                    requests.push(line.clone());
                    let (code, payload, content) = if line.contains("/api/capabilities") {
                        (status, body.as_str(), mime.as_str())
                    } else if line.contains("/api/vaults") {
                        (200, r#"{"active":{},"vaults":[]}"#, "application/json")
                    } else {
                        (
                            200,
                            r#"{"phase":"initializing","read_only":true,"verified":0,"generation":1}"#,
                            "application/json",
                        )
                    };
                    if cancel {
                        cancel_worker.store(true, Ordering::Release);
                    }
                    if stall || cancel {
                        thread::sleep(Duration::from_millis(220));
                    }
                    let _ = write!(
                        stream,
                        "HTTP/1.1 {code} Test\r\nContent-Type: {content}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{payload}",
                        payload.len()
                    );
                }
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    thread::sleep(Duration::from_millis(2))
                }
                Err(e) => panic!("{e}"),
            }
        }
        requests
    });
    let result = OwnedServer::start_cancelable(cfg, &canceled);
    let ok = result.is_ok();
    if let Ok(mut server) = result {
        server.shutdown().unwrap();
    }
    done.store(true, Ordering::Release);
    let requests = http.join().unwrap();
    assert_eq!(
        std::fs::read_to_string(fixture.0.join("spawns")).unwrap(),
        "spawn\n",
        "engine respawn/fallback"
    );
    let args = std::fs::read_to_string(fixture.0.join("args")).unwrap();
    assert!(args.contains("--canonical-read-only\n--read-only-cache-root\n"));
    let pid: i32 = std::fs::read_to_string(fixture.0.join("pid"))
        .unwrap()
        .trim()
        .parse()
        .unwrap();
    assert_eq!(
        unsafe { libc::kill(pid, 0) },
        -1,
        "owned child was not reaped"
    );
    (ok, requests, args)
}

#[test]
fn canonical_handshake_precedes_all_reads_and_reaps() {
    let (ok, requests, _) = fake_case(
        200,
        GOOD,
        "application/json; charset=utf-8",
        true,
        false,
        false,
    );
    assert!(ok);
    assert_eq!(
        requests,
        vec![
            "GET /api/capabilities HTTP/1.1",
            "GET /api/vaults HTTP/1.1",
            "GET /api/storage HTTP/1.1"
        ]
    );
}
#[test]
fn canonical_handshake_mismatch_never_reads_or_falls_back() {
    for (status, body, mime) in [
        (404, "missing", "application/json"),
        (
            200,
            r#"{"protocol":"track-canonical-read-only","version":1,"canonical_read_only":false}"#,
            "application/json",
        ),
        (
            200,
            r#"{"protocol":"old","version":1,"canonical_read_only":true}"#,
            "application/json",
        ),
        (
            200,
            r#"{"protocol":"track-canonical-read-only","version":2,"canonical_read_only":true}"#,
            "application/json",
        ),
        (200, "{}", "application/json"),
        (200, GOOD, "text/html"),
        (200, GOOD, "text/plain"),
        (
            200,
            r#"{"protocol":"track-canonical-read-only","version":1.0,"canonical_read_only":true}"#,
            "application/json",
        ),
        (
            200,
            r#"{"protocol":"track-canonical-read-only","version":1,"version":1,"canonical_read_only":true}"#,
            "application/json",
        ),
        (
            200,
            r#"{"protocol":"track-canonical-read-only","version":1,"canonical_read_only":true,"extra":0}"#,
            "application/json",
        ),
    ] {
        let (ok, requests, _) = fake_case(status, body, mime, true, false, false);
        assert!(!ok, "accepted {body}");
        assert_eq!(requests, vec!["GET /api/capabilities HTTP/1.1"]);
    }
    let huge = format!("{}{}", GOOD, " ".repeat(4097));
    let (ok, requests, _) = fake_case(200, &huge, "application/json", true, false, false);
    assert!(!ok);
    assert_eq!(requests.len(), 1);
}
#[test]
fn canonical_handshake_timeout_cancel_and_wrong_ownership_do_not_read_vaults() {
    for (token, stall, cancel) in [
        (true, true, false),
        (true, false, true),
        (false, false, false),
    ] {
        let (ok, requests, _) = fake_case(200, GOOD, "application/json", token, stall, cancel);
        assert!(!ok);
        assert_eq!(requests.len(), if token { 1 } else { 0 });
    }
}
#[test]
fn only_explicit_creation_authority_selects_writable_engine() {
    let (_fixture, mut cfg) = fixture();
    let mut cmd = Command::new("unused");
    cfg.configure_read_mode(&mut cmd).unwrap();
    assert!(cfg.canonical_read_only());
    let Vault::Path(vault) = &cfg.vault else {
        panic!()
    };
    cfg.creation_fixture = Some(
        CreateFixture::capture(vault, vault, cfg.config.as_deref(), cfg.cache.as_deref()).unwrap(),
    );
    let mut cmd = Command::new("unused");
    cfg.configure_read_mode(&mut cmd).unwrap();
    assert!(!cfg.canonical_read_only());
    assert_eq!(cmd.get_args().count(), 0);
}

#[test]
fn old_engine_flag_error_is_terminal_without_legacy_spawn() {
    let (fixture, cfg) = fixture();
    std::fs::write(&cfg.executable,"#!/bin/sh\nprintf 'spawn\\n' >> \"$HOME/../spawns\"\nprintf '%s\\n' \"$@\" > \"$HOME/../args\"\necho 'unknown flag: canonical-read-only' >&2\nexit 2\n").unwrap();
    std::fs::set_permissions(&cfg.executable, std::fs::Permissions::from_mode(0o700)).unwrap();
    assert!(OwnedServer::start(cfg).is_err());
    assert_eq!(
        std::fs::read_to_string(fixture.0.join("spawns")).unwrap(),
        "spawn\n"
    );
    assert!(
        std::fs::read_to_string(fixture.0.join("args"))
            .unwrap()
            .contains("--canonical-read-only\n")
    );
}
