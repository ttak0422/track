import AppKit
import Foundation
import TrackWebCore
import WebKit

@MainActor
final class WebViewCoordinator: NSObject, WKNavigationDelegate, WKUIDelegate {
    typealias DataLossConfirmation = @MainActor (
        String,
        @escaping @MainActor @Sendable (Bool) -> Void
    ) -> Void

    let workspaceOrigin = URL(string: "http://127.0.0.1:18765")!
    let staticAppsOrigin = URL(string: "http://127.0.0.1:18766")!

    weak var window: NSWindow?
    weak var webView: WKWebView?
    var onConfirmDataLoss: DataLossConfirmation?
    var onSmokeResult: (([String: Any]?, String?) -> Void)?
    var externalURLHandler: ((URL) -> Void)?
    var smokeMode = false
    private var didLoadWorkspace = false
    private(set) var hasCompletedWorkspaceNavigation = false
    private var smokeCheckStarted = false
    private var allowNextConfirmedNavigation = false
    private var targetBlankProbeToken: String?
    private var targetBlankProbeCounts: [String: Int] = [:]
    private var targetBlankProbeCompletion: (([String: Int]?, String?) -> Void)?
    private var targetBlankProbeTimeout: Task<Void, Never>?

    func allowNextNavigationAfterNativeConfirmation() {
        allowNextConfirmedNavigation = true
    }

    func cancelNextNavigationConfirmation() {
        allowNextConfirmedNavigation = false
    }

    func loadWorkspace() {
        guard let webView else { return }
        guard !hasCompletedWorkspaceNavigation else {
            serverDidRecover()
            return
        }
        didLoadWorkspace = true
        webView.load(URLRequest(url: workspaceOrigin))
    }

    /// Reconnect the existing document without reloading it; reload would discard editor-local drafts.
    func serverDidRecover() {
        webView?.evaluateJavaScript(
            "window.dispatchEvent(new Event('online')); window.dispatchEvent(new Event('focus'));"
        )
    }

    func installRecoveryDraft(token: String, completion: @escaping @MainActor @Sendable (Bool) -> Void) {
        guard let webView else {
            completion(false)
            return
        }
        let script = """
        (() => {
          window.__trackWebRecoveryToken = '\(token)';
          const draft = document.createElement('textarea');
          draft.id = 'track-web-recovery-draft';
          draft.value = 'unsaved draft \(token)';
          document.body.appendChild(draft);
          return draft.value;
        })()
        """
        webView.evaluateJavaScript(script) { result, error in
            completion(error == nil && (result as? String) == "unsaved draft \(token)")
        }
    }

    func verifyRecoveryDraft(token: String, completion: @escaping @MainActor @Sendable ([String: Any]?, String?) -> Void) {
        guard let webView else {
            completion(nil, "WKWebView disappeared during server recovery")
            return
        }
        let script = """
        (() => ({
          token: window.__trackWebRecoveryToken || '',
          value: document.getElementById('track-web-recovery-draft')?.value || '',
          rootChildren: document.getElementById('root')?.children.length || 0,
          currentURL: location.href
        }))()
        """
        webView.evaluateJavaScript(script) { result, error in
            if let error {
                completion(nil, error.localizedDescription)
            } else if let result = result as? [String: Any] {
                completion(result, nil)
            } else {
                completion(nil, "WKWebView returned no recovery state")
            }
        }
    }

    func runTargetBlankProbe(
        token: String,
        completion: @escaping @MainActor @Sendable ([String: Int]?, String?) -> Void
    ) {
        guard let webView else {
            completion(nil, "WKWebView disappeared during target=_blank probe")
            return
        }
        targetBlankProbeToken = token
        targetBlankProbeCounts = [:]
        targetBlankProbeCompletion = completion
        let scriptURL = "https://example.invalid/popup-script-\(token)"
        let internalURL = "http://127.0.0.1:18765/?__track_web_popup_internal=popup-internal-\(token)"
        let script = """
        (() => {
          window.open('\(scriptURL)', '_blank');
          window.open('\(internalURL)', '_blank');
          return true;
        })()
        """
        webView.evaluateJavaScript(script) { [weak self] _, error in
            guard let self else { return }
            if let error {
                self.finishTargetBlankProbe(error: error.localizedDescription)
                return
            }
            self.targetBlankProbeTimeout = Task { @MainActor [weak self] in
                try? await Task.sleep(for: .seconds(8))
                guard let self, self.targetBlankProbeToken == token else { return }
                self.finishTargetBlankProbe(error: "WKWebView did not navigate the internal target=_blank probe")
            }
        }
    }

