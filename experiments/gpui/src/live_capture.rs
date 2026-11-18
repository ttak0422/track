//! Owned preflight capture with bounded duplex pipes and group cleanup.
use super::*;
use std::os::fd::AsRawFd;
use std::os::unix::process::CommandExt;
fn invalid() -> Error {
    Error::Startup("Owned preflight capture rejected".into())
}

// Nonblocking pipe avoids joining a reader held open by a descendant. Capture is
// an owned process group and is killed/reaped on cancellation, overflow or error.
#[cfg(test)]
pub(super) fn capture_bytes(
    command: &mut Command,
    timeout: Duration,
    canceled: &AtomicBool,
) -> Result<Vec<u8>, Error> {
    capture_exchange(command, timeout, canceled, None)
}
pub(super) fn capture_exchange(
    command: &mut Command,
    timeout: Duration,
    canceled: &AtomicBool,
    payload: Option<Vec<u8>>,
) -> Result<Vec<u8>, Error> {
    if canceled.load(Ordering::Acquire) {
        return Err(Error::Stale);
    }
    if payload.is_some() {
        command.stdin(Stdio::piped());
    }
    command.process_group(0);
    let mut child = command.spawn()?;
    let outcome = (|| {
        let mut request = match payload {
            Some(payload) => Some(request_pipe::RequestPipe::new(
                child.stdin.take().ok_or_else(invalid)?,
                payload,
            )?),
            None => None,
        };
        let mut sent = request.is_none();
        let mut out = child.stdout.take().ok_or_else(invalid)?;
        let fd = out.as_raw_fd();
        // SAFETY: out owns this live pipe descriptor throughout both fcntl calls.
        let flags = unsafe { libc::fcntl(fd, libc::F_GETFL) };
        if flags < 0 {
            return Err(Error::Io(std::io::Error::last_os_error()));
        }
        // SAFETY: only descriptor flags are changed; no pointer argument is used.
        let result = unsafe { libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK) };
        if result < 0 {
            return Err(Error::Io(std::io::Error::last_os_error()));
        }
        let deadline = Instant::now() + timeout;
        let mut bytes = Vec::new();
        loop {
            if canceled.load(Ordering::Acquire) {
                return Err(Error::Stale);
            }
            if Instant::now() >= deadline {
                return Err(Error::Startup("Manifest capture timed out".into()));
            }
            if let Some(request) = &mut request {
                sent = request.pump()?;
            }
            let mut chunk = [0u8; 4096];
            match out.read(&mut chunk) {
                Ok(n) => {
                    bytes.extend_from_slice(&chunk[..n]);
                    if bytes.len() > (128 * 1024) {
                        return Err(Error::Oversized);
                    }
                }
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {}
                Err(e) => return Err(Error::Io(e)),
            }
            if capture_exited(&child)? {
                if !sent {
                    return Err(invalid());
                }
                // Drain only bytes currently available; never wait on inherited pipes.
                loop {
                    match out.read(&mut chunk) {
                        Ok(0) => break,
                        Ok(n) => {
                            bytes.extend_from_slice(&chunk[..n]);
                            if bytes.len() > (128 * 1024) {
                                return Err(Error::Oversized);
                            }
                        }
                        Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                            return Err(invalid());
                        }
                        Err(e) => return Err(Error::Io(e)),
                    }
                }
                return Ok(bytes);
            }
            thread::sleep(Duration::from_millis(5));
        }
    })();
    // Re-establish unreaped ownership before signaling, including error paths.
    if let Err(error) = capture_exited(&child) {
        retain_uncertain_child(child);
        return Err(Error::Shutdown(format!(
            "Owned preflight identity is unconfirmed: {error}"
        )));
    }
    // SAFETY: WNOWAIT just verified the unreaped leader of our exclusive process group.
    unsafe {
        libc::kill(-(child.id() as i32), libc::SIGKILL);
    }
    let status = match child.wait() {
        Ok(status) => status,
        Err(error) => {
            retain_uncertain_child(child);
            return Err(Error::Shutdown(format!(
                "Owned preflight cleanup is unconfirmed: {error}"
            )));
        }
    };
    if outcome.is_ok() && !status.success() {
        return Err(invalid());
    }
    outcome
}
fn retain_uncertain_child(mut child: Child) {
    // A failed wait never grants permission to signal a possibly recycled PID.
    thread::spawn(move || {
        if child.wait().is_err() {
            std::mem::forget(child);
        }
    });
}
fn capture_exited(child: &Child) -> Result<bool, Error> {
    // SAFETY: siginfo_t permits an all-zero representation before waitid fills it.
    let mut info: libc::siginfo_t = unsafe { std::mem::zeroed() };
    // SAFETY: the output pointer is valid; WNOWAIT preserves exact child ownership.
    let result = unsafe {
        libc::waitid(
            libc::P_PID,
            child.id() as libc::id_t,
            &mut info,
            libc::WEXITED | libc::WNOHANG | libc::WNOWAIT,
        )
    };
    if result != 0 {
        return Err(Error::Io(std::io::Error::last_os_error()));
    }
    // SAFETY: successful waitid initialized SIGCHLD data, or left its zero PID for WNOHANG.
    Ok(unsafe { info.si_pid() } == child.id() as i32)
}
