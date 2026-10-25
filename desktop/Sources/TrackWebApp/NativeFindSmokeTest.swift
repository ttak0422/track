import AppKit
import WebKit

/// Drive the same field editor and menu shortcuts as a person. Never call WKWebView.find here:
/// doing so bypasses the input-to-search connection that this regression is meant to protect.
@MainActor
final class NativeFindSmokeTest {
    private let webView: WKWebView
    private let window: NSWindow
    private let bar: () -> NSView?
    private let dispatch: (NSTextFinder.Action) -> Bool
    private var checks: [String: Bool] = [:]

    init(webView: WKWebView, window: NSWindow, bar: @escaping () -> NSView?, dispatch: @escaping (NSTextFinder.Action) -> Bool) {
        self.webView = webView
        self.window = window
        self.bar = bar
        self.dispatch = dispatch
    }

    func run(menuConfigured: Bool) async -> [String: Any] {
        checks["menuConfigured"] = menuConfigured
        do {
            try require(try await pageBool("""
            (() => {
              window.__trackReadingRestores = 0;
              window.addEventListener('track:restore-reading-focus', () => window.__trackReadingRestores++);
              return window.__trackNativeReading === true;
            })()
            """), "nativeReadingMarker")
            let originalFrame = webView.frame
            try require(try await pageBool("window.__tabBeforeFind = document.querySelector('.tabstrip').getBoundingClientRect().top; true"), "tabPositionCaptured")
            try require(dispatch(.showFindInterface), "commandFDispatched")
            try await waitUntil { self.bar().map { !$0.isHidden && $0.frame.height > 0 } == true }
            try require(true, "nativeFindUIVisible")
            let tabUnmoved = try await pageBool("document.querySelector('.tabstrip').getBoundingClientRect().top === window.__tabBeforeFind")
            try require(webView.frame == originalFrame && tabUnmoved, "findDoesNotMoveTabbar")
            let originalSize = window.contentView!.bounds.size
            window.setContentSize(NSSize(width: 800, height: 560))
            window.contentView?.layoutSubtreeIfNeeded()
            try await waitUntil { self.webView.frame.size == self.window.contentView?.bounds.size }
            try require(bar().map { $0.frame.minX >= 0 && $0.frame.maxX <= 800 && $0.frame.maxY <= 560 - 33 } == true, "findResizeKeepsTabTargets")
            window.setContentSize(originalSize)
            window.contentView?.layoutSubtreeIfNeeded()
            try await waitUntil { self.webView.frame == originalFrame }
            let before = try await snapshot()
            try enter("TrackSearchSmokeMarker")
            let first = try await selection(matching: "TrackSearchSmokeMarker")
            try require(first.id == "english-first", "queryEnteredThroughField")
            try require(true, "queryMatched")
            try require(true, "selectionTextMatches")
            try require(try await pageBool("CSS.highlights.get('track-find-all').size === 2 && CSS.highlights.get('track-find-current').size === 1", isolated: true), "allMatchesHighlighted")
            // The page is static and both occurrences are above the fold. A pixel change confined
            // to the selected text proves that a highlight is painted while the search field owns focus.
            let after = try await snapshot()
            try require(highlightChanged(before: before, after: after, selection: first), "visibleHighlight")
            let other = try await readSelection(allIndex: 1)
            try require(highlightChanged(before: before, after: after, selection: other), "otherMatchPainted")
            let activePixels = after.colorAt(x: Int((first.x + first.width / 2) * Double(after.pixelsWide) / Double(webView.bounds.width)), y: Int((first.y + 1) * Double(after.pixelsHigh) / Double(webView.bounds.height)))
            let otherPixels = after.colorAt(x: Int((other.x + other.width / 2) * Double(after.pixelsWide) / Double(webView.bounds.width)), y: Int((other.y + 1) * Double(after.pixelsHigh) / Double(webView.bounds.height)))
            try require(activePixels != nil && otherPixels != nil && activePixels != otherPixels, "currentMatchDistinct")
            try require(dispatch(.nextMatch), "nextShortcutDispatched")
            let second = try await selection(matching: "TrackSearchSmokeMarker", id: "english-second")
            try require(second.id != first.id, "nextMoved")
            try require(dispatch(.previousMatch), "previousShortcutDispatched")
            _ = try await selection(matching: "TrackSearchSmokeMarker", id: "english-first")
            try require(true, "previousMoved")
            try require(dispatch(.previousMatch), "previousWrapDispatched")
            _ = try await selection(matching: "TrackSearchSmokeMarker", id: "english-second")
            try require(true, "previousWrapped")
            try require(dispatch(.nextMatch), "nextWrapDispatched")
            _ = try await selection(matching: "TrackSearchSmokeMarker", id: "english-first")
            try require(true, "nextWrapped")
            try enter("TrackSearchSmoke")
            _ = try await selection(matching: "TrackSearchSmoke", id: "english-first")
            guard let editingField = window.firstResponder as? NSTextView else { throw Failure("Find field lost focus") }
            editingField.insertText("Marker", replacementRange: editingField.selectedRange())
            _ = try await selection(matching: "TrackSearchSmokeMarker", id: "english-first")
            try require(true, "incrementalQueryMatched")
            editingField.selectAll(nil)
            editingField.setMarkedText("UncommittedNativeFind", selectedRange: NSRange(location: 0, length: 0), replacementRange: editingField.selectedRange())
            try require(editingField.hasMarkedText(), "nativeIMECompositionStarted")
            try await Task.sleep(for: .milliseconds(400))
            let duringComposition = try await readSelection()
            try require(duringComposition.text == "TrackSearchSmokeMarker", "nativeIMEKeepsCommittedMatches")
            editingField.unmarkText()
            try enter("TrackSearchSmokeMarker")
            _ = try await selection(matching: "TrackSearchSmokeMarker", id: "english-first")
            // A late no-match callback must not overwrite a newer successful query.
            try enter("NoSuchTrackSearchSmokeText")
            try enter("TrackSearchSmokeMarker")
            _ = try await selection(matching: "TrackSearchSmokeMarker", id: "english-first")
            try await waitUntil { self.statusText() == "1 of 2" }
            try require(true, "newestQueryWins")

            try enter("日本語検索")
            _ = try await selection(matching: "日本語検索", id: "japanese-first")
            try require(true, "japaneseMatched")
            try require(dispatch(.nextMatch), "japaneseNextDispatched")
            _ = try await selection(matching: "日本語検索", id: "japanese-second")
            try require(true, "japaneseNextMoved")
            try require(dispatch(.previousMatch), "japanesePreviousDispatched")
            _ = try await selection(matching: "日本語検索", id: "japanese-first")
            try require(true, "japanesePreviousMoved")

            try require(try await pageBool("""
            (() => { const range = document.createRange(); range.selectNodeContents(document.getElementById('ordinary-selection'));
              const selection = window.getSelection(); selection.removeAllRanges(); selection.addRange(range); return true; })()
            """), "ordinarySelectionCreated")
            try enter("NoSuchTrackSearchSmokeText")
            try await waitUntil { self.statusText() == "No matches" }
            let noMatch = try await readSelection()
            try require(noMatch.text.isEmpty, "noMatchClearsSelection")
            try require(try await pageBool("window.getSelection().toString() === 'Ordinary selection stays intact.'"), "ordinarySelectionPreserved")
            try require(try await pageBool("CSS.highlights.get('track-find-all').size === 0", isolated: true), "noMatchClearsHighlights")
            try require(true, "noMatchFeedback")
            try enter("")
            try await waitUntil { self.statusText() == "" }
            try require(true, "clearResetsFeedback")
            try require(try await pageBool("CSS.highlights.size === 0", isolated: true), "clearRemovesHighlights")
            try enter("FarOffscreenMarker")
            _ = try await selection(matching: "FarOffscreenMarker", id: "far")
            try require(try await pageBool("""
            (() => { const state = globalThis.__trackPageFind.snapshot();
              return state.y >= 33 && state.y + state.height <= innerHeight; })()
            """, isolated: true), "navigationScrollsMatchIntoView")
            try require(try await pageBool("document.documentElement.style.colorScheme = 'dark'; true"), "darkThemeApplied")
            try enter("TrackSearchSmokeMarker")
            _ = try await selection(matching: "TrackSearchSmokeMarker", id: "english-first")
            let darkBefore = try await snapshot()
            try require(try await pageBool("getComputedStyle(document.documentElement).colorScheme === 'dark'"), "darkThemeMatches")
            try enter("")
            try await waitUntil { self.statusText() == "" }
            let darkClear = try await snapshot()
            try require(highlightChanged(before: darkClear, after: darkBefore, selection: first), "darkHighlightsPainted")
            try require(try await pageBool("window.getSelection().removeAllRanges(); true"), "selectionReleasedForReading")
            try enter("日本語検索")
            _ = try await selection(matching: "日本語検索", id: "japanese-first")
            guard let fieldEditor = window.firstResponder as? NSTextView else { throw Failure("Find field lost focus") }
            fieldEditor.doCommand(by: #selector(NSResponder.cancelOperation(_:)))
            try await waitUntil { self.bar()?.isHidden == true }
            try require(true, "escapeCloses")
            try await readingRestored(count: 1)
            try require(window.firstResponder === webView, "escapeRestoresWebResponder")
            try require(true, "escapeRequestsReadingFocus")
            try require(dispatch(.showFindInterface), "reopenDispatched")
            try await waitUntil { self.bar().map { !$0.isHidden && $0.frame.height > 0 } == true }
            try require((window.firstResponder as? NSTextView)?.string == "日本語検索", "reopenPreservesQuery")
            _ = try await selection(matching: "日本語検索")
            try require(true, "reopenFinds")
            func done(in view: NSView) -> NSButton? {
                if let button = view as? NSButton, button.title == "Done" { return button }
                return view.subviews.compactMap { done(in: $0) }.first
            }
            guard let button = bar().flatMap({ done(in: $0) }) else { throw Failure("Find Done button missing") }
            button.performClick(nil)
            try await readingRestored(count: 2)
            try require(bar()?.isHidden == true && window.firstResponder === webView, "doneRequestsReadingFocus")
            try require(webView.frame == originalFrame, "closingFindKeepsViewport")
        } catch {
            return report(error: String(describing: error))
        }
        return report(error: nil)
    }

    private func readingRestored(count: Int) async throws {
        for _ in 0..<80 {
            if try await pageBool("window.__trackReadingRestores === \(count) && window.getSelection().isCollapsed") { return }
            try await Task.sleep(for: .milliseconds(25))
        }
        throw Failure("Find dismissal did not request reading focus after clearing selection")
    }

    private func pageBool(_ script: String, isolated: Bool = false) async throws -> Bool {
        try await withCheckedThrowingContinuation { continuation in
            webView.evaluateJavaScript(script, in: nil, in: isolated ? .defaultClient : .page) { result in
                switch result {
                case .success(let value): continuation.resume(returning: value as? Bool == true)
                case .failure(let error): continuation.resume(throwing: error)
                }
            }
        }
    }

    private func enter(_ query: String) throws {
        guard let editor = window.firstResponder as? NSTextView,
              editor.isFieldEditor else { throw Failure("Cmd+F did not focus an editable native find field") }
        editor.selectAll(nil)
        editor.insertText(query, replacementRange: editor.selectedRange())
    }

    private func selection(matching text: String, id: String? = nil) async throws -> Selection {
        for _ in 0..<80 {
            let result = try await readSelection()
            if result.text == text, id == nil || result.id == id {
                return result
            }
            try await Task.sleep(for: .milliseconds(25))
        }
        throw Failure("Find field did not select \(text)\(id.map { " in \($0)" } ?? "")")
    }

    private func readSelection(allIndex: Int? = nil) async throws -> Selection {
        let script: String
        if let allIndex {
            script = """
            (() => { const range = Array.from(CSS.highlights.get('track-find-all'))[\(allIndex)]; const rect = range.getBoundingClientRect();
              return { text: range.toString(), id: range.startContainer.parentElement.id, x: rect.x, y: rect.y, width: rect.width, height: rect.height }; })()
            """
        } else {
            script = "globalThis.__trackPageFind?.snapshot() || {}"
        }
        return try await withCheckedThrowingContinuation { continuation in
            webView.evaluateJavaScript(script, in: nil, in: .defaultClient) { result in
                switch result {
                case .success(let value): continuation.resume(returning: Selection(value))
                case .failure(let error): continuation.resume(throwing: error)
                }
            }
        }
    }

    private func snapshot() async throws -> NSBitmapImageRep {
        // Let WebKit commit its new selection before taking the public, permission-free snapshot.
        try await Task.sleep(for: .milliseconds(100))
        return try await withCheckedThrowingContinuation { continuation in
            // An explicit configuration preserves selection highlighting; nil excludes it on macOS.
            webView.takeSnapshot(with: WKSnapshotConfiguration()) { image, error in
                if let error { continuation.resume(throwing: error); return }
                guard let data = image?.tiffRepresentation, let bitmap = NSBitmapImageRep(data: data) else {
                    continuation.resume(throwing: Failure("WKWebView snapshot was empty")); return
                }
                continuation.resume(returning: bitmap)
            }
        }
    }

    private func highlightChanged(before: NSBitmapImageRep, after: NSBitmapImageRep, selection: Selection) -> Bool {
        let x = selection.x, y = selection.y, width = selection.width, height = selection.height
        guard before.pixelsWide == after.pixelsWide, before.pixelsHigh == after.pixelsHigh,
              width > 0, height > 0 else { return false }
        let scaleX = Double(after.pixelsWide) / Double(webView.bounds.width)
        let scaleY = Double(after.pixelsHigh) / Double(webView.bounds.height)
        let minX = max(0, Int(x * scaleX)), maxX = min(after.pixelsWide, Int((x + width) * scaleX))
        let minY = max(0, Int(y * scaleY)), maxY = min(after.pixelsHigh, Int((y + height) * scaleY))
        guard minX < maxX, minY < maxY else { return false }
        var changed = 0
        for row in minY..<maxY {
            for column in minX..<maxX {
                guard let old = before.colorAt(x: column, y: row)?.usingColorSpace(.deviceRGB),
                      let new = after.colorAt(x: column, y: row)?.usingColorSpace(.deviceRGB) else { continue }
                if abs(old.redComponent - new.redComponent) + abs(old.greenComponent - new.greenComponent)
                    + abs(old.blueComponent - new.blueComponent) > 0.1 { changed += 1 }
            }
        }
        return changed > (maxX - minX) * (maxY - minY) / 10
    }

    private func statusText() -> String? {
        func status(in view: NSView) -> NSTextField? {
            if view.identifier?.rawValue == "track-find-status" { return view as? NSTextField }
            return view.subviews.compactMap { status(in: $0) }.first
        }
        return bar().flatMap { status(in: $0)?.stringValue }
    }

    private func waitUntil(_ predicate: () -> Bool) async throws {
        for _ in 0..<80 {
            if predicate() { return }
            try await Task.sleep(for: .milliseconds(25))
        }
        throw Failure("Find UI did not reach the expected state")
    }

    private func require(_ passed: Bool, _ name: String) throws {
        checks[name] = passed
        if !passed { throw Failure(name) }
    }

    private func report(error: String?) -> [String: Any] {
        var result: [String: Any] = [
            "ok": error == nil && checks.values.allSatisfy { $0 } && window.tabbingMode == .disallowed && window.styleMask.contains(.titled),
            "find": checks,
            "window": ["tabbingDisabled": window.tabbingMode == .disallowed, "titlebarPreserved": window.styleMask.contains(.titled)],
        ]
        if let error { result["error"] = error }
        return result
    }

    // Convert WebKit's untyped callback payload before resuming a concurrency continuation.
    private struct Selection: Sendable {
        let text: String
        let id: String
        let x: Double
        let y: Double
        let width: Double
        let height: Double

        init(_ value: Any?) {
            let values = value as? [String: Any] ?? [:]
            text = values["text"] as? String ?? ""
            id = values["id"] as? String ?? ""
            x = values["x"] as? Double ?? 0
            y = values["y"] as? Double ?? 0
            width = values["width"] as? Double ?? 0
            height = values["height"] as? Double ?? 0
        }
    }

    private struct Failure: Error, CustomStringConvertible {
        let description: String
        init(_ description: String) { self.description = description }
    }
}