    func decideNavigation(
        _ action: WKNavigationAction,
        in webView: WKWebView,
        completion: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void
    ) {
        guard let url = action.request.url else {
            completion(.cancel)
            return
        }
        let createsNewBrowsingContext = action.targetFrame == nil
        let targetIsMainFrame = action.targetFrame?.isMainFrame ?? true
        let userInitiated = action.navigationType == .linkActivated
        let decision = WebURLPolicy.decide(
            for: url,
            isMainFrame: targetIsMainFrame,
            isUserInitiated: userInitiated,
            workspaceOrigin: workspaceOrigin,
            staticAppsOrigin: staticAppsOrigin
        )

        switch decision {
        case .cancel:
            if createsNewBrowsingContext {
                recordTargetBlankEvent("targetless-cancel", url: url)
            }
            completion(.cancel)
        case .openInDefaultBrowser:
            if createsNewBrowsingContext {
                // Allow WebKit to call createWebViewWith, which is the one owner of target=_blank
                // dispatch. That delegate opens eligible external URLs once and returns nil.
                completion(.allow)
                return
            }
            if case .openInDefaultBrowser(let externalURL) = decision {
                openExternalURL(externalURL)
            }
            completion(.cancel)
        case .allowInWebView:
            if createsNewBrowsingContext {
                // The UI delegate will reuse this WebView for internal _blank links. Do not load here:
                // doing so as well would navigate twice if WebKit also asks to create a context.
                completion(.allow)
                return
            }
            if targetIsMainFrame, allowNextConfirmedNavigation {
                allowNextConfirmedNavigation = false
                completion(.allow)
                return
            }
            guard targetIsMainFrame,
                  didLoadWorkspace,
                  action.targetFrame != nil,
                  shouldConfirmBeforeNavigation(action.navigationType) else {
                completion(.allow)
                return
            }
            guard let onConfirmDataLoss else {
                completion(.cancel)
                return
            }
            onConfirmDataLoss("navigating to another page") { confirmed in
                completion(confirmed ? .allow : .cancel)
            }
        }
    }

    func webView(
        _ webView: WKWebView,
        decidePolicyFor navigationAction: WKNavigationAction,
        decisionHandler: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void
    ) {
        decideNavigation(navigationAction, in: webView, completion: decisionHandler)
    }

    func webView(
        _ webView: WKWebView,
        decidePolicyFor navigationResponse: WKNavigationResponse,
        decisionHandler: @escaping @MainActor @Sendable (WKNavigationResponsePolicy) -> Void
    ) {
        guard let url = navigationResponse.response.url else {
            decisionHandler(.cancel)
            return
        }
        let decision = WebURLPolicy.decide(
            for: url,
            isMainFrame: navigationResponse.isForMainFrame,
            isUserInitiated: false,
            workspaceOrigin: workspaceOrigin,
            staticAppsOrigin: staticAppsOrigin
        )
        decisionHandler(decision == .allowInWebView ? .allow : .cancel)
    }

