import AppKit
import WebKit

// The test uses a fresh, nonpersistent web view and only an owned loopback origin.
final class Probe: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKScriptMessageHandler {
    var webView: WKWebView!
    var window: NSWindow!
    let url: URL
    init(url: URL) { self.url = url }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        config.userContentController.add(self, name: "result")
        webView = WKWebView(frame: NSRect(x: 0, y: 0, width: 1200, height: 1000), configuration: config)
        window = NSWindow(contentRect: webView.frame, styleMask: [.titled], backing: .buffered, defer: false)
        window.title = "Synthetic draw.io layout regression"
        window.contentView = webView
        window.orderFront(nil)
        webView.navigationDelegate = self
        webView.load(URLRequest(url: url))
    }

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        let target = action.request.url
        decisionHandler(target?.host == url.host && target?.port == url.port ? .allow : .cancel)
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        print("FAIL: navigation: \(error.localizedDescription)"); exit(1)
    }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        let result = message.body as? String ?? "FAIL: missing result"
        print(result)
        exit(result.hasPrefix("PASS:") ? 0 : 1)
    }
}

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let probe = Probe(url: URL(string: CommandLine.arguments[1])!)
app.delegate = probe
DispatchQueue.main.asyncAfter(deadline: .now() + 35) { print("FAIL: WKWebView timeout"); exit(1) }
app.run()
