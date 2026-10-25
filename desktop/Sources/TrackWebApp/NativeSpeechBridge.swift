import Foundation
import TrackWebCore
import WebKit

@MainActor
final class NativeSpeechBridge: NSObject, WKScriptMessageHandler {
    private let origin: URL
    private let session: NativeSpeechSession
    weak var webView: WKWebView?

    init(origin: URL, session: NativeSpeechSession = NativeSpeechSession(driver: AppleSpeechDriver())) {
        self.origin = origin
        self.session = session
        super.init()
        session.emit = { [weak self] id, type, text in
            guard let self, let webView = self.webView, self.allows(webView.url),
                  let data = try? JSONSerialization.data(withJSONObject: ["id": id, "type": type, "text": text]),
                  let json = String(data: data, encoding: .utf8) else { return }
            webView.evaluateJavaScript("window.dispatchEvent(new CustomEvent('track-native-speech', {detail: \(json)}))")
        }
    }

    func install(in configuration: WKWebViewConfiguration) {
        configuration.userContentController.add(self, name: "trackSpeech")
        let originJSON = String(data: try! JSONSerialization.data(withJSONObject: [origin.absoluteString]), encoding: .utf8)!
        configuration.userContentController.addUserScript(WKUserScript(source: """
        if (\(originJSON).includes(location.origin)) {
          Object.defineProperty(window, '__trackNativeSpeech', { value: {
            postMessage: message => window.webkit.messageHandlers.trackSpeech.postMessage(message)
          }});
          addEventListener('pagehide', () => window.__trackNativeSpeech.postMessage({action:'cancel'}));
        }
        """, injectionTime: .atDocumentStart, forMainFrameOnly: true))
    }

    private func allows(_ url: URL?) -> Bool {
        guard let url else { return false }
        return url.scheme == origin.scheme && url.host == origin.host && url.port == origin.port
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.frameInfo.isMainFrame, message.webView === webView,
              allows(message.frameInfo.request.url), allows(webView?.url),
              let body = message.body as? [String: String], let action = body["action"] else { return }
        if action == "cancel", body["id"] == nil { cancel(); return }
        guard let id = body["id"], !id.isEmpty, id.count <= 100 else { return }
        switch action {
        case "start":
            guard body["locale"] == "ja-JP" else { return }
            session.start(id: id, locale: "ja-JP")
        case "stop": session.stop(id: id)
        case "cancel": session.cancel(id: id)
        default: break
        }
    }

    func cancel() { session.cancel() }
}
