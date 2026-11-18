//! Bounded local math rasterization. Only the owned early-mode child renders.
use std::time::{Duration, Instant};
static WORKER: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);
struct Permit;
impl Drop for Permit {
    fn drop(&mut self) {
        WORKER.store(false, std::sync::atomic::Ordering::Release);
    }
}
pub const MAX_INPUT: usize = 4096;
pub const MAX_OUTPUT: usize = 2 * 1024 * 1024;
pub const MAX_EDGE: u32 = 1536;
pub const MAX_PIXELS: u64 = 2_000_000;
pub const DEADLINE: Duration = Duration::from_secs(2);

pub fn admit(bytes: &[u8]) -> Result<&str, &'static str> {
    let source = std::str::from_utf8(bytes).map_err(|_| "Math input is not UTF-8")?;
    if bytes.len() > MAX_INPUT
        || source.trim().is_empty()
        || source
            .chars()
            .any(|c| c.is_control() && !matches!(c, '\n' | '\r' | '\t'))
    {
        return Err("Math input rejected");
    }
    Ok(source)
}
fn bounded_dimensions(width: u32, height: u32) -> bool {
    width > 0
        && height > 0
        && width <= MAX_EDGE
        && height <= MAX_EDGE
        && u64::from(width) * u64::from(height) <= MAX_PIXELS
}
fn validate_png(bytes: Vec<u8>) -> Result<Vec<u8>, String> {
    // Check the fixed IHDR before handing any bytes to an image decoder.
    if bytes.len() < 33
        || bytes.len() > MAX_OUTPUT
        || &bytes[..8] != b"\x89PNG\r\n\x1a\n"
        || &bytes[8..16] != b"\0\0\0\rIHDR"
    {
        return Err("Math image rejected".into());
    }
    let width = u32::from_be_bytes(bytes[16..20].try_into().unwrap());
    let height = u32::from_be_bytes(bytes[20..24].try_into().unwrap());
    if !bounded_dimensions(width, height) {
        return Err("Math image dimensions rejected".into());
    }
    Ok(bytes)
}
/// Never called in the UI process. Size is checked before the raster allocation.
fn render(bytes: &[u8]) -> Result<Vec<u8>, String> {
    use latex_rust::{
        Dim, MathFont, MathStyle, ParseOptions, PngBackground, PngOptions, layout_with_max_depth,
        parse_with_options, render_png,
    };
    let source = admit(bytes).map_err(str::to_owned)?;
    let (ast, _) = parse_with_options(source, &ParseOptions::new().with_max_depth(24))
        .map_err(|e| e.to_string())?;
    let font = MathFont::stix_two_math().map_err(|e| e.to_string())?;
    let tree =
        layout_with_max_depth(&ast, &font, MathStyle::Display, 24).map_err(|e| e.to_string())?;
    // Match the renderer's 12pt / 144dpi defaults: exactly 24 pixels per em.
    let width = (&tree.width * Dim::from_i64(24))
        .ceil_to_u32()
        .map_err(|e| e.to_string())?
        .max(1);
    let height = ((&tree.height + &tree.depth) * Dim::from_i64(24))
        .ceil_to_u32()
        .map_err(|e| e.to_string())?
        .max(1);
    if !bounded_dimensions(width, height) {
        return Err("Math layout exceeds display limits".into());
    }
    let mut options = PngOptions::new();
    options.display = true;
    options.background = PngBackground::White;
    validate_png(render_png(&tree, &font, &options).map_err(|e| e.to_string())?)
}
#[cfg(unix)]
pub fn run(bytes: &[u8], cancel: &std::sync::atomic::AtomicBool) -> Result<Vec<u8>, String> {
    use std::process::Command;
    let mut command = Command::new(std::env::current_exe().map_err(|e| e.to_string())?);
    command.arg("--internal-math-raster");
    run_command(command, bytes, cancel, DEADLINE)
}
#[cfg(unix)]
fn run_command(
    mut command: std::process::Command,
    bytes: &[u8],
    cancel: &std::sync::atomic::AtomicBool,
    deadline: Duration,
) -> Result<Vec<u8>, String> {
    use std::{
        io::{Read, Write},
        os::fd::AsRawFd,
        process::{Child, Stdio},
        sync::atomic::Ordering,
    };
    admit(bytes).map_err(str::to_owned)?;
    if WORKER
        .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
        .is_err()
    {
        return Err("Math worker busy".into());
    }
    let _permit = Permit;
    if cancel.load(Ordering::Acquire) {
        return Err("Math cancelled".into());
    }
    struct ChildGuard(Child);
    impl Drop for ChildGuard {
        fn drop(&mut self) {
            let _ = self.0.kill();
            let _ = self.0.wait();
        }
    }
    fn nonblock(fd: i32) -> std::io::Result<()> {
        unsafe {
            let flags = libc::fcntl(fd, libc::F_GETFL);
            if flags < 0 || libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK) < 0 {
                return Err(std::io::Error::last_os_error());
            }
        }
        Ok(())
    }
    let started = Instant::now();
    let mut child = ChildGuard(
        command
            .env_clear()
            .current_dir("/")
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|e| e.to_string())?,
    );
    let mut input = Some(child.0.stdin.take().ok_or("Math stdin missing")?);
    let mut output = child.0.stdout.take().ok_or("Math stdout missing")?;
    nonblock(input.as_ref().unwrap().as_raw_fd()).map_err(|e| e.to_string())?;
    nonblock(output.as_raw_fd()).map_err(|e| e.to_string())?;
    let mut sent = 0;
    let mut received = Vec::new();
    let mut eof = false;
    let mut exited = None;
    loop {
        if cancel.load(Ordering::Acquire) {
            return Err("Math cancelled".into());
        }
        if started.elapsed() >= deadline {
            return Err("Math render deadline exceeded".into());
        }
        if let Some(stdin) = input.as_mut() {
            match stdin.write(&bytes[sent..]) {
                Ok(0) => return Err("Math input closed".into()),
                Ok(n) => sent += n,
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {}
                Err(e) => return Err(e.to_string()),
            }
            if sent == bytes.len() {
                input = None;
            }
        }
        if !eof {
            let mut chunk = [0; 16384];
            match output.read(&mut chunk) {
                Ok(0) => eof = true,
                Ok(n) => {
                    if received.len() + n > MAX_OUTPUT {
                        return Err("Math output limit exceeded".into());
                    }
                    received.extend_from_slice(&chunk[..n]);
                }
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {}
                Err(e) => return Err(e.to_string()),
            }
        }
        if exited.is_none() {
            exited = child.0.try_wait().map_err(|e| e.to_string())?;
        }
        if let Some(status) = exited {
            if !status.success() {
                return Err("Math helper rejected document".into());
            }
            if eof {
                if sent != bytes.len() {
                    return Err("Math helper did not consume input".into());
                }
                return validate_png(received);
            }
        }
        std::thread::sleep(Duration::from_millis(2));
    }
}

