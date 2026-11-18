//! Bounded, nonblocking writes to an already-owned child's stdin pipe.
//! This type neither starts a child nor grants launch authority. Its caller must
//! enforce cancellation/deadlines and drain output while repeatedly pumping.
use std::fmt;
use std::io::{self, Write};
use std::os::fd::AsRawFd;
use std::process::ChildStdin;

const REQUEST_LIMIT: usize = 256 * 1024;
const WRITE_STEP: usize = 8192;

pub(super) struct RequestPipe {
    stdin: Option<ChildStdin>,
    payload: Vec<u8>,
    offset: usize,
    failed: bool,
}
impl fmt::Debug for RequestPipe {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("RequestPipe { values: [redacted] }")
    }
}
impl RequestPipe {
    pub(super) fn new(stdin: ChildStdin, payload: Vec<u8>) -> io::Result<Self> {
        if payload.is_empty() || payload.len() > REQUEST_LIMIT {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                "Invalid request byte bound",
            ));
        }
        // ChildStdin comes from the parent's owned Stdio::piped() handle. Keep
        // existing descriptor flags, changing only this pipe's nonblocking bit.
        let fd = stdin.as_raw_fd();
        let flags = unsafe { libc::fcntl(fd, libc::F_GETFL) };
        if flags < 0 || unsafe { libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK) } < 0 {
            return Err(io::Error::last_os_error());
        }
        Ok(Self {
            stdin: Some(stdin),
            payload,
            offset: 0,
            failed: false,
        })
    }

    /// One write attempt, at most 8192 bytes. A pending/interrupted pipe returns
    /// false so the caller can check its single deadline and drain child output.
    /// Successful completion closes stdin immediately, delivering EOF. An error
    /// also closes stdin and latches failure; a later pump cannot become success.
    pub(super) fn pump(&mut self) -> io::Result<bool> {
        if self.failed {
            return Err(io::Error::new(
                io::ErrorKind::BrokenPipe,
                "Request pipe previously failed",
            ));
        }
        if self.complete() {
            return Ok(true);
        }
        let end = self.payload.len().min(self.offset + WRITE_STEP);
        let result = self
            .stdin
            .as_mut()
            .expect("pending request owns stdin")
            .write(&self.payload[self.offset..end]);
        match result {
            Ok(0) => self.fail(io::Error::new(
                io::ErrorKind::WriteZero,
                "Request pipe made no progress",
            )),
            Ok(n) => {
                self.offset += n;
                if self.offset == self.payload.len() {
                    self.stdin.take();
                }
                Ok(self.complete())
            }
            Err(error)
                if matches!(
                    error.kind(),
                    io::ErrorKind::WouldBlock | io::ErrorKind::Interrupted
                ) =>
            {
                Ok(false)
            }
            Err(error) => self.fail(error),
        }
    }
    fn fail(&mut self, error: io::Error) -> io::Result<bool> {
        self.failed = true;
        self.stdin.take();
        Err(error)
    }
    pub(super) fn complete(&self) -> bool {
        !self.failed && self.offset == self.payload.len() && self.stdin.is_none()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Read;
    use std::process::{Child, ChildStdout, Command, ExitStatus, Stdio};
    use std::thread;
    use std::time::{Duration, Instant};

    // These children never create grandchildren. Cleanup also runs on assertion
    // unwinding, and uses bounded try_wait rather than an unbounded wait/join.
    struct OwnedChild {
        child: Child,
        reaped: bool,
    }
    impl OwnedChild {
        fn spawn(program: &str, args: &[&str]) -> Self {
            let child = Command::new(program)
                .args(args)
                .env_clear()
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::null())
                .spawn()
                .unwrap();
            Self {
                child,
                reaped: false,
            }
        }
        fn status(&mut self) -> io::Result<Option<ExitStatus>> {
            let status = self.child.try_wait()?;
            if status.is_some() {
                self.reaped = true;
            }
            Ok(status)
        }
        fn kill_and_reap(&mut self) -> bool {
            if self.reaped {
                return true;
            }
            let _ = self.child.kill();
            let end = Instant::now() + Duration::from_secs(2);
            while Instant::now() < end {
                match self.status() {
                    Ok(Some(_)) => return true,
                    Err(_) => return false,
                    Ok(None) => thread::sleep(Duration::from_millis(2)),
                }
            }
            false
        }
    }
    impl Drop for OwnedChild {
        fn drop(&mut self) {
            let _ = self.kill_and_reap();
        }
    }
    fn output(child: &mut OwnedChild) -> ChildStdout {
        let out = child.child.stdout.take().unwrap();
        let fd = out.as_raw_fd();
        let flags = unsafe { libc::fcntl(fd, libc::F_GETFL) };
        assert!(flags >= 0);
        assert!(unsafe { libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK) } >= 0);
        out
    }
    fn read_available(out: &mut ChildStdout, bytes: &mut Vec<u8>) -> bool {
        let mut chunk = [0; 8192];
        match out.read(&mut chunk) {
            Ok(0) => true,
            Ok(n) => {
                bytes.extend_from_slice(&chunk[..n]);
                false
            }
            Err(e)
                if matches!(
                    e.kind(),
                    io::ErrorKind::WouldBlock | io::ErrorKind::Interrupted
                ) =>
            {
                false
            }
            Err(e) => panic!("unexpected output error: {e}"),
        }
    }
    fn ready(out: &mut ChildStdout) {
        let mut bytes = Vec::new();
        let deadline = Instant::now() + Duration::from_secs(2);
        while bytes.len() < 5 {
            assert!(
                Instant::now() < deadline,
                "synthetic child did not become ready"
            );
            read_available(out, &mut bytes);
            thread::sleep(Duration::from_millis(2));
        }
        assert_eq!(bytes, b"ready");
    }

    #[test]
    fn large_request_and_echo_progress_together_and_eof_reaps_child() {
        let mut child = OwnedChild::spawn("/bin/cat", &[]);
        let mut out = output(&mut child);
        let payload: Vec<u8> = (0..REQUEST_LIMIT).map(|n| (n % 251) as u8).collect();
        let mut pipe =
            RequestPipe::new(child.child.stdin.take().unwrap(), payload.clone()).unwrap();
        assert_eq!(format!("{pipe:?}"), "RequestPipe { values: [redacted] }");
        let mut bytes = Vec::new();
        let deadline = Instant::now() + Duration::from_secs(5);
        let mut eof = false;
        while !eof || !child.reaped {
            assert!(Instant::now() < deadline, "bounded pipe exchange timed out");
            pipe.pump().unwrap();
            eof |= read_available(&mut out, &mut bytes);
            assert!(bytes.len() <= REQUEST_LIMIT);
            if let Some(status) = child.status().unwrap() {
                assert!(status.success());
            }
            thread::sleep(Duration::from_millis(1));
        }
        assert!(pipe.complete());
        assert!(pipe.pump().unwrap());
        assert_eq!(bytes, payload);
    }
    #[test]
    fn nonreading_child_never_blocks_pump() {
        let mut child = OwnedChild::spawn("/bin/sh", &["-c", "printf ready; kill -STOP $$"]);
        let mut out = output(&mut child);
        ready(&mut out);
        let mut pipe =
            RequestPipe::new(child.child.stdin.take().unwrap(), vec![b'x'; REQUEST_LIMIT]).unwrap();
        let start = Instant::now();
        for _ in 0..128 {
            // Completion is also legal on a platform with an unusually large
            // pipe buffer. Neither progress nor backpressure may block here.
            pipe.pump().unwrap();
        }
        assert!(start.elapsed() < Duration::from_secs(2));
        drop(pipe);
        assert!(child.kill_and_reap());
    }
    #[test]
    fn closed_child_stdin_fails_and_cannot_become_completed() {
        let mut child =
            OwnedChild::spawn("/bin/sh", &["-c", "exec 0<&-; printf ready; kill -STOP $$"]);
        let mut out = output(&mut child);
        ready(&mut out);
        let mut pipe =
            RequestPipe::new(child.child.stdin.take().unwrap(), b"request".to_vec()).unwrap();
        assert_eq!(pipe.pump().unwrap_err().kind(), io::ErrorKind::BrokenPipe);
        assert!(!pipe.complete());
        assert!(pipe.pump().is_err());
        assert!(child.kill_and_reap());
    }
    #[test]
    fn empty_or_oversized_requests_are_rejected_and_close_stdin() {
        for payload in [Vec::new(), vec![0; REQUEST_LIMIT + 1]] {
            let mut child = OwnedChild::spawn("/bin/cat", &[]);
            let stdin = child.child.stdin.take().unwrap();
            assert_eq!(
                RequestPipe::new(stdin, payload).unwrap_err().kind(),
                io::ErrorKind::InvalidInput
            );
            let deadline = Instant::now() + Duration::from_secs(2);
            loop {
                assert!(
                    Instant::now() < deadline,
                    "rejected request did not close stdin"
                );
                if let Some(status) = child.status().unwrap() {
                    assert!(status.success());
                    break;
                }
                thread::sleep(Duration::from_millis(2));
            }
        }
    }
}
