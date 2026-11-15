//! Owned-loopback change awareness. No note access, automatic refresh or follow action.
use std::{
    future::Future,
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, Ordering},
    },
    time::Duration,
};
use tokio::sync::watch;

const FRAME_LIMIT: usize = 16 * 1024;
const IDLE: Duration = Duration::from_secs(60);
const RETRY: Duration = Duration::from_secs(2);

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Snapshot {
    pub change: u64,
    pub data: u64,
    /// Bounded invalidation only; event payloads are never navigation authority.
    pub follow: u64,
    pub gaps: u64,
    /// Subset of gaps before the first accepted stream; total gaps still invalidate follow.
    pub initial_failures: u64,
    pub ever_connected: bool,
    pub connected: bool,
}
impl Snapshot {
    fn event(&mut self, event: &str) {
        match event {
            "change" => self.change = self.change.saturating_add(1),
            "data" => self.data = self.data.saturating_add(1),
            "follow" => self.follow = self.follow.saturating_add(1),
            _ => {} // No event directly navigates.
        }
    }
    fn gap(&mut self) {
        self.connected = false;
        if !self.ever_connected {
            self.initial_failures = self.initial_failures.saturating_add(1);
        }
        self.gaps = self.gaps.saturating_add(1);
    }
}

/// Bounded SSE fields. Partial events at EOF are deliberately discarded.
#[derive(Default)]
struct Parser {
    line: Vec<u8>,
    bytes: usize,
    cr: bool,
    event: String,
    data: bool,
    first: bool,
}
impl Parser {
    fn new() -> Self {
        Self {
            first: true,
            ..Self::default()
        }
    }
    fn feed(&mut self, bytes: &[u8], mut emit: impl FnMut(&str)) -> Result<(), ()> {
        for &byte in bytes {
            if self.cr {
                self.cr = false;
                if byte == b'\n' {
                    continue;
                }
            }
            self.bytes += 1;
            if self.bytes > FRAME_LIMIT {
                return Err(());
            }
            if byte == b'\r' || byte == b'\n' {
                self.cr = byte == b'\r';
                let line = std::str::from_utf8(&self.line).map_err(|_| ())?;
                let line = if self.first {
                    line.strip_prefix('\u{feff}').unwrap_or(line)
                } else {
                    line
                };
                self.first = false;
                if line.is_empty() {
                    if self.data {
                        emit(&self.event);
                    }
                    self.event.clear();
                    self.data = false;
                    self.bytes = 0;
                } else if !line.starts_with(':') {
                    let (field, value) = line.split_once(':').unwrap_or((line, ""));
                    let value = value.strip_prefix(' ').unwrap_or(value);
                    match field {
                        "event" => {
                            self.event.clear();
                            self.event.push_str(value);
                        }
                        "data" => self.data = true,
                        _ => {} // No Last-Event-ID/retry contract; neither is honored.
                    }
                }
                self.line.clear();
            } else {
                self.line.push(byte);
            }
        }
        Ok(())
    }
}

/// Races cancellation before polling work; dropping the future closes its request.
async fn cancelable<F: Future>(cancel: &mut watch::Receiver<bool>, future: F) -> Option<F::Output> {
    if *cancel.borrow() {
        return None;
    }
    let mut future = std::pin::pin!(future);
    let mut changed = std::pin::pin!(cancel.changed());
    std::future::poll_fn(|cx| {
        if changed.as_mut().poll(cx).is_ready() {
            return std::task::Poll::Ready(None);
        }
        future.as_mut().poll(cx).map(Some)
    })
    .await
}

pub struct Observer {
    state: Arc<Mutex<Snapshot>>,
    cancel: watch::Sender<bool>,
    #[cfg_attr(not(test), allow(dead_code))]
    done: Arc<AtomicBool>,
}
impl Observer {
    /// This is called only by Session::from_owned, never from an external URL.
    pub(crate) fn start(origin: &str, alive: Arc<AtomicBool>) -> Self {
        let state = Arc::new(Mutex::new(Snapshot::default()));
        let done = Arc::new(AtomicBool::new(false));
        let (cancel, rx) = watch::channel(false);
        let observer = Self {
            state: state.clone(),
            cancel,
            done: done.clone(),
        };
        let origin = origin.to_owned();
        std::thread::spawn(move || {
            let runtime = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build();
            match runtime {
                Ok(runtime) => runtime.block_on(observe(origin, alive, state, rx)),
                Err(_) => state.lock().unwrap().gap(),
            }
            done.store(true, Ordering::Release);
        });
        observer
    }
    pub fn snapshot(&self) -> Snapshot {
        *self.state.lock().unwrap()
    }
    pub fn cancel(&self) {
        let _ = self.cancel.send(true);
    }
    #[cfg(test)]
    fn is_done(&self) -> bool {
        self.done.load(Ordering::Acquire)
    }
}
impl Drop for Observer {
    fn drop(&mut self) {
        self.cancel();
    }
}

