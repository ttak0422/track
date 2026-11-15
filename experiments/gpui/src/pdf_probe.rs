//! Bounded inert PDF raster helper. No asset fetch, cache, or document actions.
use std::time::{Duration, Instant};
static WORKER: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);
struct Permit;
impl Drop for Permit {
    fn drop(&mut self) {
        WORKER.store(false, std::sync::atomic::Ordering::Release);
    }
}
pub const MAX_INPUT: usize = 8 * 1024 * 1024;
pub const MAX_PAGES: u32 = 256;
pub const MAX_EDGE: u32 = 1536;
pub const MAX_PIXELS: u64 = 2_000_000;
pub const HEADER: usize = 24;
pub const MAX_OUTPUT: usize = HEADER + MAX_PIXELS as usize * 4;
pub const DEADLINE: Duration = Duration::from_secs(5);

pub fn admit(bytes: &[u8], page: u32, edge: u32) -> Result<(), &'static str> {
    if bytes.len() > MAX_INPUT || !bytes.starts_with(b"%PDF-") {
        return Err("PDF input rejected");
    }
    if page == 0 || page > MAX_PAGES || edge == 0 || edge > MAX_EDGE {
        return Err("PDF request rejected");
    }
    Ok(())
}
/// Reject abnormal author-space geometry before allocating a bitmap. Rotation
/// swaps axes; CoreGraphics' drawing transform handles crop origin and rotation.
pub fn dimensions(
    width: f64,
    height: f64,
    rotation: i32,
    edge: u32,
) -> Result<(u32, u32), &'static str> {
    if !width.is_finite()
        || !height.is_finite()
        || width <= 0.
        || height <= 0.
        || width > 14400.
        || height > 14400.
        || edge == 0
        || edge > MAX_EDGE
        || rotation % 90 != 0
    {
        return Err("PDF geometry rejected");
    }
    let (w, h) = if rotation.rem_euclid(180) != 0 {
        (height, width)
    } else {
        (width, height)
    };
    let scale = (edge as f64 / w.max(h)).min((MAX_PIXELS as f64 / (w * h)).sqrt());
    let (w, h) = (
        (w * scale).floor().max(1.) as u32,
        (h * scale).floor().max(1.) as u32,
    );
    if u64::from(w) * u64::from(h) > MAX_PIXELS {
        return Err("PDF pixels rejected");
    }
    Ok((w, h))
}
/// Header: magic PDF1, total pages, displayed page, width, height, byte length;
/// all integers little-endian. Payload is opaque premultiplied BGRA, one frame.
/// Counters are validated together with pixels, never accepted ahead of them.
#[derive(Debug)]
pub struct Frame {
    pub pages: u32,
    pub page: u32,
    pub width: u32,
    pub height: u32,
    pub bgra: Vec<u8>,
}
pub fn frame(mut output: Vec<u8>, requested: u32, edge: u32) -> Result<Frame, &'static str> {
    if output.len() < HEADER
        || output.len() > MAX_OUTPUT
        || &output[..4] != b"PDF1"
        || edge == 0
        || edge > MAX_EDGE
    {
        return Err("PDF response rejected");
    }
    let n = |at| u32::from_le_bytes(output[at..at + 4].try_into().unwrap());
    let (pages, page, width, height, len) = (n(4), n(8), n(12), n(16), n(20));
    let pixels = u64::from(width) * u64::from(height);
    if pages == 0
        || pages > MAX_PAGES
        || page != requested
        || page == 0
        || page > pages
        || width == 0
        || height == 0
        || width > edge
        || height > edge
        || pixels > MAX_PIXELS
        || u64::from(len) != pixels * 4
        || output.len() != HEADER + len as usize
    {
        return Err("PDF frame bounds rejected");
    }
    let bgra = output.split_off(HEADER);
    Ok(Frame {
        pages,
        page,
        width,
        height,
        bgra,
    })
}