/// Dispatch before UI, engine, vault or session initialization.
#[cfg(unix)]
pub fn early_helper(args: &[String]) -> Option<Result<(), String>> {
    if args.first().map(String::as_str) != Some("--internal-math-raster") {
        return None;
    }
    Some((|| {
        use std::io::{Read, Write};
        if args.len() != 1 {
            return Err("Math helper arguments rejected".into());
        }
        let limit = libc::rlimit {
            rlim_cur: 2,
            rlim_max: 2,
        };
        if unsafe { libc::setrlimit(libc::RLIMIT_CPU, &limit) } != 0 {
            return Err("Math CPU limit unavailable".into());
        }
        let mut bytes = Vec::new();
        std::io::stdin()
            .lock()
            .take(MAX_INPUT as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(|e| e.to_string())?;
        let png = render(&bytes)?;
        std::io::stdout()
            .lock()
            .write_all(&png)
            .map_err(|e| e.to_string())
    })())
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn admission() {
        assert!(admit(b"x^2").is_ok());
        for bytes in [vec![], vec![b'x'; MAX_INPUT + 1], vec![0], vec![255]] {
            assert!(admit(&bytes).is_err());
        }
    }
    #[test]
    fn actual_typesetting() {
        for source in [
            r"\frac{a+b}{c}",
            r"x_i^2+e^{-x}",
            r"\sqrt{x^2+y^2}",
            r"\begin{pmatrix}a&b\\c&d\end{pmatrix}",
        ] {
            assert!(render(source.as_bytes()).is_ok(), "{source}");
        }
    }
    #[test]
    fn unsupported_and_depth_remain_errors() {
        for source in [
            r"\input{/etc/passwd}",
            r"\write18{whoami}",
            r"\includegraphics{https://example.com/x}",
            r"\frac{",
        ] {
            assert!(render(source.as_bytes()).is_err(), "{source}");
        }
        let deep = "{".repeat(32) + "x" + &"}".repeat(32);
        assert!(render(deep.as_bytes()).is_err());
    }
    #[test]
    fn pixel_budget_precedes_raster() {
        assert!(!bounded_dimensions(0, 1));
        assert!(!bounded_dimensions(1537, 1));
        assert!(!bounded_dimensions(1536, 1536));
        assert!(render("x".repeat(2000).as_bytes()).is_err());
    }
    #[test]
    fn malformed_child_image() {
        assert!(validate_png(vec![0; 33]).is_err());
        let png = render(b"x").unwrap();
        let mut too_wide = png.clone();
        too_wide[16..20].copy_from_slice(&(MAX_EDGE + 1).to_be_bytes());
        assert!(validate_png(too_wide).is_err());
        assert!(validate_png(png[..20].to_vec()).is_err());
    }
}

#[cfg(all(test, unix))]
mod process_tests {
    use super::*;
    #[test]
    fn owned_helper_deadline_failure_overflow_and_cancel() {
        use std::{
            process::Command,
            sync::atomic::{AtomicBool, Ordering},
        };
        fn command(script: &str) -> Command {
            let mut c = Command::new("/bin/sh");
            c.args(["-c", script]);
            c
        }
        let cancel = AtomicBool::new(false);
        // Shell builtins only, no grandchildren inheriting the output pipe.
        let start = Instant::now();
        assert!(
            run_command(
                command("while :; do :; done"),
                b"x",
                &cancel,
                Duration::from_millis(100)
            )
            .unwrap_err()
            .contains("deadline")
        );
        assert!(start.elapsed() < Duration::from_secs(2));
        assert!(run_command(command("exit 7"), b"x", &cancel, Duration::from_secs(1)).is_err());
        cancel.store(true, Ordering::Release);
        assert!(
            run_command(command("exit 0"), b"x", &cancel, Duration::from_secs(1))
                .unwrap_err()
                .contains("cancelled")
        );
        cancel.store(false, Ordering::Release);
        // Malformed bounded output is rejected after a successful helper exit.
        assert!(
            run_command(
                command("printf malformed"),
                b"x",
                &cancel,
                Duration::from_secs(1)
            )
            .is_err()
        );
        // A child that does not consume input remains deadline-bounded.
        let mut large = b"x".to_vec();
        large.resize(MAX_INPUT, b'x');
        assert!(
            run_command(
                command("while :; do :; done"),
                &large,
                &cancel,
                Duration::from_millis(100)
            )
            .unwrap_err()
            .contains("deadline")
        );
        std::thread::scope(|scope| {
            scope.spawn(|| {
                std::thread::sleep(Duration::from_millis(40));
                cancel.store(true, Ordering::Release);
            });
            assert!(
                run_command(
                    command("while :; do :; done"),
                    &large,
                    &cancel,
                    Duration::from_secs(2)
                )
                .unwrap_err()
                .contains("cancelled")
            );
        });
        cancel.store(false, Ordering::Release);
        // Bounded reads reject an overproducing child without retaining excess bytes.
        let script = format!(
            "chunk='{}'; while :; do printf '%s' \"$chunk\"; done",
            "a".repeat(4096)
        );
        assert!(
            run_command(command(&script), b"x", &cancel, Duration::from_secs(5))
                .unwrap_err()
                .contains("output limit")
        );
        assert!(!WORKER.load(Ordering::Acquire));
    }
}