    func webView(
        _ webView: WKWebView,
        createWebViewWith configuration: WKWebViewConfiguration,
        for navigationAction: WKNavigationAction,
        windowFeatures: WKWindowFeatures
    ) -> WKWebView? {
        // No extra native window or WebView is created. Internal target=_blank links reuse this view;
        // eligible external clicks go to the default browser, while script-created popups fail closed.
        guard let url = navigationAction.request.url else { return nil }
        recordTargetBlankEvent("created", url: url)
        let userInitiated = navigationAction.navigationType == .linkActivated
        switch WebURLPolicy.decide(
            for: url,
            isMainFrame: true,
            isUserInitiated: userInitiated,
            workspaceOrigin: workspaceOrigin,
            staticAppsOrigin: staticAppsOrigin
        ) {
        case .allowInWebView:
            recordTargetBlankEvent("internal", url: url)
            let load = { [weak self, weak webView] in
                guard let self, let webView else { return }
                if self.didLoadWorkspace && self.shouldConfirmBeforeNavigation(navigationAction.navigationType) {
                    self.onConfirmDataLoss?("opening a page in this window") { confirmed in
                        guard confirmed else { return }
                        self.allowNextNavigationAfterNativeConfirmation()
                        if webView.load(navigationAction.request) == nil {
                            self.cancelNextNavigationConfirmation()
                        }
                    }
                } else {
                    webView.load(navigationAction.request)
                }
            }
            load()
        case .openInDefaultBrowser(let externalURL):
            recordTargetBlankEvent("external-open", url: url)
            openExternalURL(externalURL)
        case .cancel:
            recordTargetBlankEvent("cancel", url: url)
            break
        }
        return nil
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        if let token = targetBlankProbeToken,
           webView.url?.absoluteString.contains("popup-internal-\(token)") == true {
            finishTargetBlankProbe(error: nil)
        }
        if let url = webView.url,
           WebURLPolicy.decide(
               for: url,
               isMainFrame: true,
               isUserInitiated: false,
               workspaceOrigin: workspaceOrigin,
               staticAppsOrigin: staticAppsOrigin
           ) == .allowInWebView {
            hasCompletedWorkspaceNavigation = true
        }
        guard smokeMode, !smokeCheckStarted else { return }
        smokeCheckStarted = true
        let script = """
        (() => {
          window.__trackWebSmoke = { done: false };
          (async () => {
            const deadline = Date.now() + 15000;
            let apiStatus = 0;
            let activePath = "";
            let root = null;
            while (Date.now() < deadline) {
              root = document.getElementById("root");
              try {
                const response = await fetch("/api/vaults", { cache: "no-store" });
                apiStatus = response.status;
                const payload = await response.json();
                activePath = payload.active?.path || "";
                if (response.ok && root && root.children.length > 0 &&
                    !root.textContent.includes("The track web UI has not been built.")) {
                  window.__trackWebSmoke = {
                    done: true,
                    ok: true,
                    apiStatus,
                    activePath,
                    rootChildren: root.children.length,
                    renderedText: (root.innerText || "").slice(0, 400)
                  };
                  return;
                }
              } catch (_) {}
              await new Promise(resolve => setTimeout(resolve, 100));
            }
            window.__trackWebSmoke = {
              done: true,
              ok: false,
              apiStatus,
              activePath,
              rootChildren: root ? root.children.length : 0,
              renderedText: root ? (root.innerText || "").slice(0, 400) : "#root missing"
            };
          })().catch(error => {
            window.__trackWebSmoke = { done: true, ok: false, error: String(error) };
          });
          return true;
        })()
        """
        webView.evaluateJavaScript(script) { [weak self] _, error in
            guard let self else { return }
            if let error {
                self.onSmokeResult?(nil, error.localizedDescription)
            } else {
                self.pollSmokeResult(attempt: 0)
            }
        }
    }

    private func pollSmokeResult(attempt: Int) {
        guard let webView else { return }
        webView.evaluateJavaScript("window.__trackWebSmoke || null") { [weak self] result, error in
            guard let self else { return }
            if let error {
                self.onSmokeResult?(nil, error.localizedDescription)
                return
            }
            if let report = result as? [String: Any], report["done"] as? Bool == true {
                self.onSmokeResult?(report, nil)
                return
            }
            guard attempt < 180 else {
                self.onSmokeResult?(nil, "WKWebView did not finish its UI/API smoke probe")
                return
            }
            Task { @MainActor [weak self] in
                try? await Task.sleep(for: .milliseconds(100))
                self?.pollSmokeResult(attempt: attempt + 1)
            }
        }
    }

