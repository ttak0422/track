import AppKit
import Foundation
import TrackWebCore
import WebKit

@MainActor
private final class SyntheticSpeechDriver: SpeechDriver {
    var starts = 0
    var capturing = false
    var receive: (@MainActor (SpeechUpdate) -> Void)?
    func authorize() async -> String? { nil }
    func start(locale: String, receive: @escaping @MainActor (SpeechUpdate) -> Void) throws {
        precondition(locale == "ja-JP")
        starts += 1
        capturing = true
        self.receive = receive
    }
    func stopAudio() { capturing = false }
    func finishAudio() { receive?(.final("合成テスト")) }
    func cancel() { capturing = false }
}

@main
@MainActor
struct SpeechBridgeTests {
    static func main() async throws {
        _ = NSApplication.shared
        let origin = URL(string: CommandLine.arguments[1])!
        let driver = SyntheticSpeechDriver()
        let bridge = NativeSpeechBridge(origin: origin, session: NativeSpeechSession(driver: driver))
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        bridge.install(in: configuration)
        let webView = WKWebView(frame: NSRect(x: 0, y: 0, width: 500, height: 400), configuration: configuration)
        bridge.webView = webView
        webView.load(URLRequest(url: origin))
        try await wait { (try? await webView.evaluateJavaScript("document.readyState === 'complete' && !!window.__trackNativeSpeech")) as? Bool == true }
        _ = try await webView.evaluateJavaScript("""
        window.events = [];
        addEventListener('track-native-speech', e => events.push(e.detail));
        __trackNativeSpeech.postMessage({action:'start', id:'one', locale:'ja-JP'}); true;
        """)
        try await wait { driver.starts == 1 && driver.capturing }
        driver.receive?(.partial("途中"))
        _ = try await webView.evaluateJavaScript("__trackNativeSpeech.postMessage({action:'stop', id:'one'}); true")
        try await wait { !driver.capturing }
        try await wait { (try? await webView.evaluateJavaScript("events.filter(e=>e.type==='final' && e.text==='合成テスト').length")) as? Int == 1 }
        print("PASS WKWebView native marker, Japanese start, partial/final delivery, and stop")

        let before = driver.starts
        _ = try await webView.evaluateJavaScript("""
        let frame = document.createElement('iframe');
        frame.src = '/frame'; document.body.appendChild(frame); true;
        """)
        try await Task.sleep(for: .milliseconds(250))
        // The fixture's same-origin subframe explicitly calls the raw handler; native frame checks reject it.
        try expect(driver.starts == before, "embedded frame reached microphone driver")
        let marker = try await webView.evaluateJavaScript("document.querySelector('iframe').contentWindow.__trackNativeSpeech === undefined") as? Bool
        try expect(marker == true, "marker leaked into subframe")
        print("PASS embedded frames cannot access native recognition")

        _ = try await webView.evaluateJavaScript("__trackNativeSpeech.postMessage({action:'start', id:'two', locale:'ja-JP'}); true")
        try await wait { driver.capturing }
        _ = try await webView.evaluateJavaScript("dispatchEvent(new Event('pagehide'))")
        try await wait { !driver.capturing }
        print("PASS pagehide cancels capture")
        _ = try await webView.evaluateJavaScript("__trackNativeSpeech.postMessage({action:'start', id:'three', locale:'ja-JP'}); true")
        try await wait { driver.capturing }
        bridge.cancel()
        try expect(!driver.capturing, "host close cancellation left microphone active")
        print("PASS host cancellation releases capture")
        let finalStarts = driver.starts
        webView.load(URLRequest(url: origin.appendingPathComponent("remote")))
        try await wait { webView.url?.path == "/remote" }
        // A second port is a distinct origin, even on loopback; the script has no marker there.
        let otherOrigin = URL(string: CommandLine.arguments[2])!
        webView.load(URLRequest(url: otherOrigin))
        try await wait { (try? await webView.evaluateJavaScript("document.readyState === 'complete' && location.port === '\(otherOrigin.port!)'")) as? Bool == true }
        _ = try await webView.evaluateJavaScript("window.webkit.messageHandlers.trackSpeech.postMessage({action:'start',id:'foreign',locale:'ja-JP'}); true")
        try await Task.sleep(for: .milliseconds(100))
        try expect(driver.starts == finalStarts, "foreign origin reached microphone driver")
        let absent = try await webView.evaluateJavaScript("window.__trackNativeSpeech === undefined") as? Bool
        try expect(absent == true, "foreign origin received native marker")
        print("PASS foreign origin and raw-handler access rejected")
    }

    private static func expect(_ condition: Bool, _ message: String) throws {
        if !condition { throw NSError(domain: "SpeechBridgeTest", code: 1, userInfo: [NSLocalizedDescriptionKey: message]) }
    }
    private static func wait(_ condition: () async -> Bool) async throws {
        let deadline = Date().addingTimeInterval(10)
        while Date() < deadline {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(20))
        }
        throw NSError(domain: "SpeechBridgeTest", code: 2, userInfo: [NSLocalizedDescriptionKey: "fixture timed out"])
    }
}
