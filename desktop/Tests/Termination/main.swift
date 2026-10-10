import AppKit
import Foundation
import TrackWebCore
import WebKit

@MainActor
final class TestApplication: NSApplication {
    var harness: TerminationHarness!
    override func reply(toApplicationShouldTerminate shouldTerminate: Bool) {
        harness.replies.append(shouldTerminate)
        if shouldTerminate {
            harness.complete()
        } else {
            super.reply(toApplicationShouldTerminate: false)
        }
    }
}

@MainActor
final class TerminationHarness: NSObject, NSApplicationDelegate {
    let app: TestApplication
    let delegate: AppDelegate
    let scenario: String
    let resultURL: URL
    var webView: WKWebView!
    var window: NSWindow!
    var prompts = 0
    var handledPanels = Set<ObjectIdentifier>()
    var childPID: Int32?
    var started: TimeInterval = 0
    var replies: [Bool] = []
    var callbackReplies: [Int] = []
    var duplicateSent = false
    var timer: Timer?
    var finished = false
    var expectedPrompts: Int { scenario.contains("cancel") || scenario == "dirty-discard" ? 1 : 0 }

    init(app: TestApplication) {
        self.app = app
        let args = CommandLine.arguments
        scenario = args[1]
        resultURL = URL(fileURLWithPath: args[3])
        delegate = AppDelegate(application: app, arguments: ["fixture"],
                               workspaceOrigin: URL(string: "http://127.0.0.1:\(args[2])")!,
                               websiteDataStore: .nonPersistent())
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        if scenario != "initial-os" { delegate.applicationDidFinishLaunching(notification) }
        timer = Timer(timeInterval: 0.03, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.handleAlert() }
        }
        RunLoop.main.add(timer!, forMode: .common)
        RunLoop.main.add(timer!, forMode: .modalPanel)
        Task { @MainActor in
            do { try await run() }
            catch { finish(error: String(describing: error)) }
        }
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(25))
            finish(error: "termination harness timed out")
        }
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        let reply = delegate.applicationShouldTerminate(sender)
        callbackReplies.append(Int(reply.rawValue))
        if !duplicateSent && (scenario == "clean-os" || scenario.contains("cancel")) {
            duplicateSent = true
            callbackReplies.append(Int(delegate.applicationShouldTerminate(app).rawValue))
            if let window, delegate.windowShouldClose(window) { finish(error: "duplicate close bypassed pending termination") }
        }
        return reply
    }

    func applicationWillTerminate(_ notification: Notification) { delegate.applicationWillTerminate(notification) }

    func findWebView(_ view: NSView) -> WKWebView? {
        if let web = view as? WKWebView { return web }
        return view.subviews.compactMap(findWebView).first
    }

    func evaluate(_ script: String) async throws -> Any? {
        try await webView.evaluateJavaScript(script)
    }

    func waitFor(_ expression: String) async throws {
        for _ in 0..<200 {
            if (try? await evaluate(expression)) as? Bool == true { return }
            try await Task.sleep(for: .milliseconds(25))
        }
        let details = try? await evaluate("JSON.stringify({url:location.href, editor:window.__trackNativeEditorState?.(), workspace:window.__trackNativeWorkspaceState?.(), text:document.body.innerText.slice(0,1200), links:Array.from(document.links).map(a=>a.getAttribute('href'))})")
        throw Failure("timeout: \(expression); \(details ?? "no details")")
    }

    func run() async throws {
        if scenario == "initial-os" || scenario == "startup-os" {
            perform(#selector(requestTermination), with: nil, afterDelay: 0)
            return
        }
        window = app.windows.first { $0.delegate === delegate }!
        webView = findWebView(window.contentView!)!
        try await waitFor("window.__trackNativeWorkspaceState?.() === false")
        _ = try await evaluate("(() => { const a = document.createElement('a'); a.href = '/notes/100'; document.body.appendChild(a); a.click(); return true; })()")
        try await waitFor("location.pathname === '/notes/100' && !!document.querySelector('.tab-close') && typeof window.__trackNativeEditorState === 'function'")
        try await waitFor("document.body.innerText.includes('Original fixture') && window.__trackNativeEditorState() === false")
        while webView.isLoading { try await Task.sleep(for: .milliseconds(25)) }
        let cache = URL(fileURLWithPath: ProcessInfo.processInfo.environment["TRACK_CACHE_DIR"]!)
        let leases = try FileManager.default.contentsOfDirectory(at: cache, includingPropertiesForKeys: nil).filter { $0.pathExtension == "pid" }
        childPID = Int32(try String(contentsOf: leases.first!, encoding: .utf8).split(separator: "\n")[0])
        if scenario == "stopped-child-quit" { try require(kill(childPID!, SIGSTOP) == 0, "could not suspend owned child") }
        if scenario == "empty-close" {
            _ = try await evaluate("document.querySelector('.tab-close').click(); true")
            try await waitFor("document.querySelectorAll('.tab').length === 0")
        }
        if scenario == "multi-clean" || scenario == "dirty-multi-cancel" {
            _ = try await evaluate("(() => { const a = document.createElement('a'); a.href = '/notes/101'; document.body.appendChild(a); a.click(); return true; })()")
            try await waitFor("document.querySelectorAll('.tab').length === 2")
            _ = try await evaluate("Array.from(document.querySelectorAll('.tab-label')).find(b => b.querySelector('.tab-title').textContent === 'Fixture').click(); true")
            try await waitFor("location.pathname === '/notes/100' && document.body.innerText.includes('Original fixture') && window.__trackNativeEditorState?.() === false")
        }
        if scenario.hasPrefix("dirty") || scenario == "saved-quit" || scenario == "pending-cancel" || scenario == "ime-cancel" || scenario == "clean-edit-quit" {
            _ = try await evaluate("document.querySelector('button[aria-label^=\"Display mode:\"]').click(); true")
            try await waitFor("!!document.querySelector('[aria-label=\"Display mode\"]')")
            _ = try await evaluate("Array.from(document.querySelectorAll('[role=menuitemradio]')).find(b => b.textContent.trim() === 'Edit').click(); true")
            try await waitFor("!!document.querySelector('form.note-editor textarea')")
            if scenario != "ime-cancel" && scenario != "clean-edit-quit" {
            _ = try await evaluate("""
            (() => { const t = document.querySelector('form.note-editor textarea');
            Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(t, 'Fixture unsaved 日本語');
            t.dispatchEvent(new Event('input', { bubbles: true })); return true; })()
            """)
            try await waitFor("window.__trackNativeEditorState() === true && !!document.querySelector('.tab.dirty')")
            }
        }
        if scenario == "ime-cancel" {
            _ = try await evaluate("document.querySelector('form.note-editor textarea').dispatchEvent(new CompositionEvent('compositionstart', {bubbles:true})); true")
        }
        if scenario == "pending-cancel" {
            try require(kill(childPID!, SIGSTOP) == 0, "could not suspend owned child")
            _ = try await evaluate("document.querySelector('form.note-editor button[type=submit]').click(); true")
            try await waitFor("document.querySelector('form.note-editor button[type=submit]').textContent === 'Saving...'")
        }
        if scenario == "saved-quit" { try await save() }
        if scenario == "timeout-cancel" {
            // Hold WebKit's process, not the native run loop. The bounded probe must settle once;
            // its late clean response must not approve a cancelled termination.
            _ = try await evaluate("window.__trackNativeEditorState = () => { const end = Date.now() + 3500; while (Date.now() < end) {} return false; }; true")
        }
        if scenario == "probe-error-cancel" { _ = try await evaluate("window.__trackNativeEditorState = () => { throw Error('fixture'); }; true") }
        if scenario == "unknown-cancel" { _ = try await evaluate("delete window.__trackNativeEditorState; delete window.__trackNativeWorkspaceState; true") }
        perform(#selector(requestTermination), with: nil, afterDelay: 0)
        if scenario.contains("cancel") {
            for _ in 0..<300 {
                if replies == [false] { break }
                try await Task.sleep(for: .milliseconds(25))
            }
            try require(replies == [false], "cancel must issue exactly one negative AppKit reply: \(replies)")
            try require(!webView.isHidden, "cancel hid the draft")
            if scenario == "ime-cancel" {
                try require((try await evaluate("window.__trackNativeEditorState()")) as? Bool == true, "composition was not protected")
                _ = try await evaluate("document.querySelector('form.note-editor textarea').dispatchEvent(new CompositionEvent('compositionend', {bubbles:true})); true")
            } else if scenario == "pending-cancel" {
                try require((try await evaluate("window.__trackNativeEditorState()")) as? Bool == true, "pending save was not protected")
                try require(kill(childPID!, SIGCONT) == 0, "could not resume owned child")
                try await waitFor("window.__trackNativeEditorState() === false && !document.querySelector('.tab.dirty')")
            } else if scenario.hasPrefix("dirty") {
                try require((try await evaluate("window.__trackNativeEditorState()")) as? Bool == true, "cancel lost draft")
                try await save()
            } else {
                try await Task.sleep(for: .seconds(4))
                try require(replies == [false], "late probe issued a second reply")
                _ = try await evaluate("window.__trackNativeEditorState = () => false; true")
            }
            perform(#selector(requestTermination), with: nil, afterDelay: 0)
        }
    }

    func save() async throws {
        _ = try await evaluate("document.querySelector('form.note-editor button[type=submit]').click(); true")
        try await waitFor("window.__trackNativeEditorState() === false && !document.querySelector('.tab.dirty')")
    }

    @objc func requestTermination() {
        started = ProcessInfo.processInfo.systemUptime
        if scenario.hasSuffix("close") {
            window.performClose(nil)
        } else if scenario.contains("os") {
            // Simulate the delegate callback used by OS termination; never request logout/shutdown.
            _ = applicationShouldTerminate(app)
        } else {
            let event = NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: .command,
                timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber,
                context: nil, characters: "q", charactersIgnoringModifiers: "q", isARepeat: false, keyCode: 12)!
            if app.mainMenu?.performKeyEquivalent(with: event) != true { finish(error: "Cmd-Q not handled") }
        }
    }

    func handleAlert() {
        let panels = app.windows.filter { $0 is NSPanel }
        for panel in panels where panel.isVisible && !handledPanels.contains(ObjectIdentifier(panel)) {
            func buttons(_ view: NSView) -> [NSButton] {
                (view as? NSButton).map { [$0] } ?? view.subviews.flatMap(buttons)
            }
            guard let content = panel.contentView else { continue }
            let choices = buttons(content)
            guard let cancel = choices.first(where: { $0.title == "Cancel" }) else { continue }
            handledPanels.insert(ObjectIdentifier(panel))
            prompts += 1
            if prompts > expectedPrompts {
                cancel.performClick(nil)
                finish(error: "unexpected unsaved-edits warning in \(scenario)")
                return
            }
            let button = scenario == "dirty-discard" ? choices.first(where: { $0.title == "Continue" })! : cancel
            button.performClick(nil)
        }
    }

    func complete() {
        if let childPID, kill(childPID, 0) == 0 || errno != ESRCH {
            finish(error: "AppKit received approval before owned Go child was reaped"); return
        }
        if scenario == "stopped-child-quit", ProcessInfo.processInfo.systemUptime - started < 2.5 {
            finish(error: "stopped child bypassed bounded graceful shutdown"); return
        }
        if prompts != expectedPrompts { finish(error: "prompt count \(prompts), expected \(expectedPrompts)"); return }
        if replies != (scenario.contains("cancel") ? [false, true] : [true]) {
            finish(error: "unexpected termination replies: \(replies)"); return
        }
        if callbackReplies.contains(Int(NSApplication.TerminateReply.terminateCancel.rawValue)) {
            finish(error: "synchronous cancel bypassed deferred flow"); return
        }
        finish(error: nil)
    }

    func finish(error: String?) {
        guard !finished else { return }
        finished = true
        let result: [String: Any] = ["scenario": scenario, "ok": error == nil, "error": error ?? "",
                                    "prompts": prompts, "replies": replies, "callbacks": callbackReplies,
                                    "duplicateSent": duplicateSent, "stopDuration": ProcessInfo.processInfo.systemUptime - started]
        try! JSONSerialization.data(withJSONObject: result, options: [.sortedKeys]).write(to: resultURL)
        print(String(data: try! JSONSerialization.data(withJSONObject: result, options: [.sortedKeys]), encoding: .utf8)!)
        exit(error == nil ? 0 : 1)
    }
}

struct Failure: Error, CustomStringConvertible { let description: String; init(_ value: String) { description = value } }
func require(_ value: Bool, _ message: String) throws { if !value { throw Failure(message) } }
let app = TestApplication.shared as! TestApplication
let harness = TerminationHarness(app: app)
app.harness = harness
app.delegate = harness
app.run()