fn endpoint(origin: &str) -> Option<reqwest::Url> {
    let mut url = reqwest::Url::parse(origin).ok()?;
    if url.scheme() != "http"
        || url.host_str() != Some("127.0.0.1")
        || url.port_or_known_default() == Some(0)
        || !url.username().is_empty()
        || url.password().is_some()
        || url.path() != "/"
        || url.query().is_some()
        || url.fragment().is_some()
    {
        return None;
    }
    url.set_path("/api/events");
    Some(url)
}
async fn observe(
    origin: String,
    alive: Arc<AtomicBool>,
    state: Arc<Mutex<Snapshot>>,
    mut cancel: watch::Receiver<bool>,
) {
    let Some(url) = endpoint(&origin) else {
        state.lock().unwrap().gap();
        return;
    };
    let Ok(client) = reqwest::Client::builder()
        .no_proxy()
        .redirect(reqwest::redirect::Policy::none())
        .connect_timeout(Duration::from_secs(2))
        .build()
    else {
        state.lock().unwrap().gap();
        return;
    };
    while alive.load(Ordering::Acquire) && !*cancel.borrow() {
        // No lifetime deadline on an established stream; heartbeat timeout is per chunk.
        let result = cancelable(
            &mut cancel,
            tokio::time::timeout(
                Duration::from_secs(5),
                client
                    .get(url.clone())
                    .header("Accept", "text/event-stream")
                    .send(),
            ),
        )
        .await;
        let Some(result) = result else {
            break;
        };
        if let Ok(Ok(mut response)) = result {
            let content_type = response
                .headers()
                .get(reqwest::header::CONTENT_TYPE)
                .and_then(|v| v.to_str().ok())
                .and_then(|v| v.split(';').next())
                .is_some_and(|v| v.trim().eq_ignore_ascii_case("text/event-stream"));
            if response.status() == reqwest::StatusCode::OK && content_type {
                {
                    let mut snapshot = state.lock().unwrap();
                    snapshot.connected = true;
                    snapshot.ever_connected = true;
                }
                let mut parser = Parser::new();
                while alive.load(Ordering::Acquire) {
                    let Some(chunk) =
                        cancelable(&mut cancel, tokio::time::timeout(IDLE, response.chunk())).await
                    else {
                        state.lock().unwrap().connected = false;
                        return;
                    };
                    match chunk {
                        Ok(Ok(Some(bytes))) => {
                            if parser
                                .feed(&bytes, |event| state.lock().unwrap().event(event))
                                .is_err()
                            {
                                break;
                            }
                        }
                        _ => break,
                    }
                }
            }
        }
        state.lock().unwrap().gap(); // No replay: any break may have missed updates.
        if !alive.load(Ordering::Acquire) {
            break;
        }
        if cancelable(&mut cancel, tokio::time::sleep(RETRY))
            .await
            .is_none()
        {
            break;
        }
    }
    state.lock().unwrap().connected = false;
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        io::{Read, Write},
        net::TcpListener,
        time::Instant,
    };

    #[test]
    fn sse_lines_chunks_comments_and_follow_are_bounded() {
        for newline in ["\n", "\r", "\r\n"] {
            let text = format!(
                "\u{feff}: connected{newline}{newline}event: change{newline}data: {{}}{newline}{newline}: ping{newline}{newline}event: follow{newline}data: 日本語{newline}{newline}event: data{newline}data: {{}}{newline}{newline}"
            );
            for size in 1..=text.len() {
                let mut parser = Parser::new();
                let mut snapshot = Snapshot::default();
                for chunk in text.as_bytes().chunks(size) {
                    parser.feed(chunk, |event| snapshot.event(event)).unwrap();
                }
                assert_eq!((snapshot.change, snapshot.data, snapshot.follow), (1, 1, 1));
            }
        }
    }
    #[test]
    fn partial_frames_unknown_fields_and_limits_never_invent_changes() {
        let mut parser = Parser::new();
        let mut snapshot = Snapshot::default();
        parser.feed(b"event: change\n\nevent: ignored\ndata: x\n\nid: 9\nretry: 1\nevent: change\ndata: x", |e| snapshot.event(e)).unwrap();
        assert_eq!(snapshot.change, 0); // EOF cannot dispatch pending data.
        assert_eq!(snapshot.data, 0);
        assert!(parser.feed(&vec![b'x'; FRAME_LIMIT], |_| {}).is_err());
        assert!(Parser::new().feed(b"event: \xff\n", |_| {}).is_err());
        let mut parser = Parser::new();
        for _ in 0..10_000 {
            parser
                .feed(b": ping\n\n", |_| panic!("comment dispatched"))
                .unwrap();
        }
        assert!(parser.line.is_empty());
        assert_eq!(parser.bytes, 0);
    }
    #[test]
    fn only_exact_owned_loopback_origins_are_accepted() {
        assert_eq!(
            endpoint("http://127.0.0.1:1234").unwrap().as_str(),
            "http://127.0.0.1:1234/api/events"
        );
        for url in [
            "https://127.0.0.1:1234",
            "http://localhost:1234",
            "http://example.com:1234",
            "http://user@127.0.0.1:1234",
            "http://127.0.0.1:1234/other",
            "http://127.0.0.1:1234/?x",
            "http://127.0.0.1:1234/#fragment",
        ] {
            assert!(endpoint(url).is_none(), "{url}");
        }
    }
    fn wait(mut predicate: impl FnMut() -> bool) {
        let end = Instant::now() + Duration::from_secs(5);
        while !predicate() {
            assert!(Instant::now() < end, "timed out");
            std::thread::sleep(Duration::from_millis(5));
        }
    }
    fn accept(listener: &TcpListener) -> std::net::TcpStream {
        listener.set_nonblocking(true).unwrap();
        let mut stream = None;
        wait(|| match listener.accept() {
            Ok((s, _)) => {
                stream = Some(s);
                true
            }
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => false,
            Err(e) => panic!("{e}"),
        });
        let mut stream = stream.unwrap();
        stream.set_nonblocking(false).unwrap();
        stream
            .set_read_timeout(Some(Duration::from_secs(3)))
            .unwrap();
        let mut request = Vec::new();
        while !request.ends_with(b"\r\n\r\n") {
            let mut b = [0];
            stream.read_exact(&mut b).unwrap();
            request.push(b[0]);
            assert!(request.len() < 8192);
        }
        assert!(request.starts_with(b"GET /api/events HTTP/1.1\r\n"));
        stream
    }
    fn headers(stream: &mut std::net::TcpStream) {
        stream.write_all(b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream; charset=utf-8\r\nTransfer-Encoding: chunked\r\n\r\n").unwrap();
    }
    fn chunk(stream: &mut std::net::TcpStream, text: &[u8]) {
        write!(stream, "{:x}\r\n", text.len()).unwrap();
        stream.write_all(text).unwrap();
        stream.write_all(b"\r\n").unwrap();
        stream.flush().unwrap();
    }
    #[test]
    fn idle_stream_cancel_releases_worker_without_waiting_for_heartbeat() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || {
            let mut stream = accept(&listener);
            headers(&mut stream);
            chunk(&mut stream, b": connected\n\nevent: change\ndata: {}\n\n");
            let mut byte = [0];
            assert_eq!(stream.read(&mut byte).unwrap(), 0);
        });
        let observer = Observer::start(&origin, Arc::new(AtomicBool::new(true)));
        wait(|| observer.snapshot().change == 1);
        let start = Instant::now();
        observer.cancel();
        wait(|| observer.is_done());
        assert!(start.elapsed() < Duration::from_secs(2));
        assert!(!observer.snapshot().connected);
        server.join().unwrap();
    }
    #[test]
    fn drop_cancels_pending_headers_without_waiting_for_deadline() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let (ready, received) = std::sync::mpsc::channel();
        let server = std::thread::spawn(move || {
            let mut stream = accept(&listener);
            ready.send(()).unwrap();
            let mut byte = [0];
            assert_eq!(stream.read(&mut byte).unwrap(), 0);
        });
        let observer = Observer::start(&origin, Arc::new(AtomicBool::new(true)));
        received.recv_timeout(Duration::from_secs(3)).unwrap();
        let done = observer.done.clone();
        let start = Instant::now();
        drop(observer);
        wait(|| done.load(Ordering::Acquire));
        assert!(start.elapsed() < Duration::from_secs(2));
        server.join().unwrap();
    }

    #[test]
    fn reconnect_keeps_gaps_and_discards_unterminated_old_frame() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || {
            let mut first = accept(&listener);
            headers(&mut first);
            chunk(
                &mut first,
                b"event: change\ndata: {}\n\nevent: data\ndata: unfinished",
            );
            first.write_all(b"0\r\n\r\n").unwrap();
            drop(first);
            let mut second = accept(&listener);
            headers(&mut second);
            chunk(
                &mut second,
                b"event: data\ndata: {}\n\nevent: follow\ndata: {}\n\n",
            );
            let mut byte = [0];
            assert_eq!(second.read(&mut byte).unwrap(), 0);
        });
        let observer = Observer::start(&origin, Arc::new(AtomicBool::new(true)));
        wait(|| observer.snapshot().data == 1);
        let snapshot = observer.snapshot();
        assert_eq!((snapshot.change, snapshot.data, snapshot.follow), (1, 1, 1));
        assert!(snapshot.connected && snapshot.gaps >= 1);
        observer.cancel();
        wait(|| observer.is_done());
        server.join().unwrap();
    }
    #[test]
    fn initial_503_then_stream_loss_and_rejected_reconnect_keep_total_gaps() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let origin = format!("http://{}", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || {
            accept(&listener)
                .write_all(b"HTTP/1.1 503 Unavailable\r\nContent-Length: 0\r\n\r\n")
                .unwrap();
            let mut first = accept(&listener);
            headers(&mut first);
            chunk(&mut first, b": connected\n\nevent: change\ndata: {}\n\n");
            first.write_all(b"0\r\n\r\n").unwrap();
            drop(first);
            accept(&listener)
                .write_all(b"HTTP/1.1 503 Unavailable\r\nContent-Length: 0\r\n\r\n")
                .unwrap();
            let mut last = accept(&listener);
            headers(&mut last);
            chunk(&mut last, b": connected\n\nevent: data\ndata: {}\n\n");
            let mut byte = [0];
            assert_eq!(last.read(&mut byte).unwrap(), 0);
        });
        let observer = Observer::start(&origin, Arc::new(AtomicBool::new(true)));
        wait(|| observer.snapshot().initial_failures == 1);
        assert_eq!(observer.snapshot().gaps, 1);
        assert!(!observer.snapshot().ever_connected);
        wait(|| observer.snapshot().change == 1);
        wait(|| observer.snapshot().data == 1);
        let snapshot = observer.snapshot();
        assert!(snapshot.connected && snapshot.ever_connected);
        assert_eq!((snapshot.initial_failures, snapshot.gaps), (1, 3));
        observer.cancel();
        wait(|| observer.is_done());
        assert_eq!(observer.snapshot().gaps, 3); // cancellation is not a gap
        server.join().unwrap();
    }
    #[test]
    fn rejected_status_content_type_and_redirect_require_refresh() {
        for response in [
            "HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1:1/never\r\nContent-Length: 0\r\n\r\n",
            "HTTP/1.1 503 Unavailable\r\nContent-Length: 0\r\n\r\n",
            "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 0\r\n\r\n",
        ] {
            let listener = TcpListener::bind("127.0.0.1:0").unwrap();
            let origin = format!("http://{}", listener.local_addr().unwrap());
            let server = std::thread::spawn(move || {
                accept(&listener).write_all(response.as_bytes()).unwrap();
            });
            let observer = Observer::start(&origin, Arc::new(AtomicBool::new(true)));
            wait(|| observer.snapshot().gaps > 0);
            assert!(!observer.snapshot().connected);
            observer.cancel();
            wait(|| observer.is_done());
            server.join().unwrap();
        }
    }
}
