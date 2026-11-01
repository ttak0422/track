import AppKit
import Foundation
import WebKit

@MainActor
private final class MockNotifications: ActivityNotificationDriver {
    var open: ((String) -> Void)?
    var state = "Allowed"
    var authorizations = 0
    var identifiers: [String] = []
    var removals: [[String]] = []
    var fail = false
    var release: CheckedContinuation<Void, Never>?
    var suspend = false
    func permission() async -> String { state }
    func authorize() async throws -> Bool { authorizations += 1; return state == "Allowed" }
    func post(identifier: String) async throws {
        identifiers.append(identifier)
        if suspend { await withCheckedContinuation { release = $0 } }
        if fail { throw CocoaError(.fileReadUnknown) }
    }
    func remove(identifiers: [String]) { removals.append(identifiers) }
}

@MainActor
private final class FixtureNavigation: NSObject, WKNavigationDelegate {
    let bridge: NativeNotificationBridge
    init(_ bridge: NativeNotificationBridge) { self.bridge = bridge }
    func webView(_ webView: WKWebView, didStartProvisionalNavigation navigation: WKNavigation!) { bridge.pageChanged() }
    func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) { bridge.documentCommitted(webView.url) }
}

@main
@MainActor
struct NotificationBridgeTests {
    static func main() async throws {
        _ = NSApplication.shared
        let origin = URL(string: CommandLine.arguments[1])!
        let driver = MockNotifications()
        var preferences: [Bool] = []
        let bridge = NativeNotificationBridge(origin: origin, driver: driver, saveEnabled: { preferences.append($0) })
        try expect(!(await bridge.post(noteID: "123", day: "2026-11-01")), "disabled must fall back")
        try expect(driver.authorizations == 0 && driver.identifiers.isEmpty, "launch/activity requested permission")
        driver.state = "Denied in System Settings"
        await bridge.setEnabled(true)
        try expect(!bridge.enabled && preferences == [false], "denial must not enable notifications")
        driver.state = "Allowed"
        await bridge.setEnabled(true)
        try expect(bridge.enabled && preferences == [false, true], "explicit enable preference missing")
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        bridge.install(in: configuration)
        let webView = WKWebView(frame: NSRect(x: 0, y: 0, width: 500, height: 400), configuration: configuration)
        bridge.webView = webView
        let navigation = FixtureNavigation(bridge)
        webView.navigationDelegate = navigation
        webView.load(URLRequest(url: origin))
        try await wait { (try? await webView.evaluateJavaScript("document.readyState === 'complete' && !!window.__trackNativeNotifications")) as? Bool == true }
        let valid = "{action:'activity',noteID:'work~123',day:'2026-11-01'}"
        let posted = try await call(webView, valid)
        try expect(posted, "allowed activity failed")
        let duplicate = try await call(webView, valid)
        try expect(duplicate && driver.identifiers.count == 1, "daily dedup failed")
        let authorizations = driver.authorizations
        for invalid in ["{action:'authorize'}", "{action:'activity',noteID:'../secret',day:'2026-11-01'}", "{action:'activity',noteID:'123',day:'x'}", "{action:'activity',noteID:'123',day:'2026-11-01',title:'secret'}", "{action:'activity',noteID:123,day:'2026-11-01'}"] {
            let result = try await call(webView, invalid)
            try expect(!result, "invalid message reached native notification service")
        }
        try expect(driver.authorizations == authorizations && driver.identifiers.count == 1, "JS exposed permission or arbitrary notification API")
        print("PASS disabled/denied/explicit enable, allowlisted payloads and daily dedup")

        driver.fail = true
        try expect(!(await bridge.post(noteID: "456", day: "2026-11-01")), "posting failure must fall back")
        driver.fail = false
        try expect(await bridge.post(noteID: "456", day: "2026-11-01"), "failure poisoned future retries")
        driver.state = "Denied in System Settings"
        try expect(!(await bridge.post(noteID: "789", day: "2026-11-01")), "revoked permission must fall back")
        driver.state = "Allowed"
        driver.suspend = true
        let first = Task { await bridge.post(noteID: "789", day: "2026-11-01") }
        try await wait { driver.release != nil }
        let second = Task { await bridge.post(noteID: "789", day: "2026-11-01") }
        await Task.yield()
        let count = driver.identifiers.count
        driver.release?.resume(); driver.release = nil
        let results = await (first.value, second.value)
        try expect(results.0 && results.1 && driver.identifiers.count == count, "concurrent posting duplicated or lost activity")
        driver.suspend = false
        print("PASS posting failure/retry, permission revocation and concurrent dedup")

        _ = try await webView.evaluateJavaScript("window.opens=[]; addEventListener('track-native-notification-open', e=>opens.push(e.detail.noteID)); true")
        driver.open?(driver.identifiers[0])
        try expect((try await webView.evaluateJavaScript("opens.length")) as? Int == 0, "click dispatched before router ready")
        _ = try await call(webView, "{action:'ready'}")
        try await wait { (try? await webView.evaluateJavaScript("opens[0] === 'work~123'")) as? Bool == true }
        driver.open?("track-activity-stale-from-another-process")
        try expect((try await webView.evaluateJavaScript("opens.length")) as? Int == 1, "stale click routed to a different vault")
        print("PASS router-ready click handoff and stale identifier protection")

        let before = driver.identifiers.count
        _ = try await webView.evaluateJavaScript("let frame=document.createElement('iframe');frame.src='/frame';document.body.appendChild(frame);true")
        try await wait { (try? await webView.evaluateJavaScript("document.querySelector('iframe').contentWindow.finished === true")) as? Bool == true }
        try expect(driver.identifiers.count == before, "embedded frame posted a native notification")
        try expect((try await webView.evaluateJavaScript("document.querySelector('iframe').contentWindow.__trackNativeNotifications === undefined")) as? Bool == true, "marker leaked to frame")
        _ = try await webView.evaluateJavaScript("let asset=document.createElement('iframe');asset.src='/api/asset?name=tool.html';asset.sandbox='allow-scripts allow-same-origin';document.body.appendChild(asset);true")
        try await wait { (try? await webView.evaluateJavaScript("window.assetBlocked === true")) as? Bool == true }
        try expect(driver.identifiers.count == before, "sandboxed asset called parent bridge")
        bridge.pageChanged()
        webView.load(URLRequest(url: origin.appendingPathComponent("api/asset-legacy")))
        try await wait { (try? await webView.evaluateJavaScript("document.readyState === 'complete' && location.pathname === '/api/asset-legacy'")) as? Bool == true }
        try expect((try await webView.evaluateJavaScript("window.__trackNativeNotifications === undefined")) as? Bool == true, "asset received native marker")
        _ = try await webView.evaluateJavaScript("history.replaceState(null,'','/');true")
        let spoofed = try await webView.callAsyncJavaScript("return await window.webkit.messageHandlers.trackNotifications.postMessage(\(valid))", contentWorld: .page) as? Bool
        try expect(spoofed == false && driver.identifiers.count == before, "asset promoted itself through history")
        print("PASS executable asset isolation, parent bridge denial and history spoof rejection")

        let otherOrigin = URL(string: CommandLine.arguments[2])!
        bridge.pageChanged()
        webView.load(URLRequest(url: otherOrigin))
        try await wait { (try? await webView.evaluateJavaScript("document.readyState === 'complete' && location.port === '\(otherOrigin.port!)'")) as? Bool == true }
        let foreign = try await webView.callAsyncJavaScript("return await window.webkit.messageHandlers.trackNotifications.postMessage(\(valid))", contentWorld: .page) as? Bool
        try expect(foreign == false && driver.identifiers.count == before, "foreign raw handler escaped origin check")
        try expect((try await webView.evaluateJavaScript("window.__trackNativeNotifications === undefined")) as? Bool == true, "foreign origin received marker")
        print("PASS same-origin iframe and foreign main-frame raw-handler rejection")

        await bridge.setEnabled(false)
        try expect(!bridge.enabled && !driver.removals.isEmpty, "disable did not clear delivered/pending notices")
        await bridge.setEnabled(true)
        try expect(await bridge.post(noteID: "123", day: "2026-11-01"), "re-enable lost activity to stale dedup")
        driver.suspend = true
        let pending = Task { await bridge.post(noteID: "999", day: "2026-11-01") }
        try await wait { driver.release != nil }
        let oldIdentifier = driver.identifiers.last!
        await bridge.setEnabled(false)
        await bridge.setEnabled(true)
        driver.suspend = false
        try expect(await bridge.post(noteID: "1000", day: "2026-11-01"), "new enabled generation failed to post")
        let newerIdentifier = driver.identifiers.last!
        driver.release?.resume(); driver.release = nil
        try expect(!(await pending.value), "disable during posting falsely acknowledged delivery")
        try expect(driver.removals.last == [oldIdentifier], "stale posting cleared a newer generation")
        try expect(!driver.removals.flatMap { $0 }.contains(newerIdentifier), "newly acknowledged notice was cleared")
        print("PASS disable/re-enable, in-flight fallback and stale-generation scoped removal; no OS permission/settings touched")
    }

    private static func call(_ webView: WKWebView, _ body: String) async throws -> Bool {
        try await webView.callAsyncJavaScript("return await __trackNativeNotifications.postMessage(\(body))", contentWorld: .page) as? Bool == true
    }
    private static func expect(_ condition: Bool, _ message: String) throws {
        if !condition { throw NSError(domain: "NotificationBridgeTest", code: 1, userInfo: [NSLocalizedDescriptionKey: message]) }
    }
    private static func wait(_ condition: () async -> Bool) async throws {
        let deadline = Date().addingTimeInterval(10)
        while Date() < deadline {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(20))
        }
        throw NSError(domain: "NotificationBridgeTest", code: 2, userInfo: [NSLocalizedDescriptionKey: "fixture timed out"])
    }
}