/// Owned early-mode helper only; its command-line entry point must exist before
/// this is wired. Nonblocking pipes keep cancellation effective during input,
/// parser hangs and output. ChildGuard kills/reaps on every unsuccessful exit.
#[cfg(unix)]
pub fn run(
    bytes: &[u8],
    page: u32,
    edge: u32,
    cancel: &std::sync::atomic::AtomicBool,
) -> Result<Frame, String> {
    use std::process::Command;
    let mut command = Command::new(std::env::current_exe().map_err(|e| e.to_string())?);
    command.args([
        "--internal-pdf-raster",
        &page.to_string(),
        &edge.to_string(),
    ]);
    run_command(command, bytes, page, edge, cancel, DEADLINE)
}
#[cfg(unix)]
fn run_command(
    mut command: std::process::Command,
    bytes: &[u8],
    page: u32,
    edge: u32,
    cancel: &std::sync::atomic::AtomicBool,
    deadline: Duration,
) -> Result<Frame, String> {
    use std::{
        io::{Read, Write},
        os::fd::AsRawFd,
        process::{Child, Stdio},
        sync::atomic::Ordering,
    };
    admit(bytes, page, edge).map_err(str::to_owned)?;
    if WORKER
        .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
        .is_err()
    {
        return Err("PDF worker busy".into());
    }
    let _permit = Permit;
    if cancel.load(Ordering::Acquire) {
        return Err("PDF cancelled".into());
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
    let mut input = Some(child.0.stdin.take().ok_or("PDF stdin missing")?);
    let mut output = child.0.stdout.take().ok_or("PDF stdout missing")?;
    nonblock(input.as_ref().unwrap().as_raw_fd()).map_err(|e| e.to_string())?;
    nonblock(output.as_raw_fd()).map_err(|e| e.to_string())?;
    let mut sent = 0;
    let mut received = Vec::new();
    let mut eof = false;
    let mut exited = None;
    loop {
        if cancel.load(Ordering::Acquire) {
            return Err("PDF cancelled".into());
        }
        if started.elapsed() >= deadline {
            return Err("PDF render deadline exceeded".into());
        }
        if let Some(stdin) = input.as_mut() {
            match stdin.write(&bytes[sent..]) {
                Ok(0) => return Err("PDF input closed".into()),
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
                        return Err("PDF output limit exceeded".into());
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
                return Err("PDF helper rejected document".into());
            }
            if eof {
                if sent != bytes.len() {
                    return Err("PDF helper did not consume input".into());
                }
                return frame(received, page, edge).map_err(str::to_owned);
            }
        }
        std::thread::sleep(Duration::from_millis(2));
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn admission_bounds() {
        assert!(admit(b"%PDF-1.7", 1, 1200).is_ok());
        assert!(admit(b"not pdf", 1, 100).is_err());
        assert!(admit(b"%PDF-", 0, 100).is_err());
        assert!(admit(b"%PDF-", 1, MAX_EDGE + 1).is_err());
    }
    #[test]
    fn finite_bounded_portrait_landscape() {
        assert_eq!(dimensions(600., 800., 0, 1200).unwrap(), (900, 1200));
        assert_eq!(dimensions(600., 800., 90, 1200).unwrap(), (1200, 900));
        assert!(dimensions(f64::NAN, 10., 0, 100).is_err());
        assert!(dimensions(10., f64::INFINITY, 0, 100).is_err());
        assert!(dimensions(14401., 10., 0, 100).is_err());
        let (w, h) = dimensions(100., 100., 0, 1536).unwrap();
        assert!(u64::from(w) * u64::from(h) <= MAX_PIXELS);
    }
    fn wire() -> Vec<u8> {
        let mut b = b"PDF1".to_vec();
        for n in [3u32, 1, 2, 2, 16] {
            b.extend(n.to_le_bytes());
        }
        b.extend([255; 16]);
        b
    }
    #[test]
    fn counters_and_payload_are_atomic() {
        assert!(frame(wire(), 1, 100).is_ok());
        assert!(frame(wire(), 2, 100).is_err());
        let mut b = wire();
        b.push(0);
        assert!(frame(b, 1, 100).is_err());
        let mut b = wire();
        b[4..8].copy_from_slice(&257u32.to_le_bytes());
        assert!(frame(b, 1, 100).is_err());
        let mut b = wire();
        b.pop();
        assert!(frame(b, 1, 100).is_err());
    }
}

/// Call before logger, UI, vault, session or engine initialization. This mode has
/// no filesystem input argument; its sole input is a bounded anonymous pipe.
#[cfg(target_os = "macos")]
pub fn early_helper(args: &[String]) -> Option<Result<(), String>> {
    if args.first().map(String::as_str) != Some("--internal-pdf-raster") {
        return None;
    }
    Some((|| {
        use std::io::{Read, Write};
        if args.len() != 3 {
            return Err("PDF helper arguments rejected".into());
        }
        let page = args[1].parse::<u32>().map_err(|_| "PDF page rejected")?;
        let edge = args[2].parse::<u32>().map_err(|_| "PDF edge rejected")?;
        // Secondary CPU bound. Wall timeout remains the supervising parent's job.
        let limit = libc::rlimit {
            rlim_cur: 3,
            rlim_max: 3,
        };
        if unsafe { libc::setrlimit(libc::RLIMIT_CPU, &limit) } != 0 {
            return Err("PDF CPU limit unavailable".into());
        }
        let mut bytes = Vec::new();
        std::io::stdin()
            .lock()
            .take(MAX_INPUT as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| "PDF input failed")?;
        admit(&bytes, page, edge).map_err(str::to_owned)?;
        let f = mac::render(&bytes, page, edge)?;
        let mut output = std::io::stdout().lock();
        output.write_all(b"PDF1").map_err(|_| "PDF output failed")?;
        for n in [f.pages, f.page, f.width, f.height, f.bgra.len() as u32] {
            output
                .write_all(&n.to_le_bytes())
                .map_err(|_| "PDF output failed")?;
        }
        output.write_all(&f.bgra).map_err(|_| "PDF output failed")?;
        Ok(())
    })())
}
#[cfg(target_os = "macos")]
mod mac {
    use super::*;
    use std::ffi::c_void;
    type Ref = *mut c_void;
    #[repr(C)]
    #[derive(Clone, Copy)]
    struct Point {
        x: f64,
        y: f64,
    }
    #[repr(C)]
    #[derive(Clone, Copy)]
    struct Size {
        width: f64,
        height: f64,
    }
    #[repr(C)]
    #[derive(Clone, Copy)]
    struct Rect {
        origin: Point,
        size: Size,
    }
    #[repr(C)]
    #[derive(Clone, Copy)]
    struct Transform {
        a: f64,
        b: f64,
        c: f64,
        d: f64,
        tx: f64,
        ty: f64,
    }
    #[link(name = "CoreGraphics", kind = "framework")]
    unsafe extern "C" {
        fn CGDataProviderCreateWithData(
            info: Ref,
            data: *const c_void,
            size: usize,
            release: Option<unsafe extern "C" fn(Ref, *const c_void, usize)>,
        ) -> Ref;
        fn CGDataProviderRelease(p: Ref);
        fn CGPDFDocumentCreateWithProvider(p: Ref) -> Ref;
        fn CGPDFDocumentRelease(p: Ref);
        fn CGPDFDocumentIsEncrypted(p: Ref) -> bool;
        fn CGPDFDocumentGetNumberOfPages(p: Ref) -> usize;
        fn CGPDFDocumentGetPage(p: Ref, page: usize) -> Ref;
        fn CGPDFPageGetBoxRect(p: Ref, which: i32) -> Rect;
        fn CGPDFPageGetRotationAngle(p: Ref) -> i32;
        fn CGPDFPageGetDrawingTransform(
            p: Ref,
            which: i32,
            rect: Rect,
            rotate: i32,
            preserve: bool,
        ) -> Transform;
        fn CGColorSpaceCreateDeviceRGB() -> Ref;
        fn CGColorSpaceRelease(p: Ref);
        fn CGBitmapContextCreate(
            data: Ref,
            width: usize,
            height: usize,
            bits: usize,
            stride: usize,
            space: Ref,
            info: u32,
        ) -> Ref;
        fn CGContextRelease(p: Ref);
        fn CGContextSetRGBFillColor(p: Ref, r: f64, g: f64, b: f64, a: f64);
        fn CGContextFillRect(p: Ref, rect: Rect);
        fn CGContextScaleCTM(p: Ref, x: f64, y: f64);
        fn CGContextConcatCTM(p: Ref, t: Transform);
        fn CGContextDrawPDFPage(p: Ref, page: Ref);
    }
    struct Owned(Ref, unsafe extern "C" fn(Ref));
    impl Drop for Owned {
        fn drop(&mut self) {
            unsafe { (self.1)(self.0) }
        }
    }
    fn owned(p: Ref, release: unsafe extern "C" fn(Ref)) -> Result<Owned, String> {
        if p.is_null() {
            Err("PDF native allocation or parsing failed".into())
        } else {
            Ok(Owned(p, release))
        }
    }
    pub(super) fn render(bytes: &[u8], requested: u32, edge: u32) -> Result<Frame, String> {
        unsafe {
            // The data provider borrows bytes. Document and provider are released in
            // reverse declaration order before this borrowed slice leaves the function.
            let provider = owned(
                CGDataProviderCreateWithData(
                    std::ptr::null_mut(),
                    bytes.as_ptr().cast(),
                    bytes.len(),
                    None,
                ),
                CGDataProviderRelease,
            )?;
            let document = owned(
                CGPDFDocumentCreateWithProvider(provider.0),
                CGPDFDocumentRelease,
            )?;
            if CGPDFDocumentIsEncrypted(document.0) {
                return Err("Encrypted PDF is unsupported".into());
            }
            let pages = CGPDFDocumentGetNumberOfPages(document.0);
            if pages == 0
                || pages > MAX_PAGES as usize
                || requested == 0
                || requested as usize > pages
            {
                return Err("PDF page count or selection rejected".into());
            }
            let page = CGPDFDocumentGetPage(document.0, requested as usize);
            if page.is_null() {
                return Err("PDF page missing".into());
            }
            let crop = CGPDFPageGetBoxRect(page, 1);
            if !crop.origin.x.is_finite() || !crop.origin.y.is_finite() {
                return Err("PDF crop origin rejected".into());
            }
            let rotation = CGPDFPageGetRotationAngle(page);
            let (width, height) = dimensions(crop.size.width, crop.size.height, rotation, edge)
                .map_err(str::to_owned)?;
            let mut bgra = vec![0u8; width as usize * height as usize * 4];
            let colors = owned(CGColorSpaceCreateDeviceRGB(), CGColorSpaceRelease)?;
            // kCGBitmapByteOrder32Little | kCGImageAlphaPremultipliedFirst.
            let context = owned(
                CGBitmapContextCreate(
                    bgra.as_mut_ptr().cast(),
                    width as usize,
                    height as usize,
                    8,
                    width as usize * 4,
                    colors.0,
                    0x2002,
                ),
                CGContextRelease,
            )?;
            let rect = Rect {
                origin: Point { x: 0., y: 0. },
                size: Size {
                    width: width as f64,
                    height: height as f64,
                },
            };
            CGContextSetRGBFillColor(context.0, 1., 1., 1., 1.);
            CGContextFillRect(context.0, rect);
            let (w, h) = if rotation.rem_euclid(180) != 0 {
                (crop.size.height, crop.size.width)
            } else {
                (crop.size.width, crop.size.height)
            };
            let scale = (width as f64 / w).min(height as f64 / h);
            CGContextScaleCTM(context.0, scale, scale);
            let target = Rect {
                origin: Point { x: 0., y: 0. },
                size: Size {
                    width: width as f64 / scale,
                    height: height as f64 / scale,
                },
            };
            let transform = CGPDFPageGetDrawingTransform(page, 1, target, 0, true);
            if ![
                transform.a,
                transform.b,
                transform.c,
                transform.d,
                transform.tx,
                transform.ty,
            ]
            .iter()
            .all(|v| v.is_finite())
            {
                return Err("PDF transform rejected".into());
            }
            CGContextConcatCTM(context.0, transform);
            // Raster drawing only: never inspect/dispatch actions, URLs, annotations,
            // scripts, forms or embedded attachments. Not a security sandbox boundary.
            CGContextDrawPDFPage(context.0, page);
            drop(context);
            Ok(Frame {
                pages: pages as u32,
                page: requested,
                width,
                height,
                bgra,
            })
        }
    }
}

#[cfg(all(test, unix))]
mod lifecycle_tests {
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
                b"%PDF-1.7",
                1,
                100,
                &cancel,
                Duration::from_millis(100)
            )
            .unwrap_err()
            .contains("deadline")
        );
        assert!(start.elapsed() < Duration::from_secs(2));
        assert!(
            run_command(
                command("exit 7"),
                b"%PDF-1.7",
                1,
                100,
                &cancel,
                Duration::from_secs(1)
            )
            .is_err()
        );
        cancel.store(true, Ordering::Release);
        assert!(
            run_command(
                command("exit 0"),
                b"%PDF-1.7",
                1,
                100,
                &cancel,
                Duration::from_secs(1)
            )
            .unwrap_err()
            .contains("cancelled")
        );
        cancel.store(false, Ordering::Release);
        // Malformed bounded output is rejected after a successful helper exit.
        assert!(
            run_command(
                command("printf malformed"),
                b"%PDF-1.7",
                1,
                100,
                &cancel,
                Duration::from_secs(1)
            )
            .is_err()
        );
        // A full pipe must not make write_all block past the supervisor deadline.
        let mut large = b"%PDF-1.7".to_vec();
        large.resize(1024 * 1024, 0);
        assert!(
            run_command(
                command("while :; do :; done"),
                &large,
                1,
                100,
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
                    1,
                    100,
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
            run_command(
                command(&script),
                b"%PDF-1.7",
                1,
                100,
                &cancel,
                Duration::from_secs(5)
            )
            .unwrap_err()
            .contains("output limit")
        );
        assert!(!WORKER.load(Ordering::Acquire));
    }
}
