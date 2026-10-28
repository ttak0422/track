import AppKit
import WebKit

/// AppKit owns input; isolated page ranges paint matches without replacing ordinary text selection.
/// WKWebView is not an NSTextFinderClient. Assigning it to NSTextFinder.client can present a bar
/// without connecting edits in that bar to the web document.
@MainActor
final class WebViewFindController: NSObject, NSSearchFieldDelegate {
    let bar = NSVisualEffectView(frame: NSRect(x: 0, y: 0, width: 360, height: 76))
    var onVisibilityChange: ((Bool) -> Void)?
    var onLayoutChange: ((CGFloat) -> Void)?

    private weak var webView: WKWebView?
    private let field = NSSearchField(frame: .zero)
    private let status = NSTextField(labelWithString: "")
    private let previous = NSButton(title: "Previous", target: nil, action: nil)
    private let next = NSButton(title: "Next", target: nil, action: nil)
    private var visible = false
    private var generation = 0
    private var lastQuery = ""
    private var pollTask: Task<Void, Never>?
    private var inFlight: Int?
    private var usesLegacyFind = false

    init(webView: WKWebView) {
        self.webView = webView
        super.init()
        field.placeholderString = "Find in page"
        field.setAccessibilityLabel("Find in page")
        field.delegate = self
        field.target = self
        field.action = #selector(findNext(_:))
        field.sendsWholeSearchString = true
        status.identifier = NSUserInterfaceItemIdentifier("track-find-status")
        status.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        status.textColor = .secondaryLabelColor
        status.setContentHuggingPriority(.defaultLow, for: .horizontal)
        previous.target = self
        previous.action = #selector(findPrevious(_:))
        previous.toolTip = "Previous match (Shift–Command–G)"
        next.target = self
        next.action = #selector(findNext(_:))
        next.toolTip = "Next match (Command–G)"
        let done = NSButton(title: "Done", target: self, action: #selector(close(_:)))
        bar.material = .popover
        bar.blendingMode = .withinWindow
        bar.state = .active
        bar.wantsLayer = true
        bar.layer?.cornerRadius = 8
        let entry = NSStackView(views: [field, done])
        let navigation = NSStackView(views: [previous, next, status])
        let stack = NSStackView(views: [entry, navigation])
        stack.orientation = .vertical
        stack.spacing = 8
        stack.alignment = .leading
        entry.distribution = .fill
        navigation.spacing = 8
        entry.translatesAutoresizingMaskIntoConstraints = false
        stack.translatesAutoresizingMaskIntoConstraints = false
        bar.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: bar.leadingAnchor, constant: 12),
            stack.trailingAnchor.constraint(equalTo: bar.trailingAnchor, constant: -12),
            stack.centerYAnchor.constraint(equalTo: bar.centerYAnchor),
            entry.widthAnchor.constraint(equalTo: stack.widthAnchor),
            field.widthAnchor.constraint(greaterThanOrEqualToConstant: 100),
            field.widthAnchor.constraint(lessThanOrEqualToConstant: 360),
            status.widthAnchor.constraint(greaterThanOrEqualToConstant: 76),
        ])
        updateButtons()
    }

    func validateAction(_ action: NSTextFinder.Action) -> Bool {
        switch action {
        case .showFindInterface: return webView?.isHidden == false
        case .hideFindInterface: return visible
        case .nextMatch, .previousMatch: return !field.stringValue.isEmpty && webView?.isHidden == false
        default: return false
        }
    }

    func performAction(_ action: NSTextFinder.Action) {
        switch action {
        case .showFindInterface:
            let wasVisible = visible
            visible = true
            onVisibilityChange?(true)
            field.selectText(nil)
            if !wasVisible {
                search(backwards: false, restart: true)
                pollTask?.cancel()
                pollTask = Task { @MainActor [weak self] in
                    while !Task.isCancelled {
                        try? await Task.sleep(for: .milliseconds(300))
                        guard !Task.isCancelled, let self, self.visible else { return }
                        if self.inFlight == nil && !self.usesLegacyFind &&
                            (self.field.currentEditor() as? NSTextView)?.hasMarkedText() != true {
                            self.search(backwards: false, restart: false, delta: 0)
                        }
                    }
                }
            }
        case .hideFindInterface: close(nil)
        case .nextMatch: findNext(nil)
        case .previousMatch: findPrevious(nil)
        default: break
        }
    }

    func controlTextDidChange(_ notification: Notification) {
        // Do not search an uncommitted IME composition. Its commit sends the normal change event.
        guard (field.currentEditor() as? NSTextView)?.hasMarkedText() != true else { return }
        guard field.stringValue != lastQuery else { return }
        search(backwards: false, restart: true)
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy commandSelector: Selector) -> Bool {
        if commandSelector == #selector(NSResponder.cancelOperation(_:)) {
            close(nil)
            return true
        }
        if commandSelector == #selector(NSResponder.insertNewline(_:)) {
            search(backwards: NSApp.currentEvent?.modifierFlags.contains(.shift) == true, restart: false)
            return true
        }
        return false
    }

    @objc private func findNext(_ sender: Any?) { search(backwards: false, restart: false) }
    @objc private func findPrevious(_ sender: Any?) { search(backwards: true, restart: false) }

    @objc private func close(_ sender: Any?) {
        generation += 1
        pollTask?.cancel()
        pollTask = nil
        visible = false
        onVisibilityChange?(false)
        if let webView { webView.window?.makeFirstResponder(webView) }
        let request = generation
        clearMatches { [weak self] in
            guard let self, self.generation == request, !self.visible,
                  let webView = self.webView, webView.window?.firstResponder === webView else { return }
            // Restoring the native responder alone does not restore the page's reading context.
            webView.evaluateJavaScript("""
            if (window.__trackNativeReading === true) {
              window.dispatchEvent(new Event('track:restore-reading-focus'));
            }
            """)
        }
    }

    private func search(backwards: Bool, restart: Bool, delta: Int? = nil) {
        guard let webView else { return }
        generation += 1
        let request = generation
        let query = field.stringValue
        lastQuery = query
        if delta == nil { updateButtons() }
        guard let data = try? JSONSerialization.data(withJSONObject: [query, delta ?? (restart ? 0 : backwards ? -1 : 1), restart]),
              let arguments = String(data: data, encoding: .utf8) else { return }
        inFlight = request
        webView.evaluateJavaScript(
            PageFindScript.source + "\nglobalThis.__trackPageFind.update(..." + arguments + ");",
            in: nil, in: .defaultClient
        ) { [weak self] result in
            guard let self else { return }
            if self.inFlight == request { self.inFlight = nil }
            guard self.generation == request else { return }
            guard case .success(let value) = result, let state = value as? [String: Any] else {
                self.status.stringValue = "Find unavailable"
                return
            }
            self.onLayoutChange?(CGFloat(state["tabBottom"] as? Double ?? 0) + 8)
            self.bar.appearance = NSAppearance(named: state["dark"] as? Bool == true ? .darkAqua : .aqua)
            self.usesLegacyFind = state["supported"] as? Bool != true
            if self.usesLegacyFind {
                self.legacySearch(query, backwards: backwards, restart: restart, request: request)
                return
            }
            let count = state["count"] as? Int ?? 0
            let index = state["index"] as? Int ?? -1
            self.status.stringValue = query.isEmpty ? "" : count == 0 ? "No matches" : "\(index + 1) of \(count)"
            self.previous.isEnabled = count > 0
            self.next.isEnabled = count > 0
        }
    }

    // macOS 14.0/14.1 WebKit predates CSS Highlights. Retain its existing single-match Find.
    private func legacySearch(_ query: String, backwards: Bool, restart: Bool, request: Int) {
        guard let webView else { return }
        guard !query.isEmpty else { clearSelection(); status.stringValue = ""; return }
        let find: @MainActor @Sendable () -> Void = { [weak self, weak webView] in
            guard let self, let webView, self.generation == request else { return }
            let configuration = WKFindConfiguration()
            configuration.backwards = backwards
            configuration.caseSensitive = false
            configuration.wraps = true
            webView.find(query, configuration: configuration) { [weak self] result in
                guard let self, self.generation == request else { return }
                self.status.stringValue = result.matchFound ? "Match found" : "No matches"
                if !result.matchFound { self.clearSelection() }
            }
        }
        if restart { clearSelection(completion: find) } else { find() }
    }

    private func clearMatches(completion: @escaping @MainActor @Sendable () -> Void) {
        guard let webView else { completion(); return }
        if usesLegacyFind { clearSelection(completion: completion); return }
        webView.evaluateJavaScript("globalThis.__trackPageFind?.clear(); undefined", in: nil, in: .defaultClient) { _ in completion() }
    }

    private func clearSelection(completion: (@MainActor @Sendable () -> Void)? = nil) {
        guard let webView else { completion?(); return }
        webView.evaluateJavaScript("window.getSelection()?.removeAllRanges(); undefined", in: nil, in: .defaultClient) { _ in completion?() }
    }

    private func updateButtons() {
        previous.isEnabled = !field.stringValue.isEmpty
        next.isEnabled = !field.stringValue.isEmpty
    }
}
