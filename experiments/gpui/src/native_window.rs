//! Public AppKit adaptation for the GPUI window created by this process.
//! No global settings, private selectors, other windows, or background-thread UI.

#[cfg(target_os = "macos")]
#[allow(unexpected_cfgs)] // objc 0.2 macros probe their historical cargo-clippy feature.
pub fn configure(window: &gpui::Window) -> Result<(), &'static str> {
    use cocoa::appkit::NSWindowButton;
    use cocoa::base::{BOOL, NO, YES, id, nil};
    use objc::{class, msg_send, sel, sel_impl};
    use raw_window_handle::{HasWindowHandle, RawWindowHandle};

    // SAFETY: NSThread's documented class query is safe before acquiring any UI
    // pointer. All remaining AppKit operations are refused off the main thread.
    let on_main_thread: BOOL = unsafe { msg_send![class!(NSThread), isMainThread] };
    if on_main_thread != YES {
        return Err("native window configuration requires the AppKit main thread");
    }
    let handle = HasWindowHandle::window_handle(window)
        .map_err(|_| "GPUI did not provide a native window handle")?;
    let RawWindowHandle::AppKit(handle) = handle.as_raw() else {
        return Err("GPUI returned a non-AppKit handle on macOS");
    };
    let view = handle.ns_view.as_ptr() as id;
    if view == nil {
        return Err("GPUI supplied a null NSView");
    }

    // SAFETY: HasWindowHandle borrows the live GPUI Window; its AppKit contract
    // provides a valid NSView owned by that window. This function is synchronous
    // on the main thread, retains no pointers, and sends only documented AppKit
    // selectors. Each nullable returned NSWindow/NSButton is checked before use.
    unsafe {
        let native_window: id = msg_send![view, window];
        if native_window == nil {
            return Err("GPUI NSView is not attached to an NSWindow");
        }
        let kinds = [
            NSWindowButton::NSWindowCloseButton,
            NSWindowButton::NSWindowMiniaturizeButton,
            NSWindowButton::NSWindowZoomButton,
        ];
        let mut buttons = [nil; 3];
        for (index, kind) in kinds.into_iter().enumerate() {
            let button: id = msg_send![native_window, standardWindowButton: kind];
            if button == nil {
                return Err("titled NSWindow is missing a standard control");
            }
            buttons[index] = button;
        }
        // Keep native titled/closable/resizable/miniaturizable semantics; hide
        // only their visual controls. Arbitrary reading-surface drags stay off.
        let _: () = msg_send![native_window, setMovableByWindowBackground: NO];
        for button in buttons {
            let _: () = msg_send![button, setHidden: YES];
            let hidden: BOOL = msg_send![button, isHidden];
            if hidden != YES {
                return Err("AppKit did not hide a standard window control");
            }
        }
    }
    Ok(())
}

#[cfg(not(target_os = "macos"))]
pub fn configure(_: &gpui::Window) -> Result<(), &'static str> {
    Ok(())
}
