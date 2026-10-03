import AppKit
import WebKit

@MainActor
final class CloseProbe: NSObject, NSWindowDelegate {
    var called = false
    func windowShouldClose(_ sender: NSWindow) -> Bool { called = true; return false }
}

@main
@MainActor
struct WindowChromeTest {
    static func main() {
        let app = NSApplication.shared
        app.setActivationPolicy(.regular)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 800, height: 500),
                              styleMask: [.titled, .closable, .miniaturizable, .resizable],
                              backing: .buffered, defer: false)
        let closeProbe = CloseProbe()
        window.delegate = closeProbe
        window.title = "Track — synthetic window fixture"
        window.isReleasedWhenClosed = false
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        let web = WKWebView(frame: .zero, configuration: config)
        window.contentView = web
        let before = CommandLine.arguments.contains("--before")
        if !before { WorkspaceWindowChrome.apply(to: window) }
        let menu = NSMenu()
        WorkspaceWindowChrome.installWindowMenu(in: menu, application: app)
        app.mainMenu = menu
        window.center()
        window.makeKeyAndOrderFront(nil)
        app.activate(ignoringOtherApps: true)
        web.loadHTMLString("""
        <html><body style="margin:0;background:#191d1e;color:white;font:16px system-ui">
        <div style="background:#333;padding:12px">BETA Shared　　ALPHA</div>
        <div style="padding:12px;border-bottom:1px solid #555">Note one　×　　Note two　×</div>
        <main style="padding:24px"><h1>Synthetic workspace</h1>
        <input aria-label="Keyboard test" placeholder="Type here"><p>Native titlebar regression fixture</p></main>
        </body></html>
        """, baseURL: nil)
        DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
            precondition(window.canBecomeKey && window.canBecomeMain)
            precondition(window.accessibilityRole() == .window)
            precondition(window.title == "Track — synthetic window fixture")
            if !before {
                precondition(abs(web.frame.height - window.frame.height) < 1, "content must reclaim titlebar height")
                precondition(window.standardWindowButton(.closeButton)?.isHidden == true)
                window.setContentSize(NSSize(width: 840, height: 520))
                precondition(abs(web.frame.height - window.frame.height) < 1, "resize must retain full height")
                precondition(window.makeFirstResponder(web))
                precondition(app.sendAction(#selector(NSWindow.performClose(_:)), to: nil, from: nil))
                precondition(closeProbe.called, "Close must reach the unsaved-work delegate")
                precondition(app.sendAction(#selector(NSWindow.miniaturize(_:)), to: nil, from: nil))
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                if !before {
                    precondition(window.isMiniaturized)
                    window.deminiaturize(nil)
                    window.makeKeyAndOrderFront(nil)
                }
                print("PASS native geometry, focus, accessibility role, resize and menu minimize; window=\(window.windowNumber)")
                fflush(stdout)
                if let output = ProcessInfo.processInfo.environment["WINDOW_FIXTURE_ID"] {
                    try! String(window.windowNumber).write(toFile: output, atomically: true, encoding: .utf8)
                }
                DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                    if let output = ProcessInfo.processInfo.environment["WINDOW_FIXTURE_SCREENSHOT"] {
                        let capture = Process()
                        capture.executableURL = URL(fileURLWithPath: "/usr/sbin/screencapture")
                        capture.arguments = ["-x", "-o", "-l", String(window.windowNumber), output]
                        try! capture.run()
                        capture.waitUntilExit()
                        print("Screenshot exit: \(capture.terminationStatus)")
                    }
                    if !before {
                        precondition(app.sendAction(#selector(NSWindow.toggleFullScreen(_:)), to: nil, from: nil))
                        DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                            precondition(window.styleMask.contains(.fullScreen), "enter full screen")
                            window.toggleFullScreen(nil)
                            DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                                precondition(!window.styleMask.contains(.fullScreen), "exit full screen")
                                precondition(abs(web.frame.height - window.frame.height) < 1)
                                print("PASS full-screen round trip")
                                app.terminate(nil)
                            }
                        }
                    } else {
                        app.terminate(nil)
                    }
                }
            }
        }
        app.run()
    }
}
