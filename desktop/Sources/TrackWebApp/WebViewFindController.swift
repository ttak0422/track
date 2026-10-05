import AppKit
import WebKit

/// AppKit owns the input; WebKit owns finding, scrolling, and painting the selected page text.
/// WKWebView is not an NSTextFinderClient. Assigning it to NSTextFinder.client can present a bar
/// without connecting edits in that bar to the web document.
@MainActor
final class WebViewFindController: NSObject, NSSearchFieldDelegate {
    let bar = NSView(frame: NSRect(x: 0, y: 0, width: 600, height: 42))
    var onVisibilityChange: ((Bool) -> Void)?

    private weak var webView: WKWebView?
    private let field = NSSearchField(frame: .zero)
    private let status = NSTextField(labelWithString: "")
    private let previous = NSButton(title: "Previous", target: nil, action: nil)
    private let next = NSButton(title: "Next", target: nil, action: nil)
    private var visible = false
    private var generation = 0
    private var lastQuery = ""

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
        let stack = NSStackView(views: [field, status, previous, next, done])
        stack.orientation = .horizontal
        stack.spacing = 8
        stack.alignment = .centerY
        stack.translatesAutoresizingMaskIntoConstraints = false
        bar.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: bar.leadingAnchor, constant: 12),
            stack.trailingAnchor.constraint(equalTo: bar.trailingAnchor, constant: -12),
            stack.centerYAnchor.constraint(equalTo: bar.centerYAnchor),
            field.widthAnchor.constraint(greaterThanOrEqualToConstant: 180),
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
            if !wasVisible && !field.stringValue.isEmpty { search(backwards: false, restart: true) }
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
        visible = false
        onVisibilityChange?(false)
        clearSelection()
        if let webView { webView.window?.makeFirstResponder(webView) }
    }

    private func search(backwards: Bool, restart: Bool) {
        guard let webView else { return }
        generation += 1
        let request = generation
        let query = field.stringValue
        lastQuery = query
        updateButtons()
        status.stringValue = query.isEmpty ? "" : "Searching…"
        status.textColor = .secondaryLabelColor
        guard !query.isEmpty else {
            clearSelection()
            return
        }
        let find: @MainActor @Sendable () -> Void = { [weak self, weak webView] in
            guard let self, let webView, self.generation == request else { return }
            let configuration = WKFindConfiguration()
            configuration.backwards = backwards
            configuration.caseSensitive = false
            configuration.wraps = true
            webView.find(query, configuration: configuration) { [weak self] result in
                guard let self, self.generation == request else { return }
                if result.matchFound {
                    self.status.stringValue = "Match found"
                } else {
                    // Public WKWebView.find does not clear the previous match on an empty or
                    // missing query. Remove it so stale highlighted text cannot look like a hit.
                    self.clearSelection { [weak self] in
                        guard let self, self.generation == request else { return }
                        self.status.stringValue = "No matches"
                        self.status.textColor = .secondaryLabelColor
                    }
                }
            }
        }
        if restart {
            // Incremental queries begin at the first page match, rather than skipping to the next
            // occurrence on every keystroke. Clear in the isolated client world; no page bridge.
            clearSelection(completion: find)
        } else {
            find()
        }
    }

    private func clearSelection(completion: (@MainActor @Sendable () -> Void)? = nil) {
        guard let webView else { completion?(); return }
        webView.evaluateJavaScript(
            "window.getSelection()?.removeAllRanges(); undefined",
            in: nil,
            in: .defaultClient
        ) { _ in completion?() }
    }

    private func updateButtons() {
        previous.isEnabled = !field.stringValue.isEmpty
        next.isEnabled = !field.stringValue.isEmpty
    }
}