    private func recordTargetBlankEvent(_ name: String, url: URL?) {
        guard let token = targetBlankProbeToken,
              let absoluteURL = url?.absoluteString,
              let label = ["script", "click", "internal"].first(where: {
                  absoluteURL.contains("popup-\($0)-\(token)")
              }) else { return }
        let key = "\(name).\(label)"
        targetBlankProbeCounts[key, default: 0] += 1
    }

    private func openExternalURL(_ url: URL) {
        if let externalURLHandler {
            externalURLHandler(url)
        } else {
            _ = NSWorkspace.shared.open(url)
        }
    }

    private func finishTargetBlankProbe(error: String?) {
        guard let completion = targetBlankProbeCompletion else { return }
        targetBlankProbeCompletion = nil
        targetBlankProbeTimeout?.cancel()
        targetBlankProbeTimeout = nil
        completion(targetBlankProbeCounts, error)
        targetBlankProbeToken = nil
    }

    func webView(
        _ webView: WKWebView,
        runJavaScriptAlertPanelWithMessage message: String,
        initiatedByFrame frame: WKFrameInfo,
        completionHandler: @escaping @MainActor @Sendable () -> Void
    ) {
        let alert = NSAlert()
        alert.messageText = "Track"
        alert.informativeText = message
        alert.addButton(withTitle: "OK")
        present(alert) { _ in completionHandler() }
    }

    func webView(
        _ webView: WKWebView,
        runJavaScriptConfirmPanelWithMessage message: String,
        initiatedByFrame frame: WKFrameInfo,
        completionHandler: @escaping @MainActor @Sendable (Bool) -> Void
    ) {
        let alert = NSAlert()
        alert.messageText = "Track"
        alert.informativeText = message
        alert.addButton(withTitle: "Continue")
        alert.addButton(withTitle: "Cancel")
        present(alert) { completionHandler($0 == .alertFirstButtonReturn) }
    }

    func webView(
        _ webView: WKWebView,
        runJavaScriptTextInputPanelWithPrompt prompt: String,
        defaultText: String?,
        initiatedByFrame frame: WKFrameInfo,
        completionHandler: @escaping @MainActor @Sendable (String?) -> Void
    ) {
        let alert = NSAlert()
        alert.messageText = "Track"
        alert.informativeText = prompt
        alert.addButton(withTitle: "OK")
        alert.addButton(withTitle: "Cancel")
        let field = NSTextField(string: defaultText ?? "")
        field.frame = NSRect(x: 0, y: 0, width: 280, height: 24)
        alert.accessoryView = field
        present(alert) { response in
            completionHandler(response == .alertFirstButtonReturn ? field.stringValue : nil)
        }
    }

    func webView(
        _ webView: WKWebView,
        runOpenPanelWith parameters: WKOpenPanelParameters,
        initiatedByFrame frame: WKFrameInfo,
        completionHandler: @escaping @MainActor @Sendable ([URL]?) -> Void
    ) {
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = parameters.allowsDirectories
        panel.allowsMultipleSelection = parameters.allowsMultipleSelection
        panel.resolvesAliases = true
        if let window {
            panel.beginSheetModal(for: window) { response in
                completionHandler(response == .OK ? panel.urls : nil)
            }
        } else {
            panel.begin { response in
                completionHandler(response == .OK ? panel.urls : nil)
            }
        }
    }

    private func shouldConfirmBeforeNavigation(_ type: WKNavigationType) -> Bool {
        switch type {
        case .linkActivated, .formSubmitted, .formResubmitted, .backForward, .reload:
            return true
        case .other:
            return false
        @unknown default:
            return true
        }
    }

    private func present(
        _ alert: NSAlert,
        completion: @escaping @MainActor @Sendable (NSApplication.ModalResponse) -> Void
    ) {
        if let window {
            alert.beginSheetModal(for: window, completionHandler: completion)
        } else {
            completion(.alertSecondButtonReturn)
        }
    }
}
