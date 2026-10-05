import AppKit
import Foundation
import TrackWebCore
import WebKit

// Standalone integration harness: compiles the production coordinator/process unchanged.
// It intentionally does not instantiate AppDelegate or click native modal buttons.
@MainActor
final class RecoveryHarness: NSObject, NSApplicationDelegate {
    let webView = WKWebView(frame: NSRect(x: 0, y: 0, width: 1000, height: 800), configuration: {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        return configuration
    }())
    var window: NSWindow!
    var coordinator: WebViewCoordinator!
    var supervisor: WebServerSupervisor!
    var cancellations = 0
    var finished = false
    let arguments = CommandLine.arguments

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        window = NSWindow(contentRect: webView.frame, styleMask: [.titled, .resizable], backing: .buffered, defer: false)
        window.contentView = webView
        window.orderFront(nil)
        Task { @MainActor in
            do { try await run(); finish(nil) }
            catch { finish(String(describing: error)) }
        }
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(80))
            finish("overall harness timeout")
        }
    }

    func run() async throws {
        let defaults = WebViewCoordinator()
        try require(defaults.workspaceOrigin.absoluteString == "http://127.0.0.1:18765", "default workspace origin changed")
        try require(defaults.staticAppsOrigin.absoluteString == "http://127.0.0.1:18766", "default apps origin changed")
        let port = Int(arguments[2])!
        let origin = URL(string: "http://127.0.0.1:\(port)")!
        coordinator = WebViewCoordinator(workspaceOrigin: origin, staticAppsOrigin: URL(string: "http://127.0.0.1:\(port + 1)")!)
        coordinator.webView = webView
        coordinator.window = window
        coordinator.onConfirmDataLoss = { [weak self] _, completion in
            self?.cancellations += 1
            completion(false)
        }
        webView.navigationDelegate = coordinator
        webView.uiDelegate = coordinator
        let executable = URL(fileURLWithPath: arguments[1])
        supervisor = WebServerSupervisor(endpoint: origin.appendingPathComponent("api/vaults"), healthProbe: { endpoint in
            do {
                let (_, response) = try await URLSession.shared.data(from: endpoint)
                return (response as? HTTPURLResponse)?.statusCode == 200
            } catch { return false }
        }, childFactory: { _ in
            TrackServerProcess(executableURL: executable, arguments: ["web", "--addr", "127.0.0.1:\(port)"], environmentOverrides: [:])
        })
        supervisor.start()
        try await ready()
        coordinator.loadWorkspace()
        webView.load(URLRequest(url: origin.appendingPathComponent("notes/100")))
        for _ in 0..<200 {
            if coordinator.hasCompletedWorkspaceNavigation { break }
            try await Task.sleep(for: .milliseconds(50))
        }
        try require(coordinator.hasCompletedWorkspaceNavigation, "initial navigation timeout")
        let probe = try String(contentsOfFile: arguments[3], encoding: .utf8)
        _ = try await evaluate(probe + "\n'installed'")
        try await stage("clean")
        try require(cancellations == 0, "clean Contents link displayed a warning")
        _ = try await evaluate("(() => { const link = document.createElement('a'); link.href = '/notes/101'; document.body.appendChild(link); link.click(); return true; })()")
        for _ in 0..<200 {
            if !webView.isLoading && webView.url?.path == "/notes/101" { break }
            try await Task.sleep(for: .milliseconds(50))
        }
        try require(webView.url?.path == "/notes/101" && cancellations == 0, "unchanged note navigation was blocked")
        // Let the destination editor mount before exercising a programmatic document replacement.
        var editorMounted = false
        for _ in 0..<200 {
            if try await evaluate("document.querySelector('form.note-editor') ? 'ready' : 'waiting'") == "ready" {
                editorMounted = true
                break
            }
            try await Task.sleep(for: .milliseconds(50))
        }
        try require(editorMounted, "second note editor did not mount")
        webView.load(URLRequest(url: origin.appendingPathComponent("notes/100")))
        for _ in 0..<200 {
            if !webView.isLoading && webView.url?.path == "/notes/100" { break }
            try await Task.sleep(for: .milliseconds(50))
        }
        try require(webView.url?.path == "/notes/100" && cancellations == 0, "clean return navigation was blocked")
        _ = try await evaluate(probe + "\n'installed'")
        try await stage("prepare")
        try require(cancellations == 0, "dirty Contents link displayed a warning")
        // The existing delegate contract allows a deterministic Cancel response to reload.
        webView.reload()
        for _ in 0..<100 {
            if cancellations == 1 { break }
            try await Task.sleep(for: .milliseconds(50))
        }
        try require(cancellations == 1, "reload did not request exactly one cancellation")
        try await stage("cancelled")
        await withCheckedContinuation { continuation in
            supervisor.stop { continuation.resume() }
        }
        try require(supervisor.state == .stopped, "owned child not stopped/reaped")
        try await stage("offline")
        supervisor.start()
        try await ready()
        coordinator.serverDidRecover()
        try await stage("recovered")
    }

    func ready() async throws {
        for _ in 0..<250 {
            if supervisor.state == .ready { return }
            if case .failed(let reason) = supervisor.state { throw Failure(reason) }
            try await Task.sleep(for: .milliseconds(100))
        }
        throw Failure("server readiness timeout")
    }

    func evaluate(_ script: String) async throws -> String {
        try await withCheckedThrowingContinuation { continuation in
            webView.evaluateJavaScript(script) { result, error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: result as? String ?? "") }
            }
        }
    }

    func stage(_ name: String) async throws {
        _ = try await evaluate("window.__wk3Run('\(name)'); 'started'")
        for _ in 0..<300 {
            let json = try await evaluate("JSON.stringify(window.__wk3Result)")
            if let data = json.data(using: .utf8),
               let result = try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]) as? [String: Any] {
                try require(result["ok"] as? Bool == true, json)
                print("PASS \(name): \(json)")
                return
            }
            try await Task.sleep(for: .milliseconds(50))
        }
        throw Failure("probe timeout: \(name)")
    }

    func finish(_ error: String?) {
        guard !finished else { return }
        finished = true
        let complete: @MainActor () -> Void = {
            if let error { fputs("FAIL WK3: \(error)\n", stderr) }
            else { print("PASS WK3: defaults, React draft, cancelled reload, failed offline save, retry and successful save") }
            exit(error == nil ? 0 : 1)
        }
        if let supervisor { supervisor.stop(completion: complete) }
        else { complete() }
    }
}

struct Failure: Error, CustomStringConvertible {
    let description: String
    init(_ description: String) { self.description = description }
}

func require(_ condition: Bool, _ message: String) throws {
    if !condition { throw Failure(message) }
}

let application = NSApplication.shared
let harness = RecoveryHarness()
application.delegate = harness
application.run()
