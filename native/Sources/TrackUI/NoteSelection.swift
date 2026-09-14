import AppKit
import Observation
import SwiftUI

/// A snapshot of the actual responder selection, captured before a toolbar
/// click moves focus. Source editors keep literal Markdown; rendered selections
/// preserve native rich text. Capture never changes the user's clipboard.
@MainActor
@Observable
public final class NoteSelection {
    public private(set) var text = ""
    public private(set) var markdown = ""
    public private(set) var html: Data?
    public var isEmpty: Bool { text.isEmpty }

    public init() {}
    public func clear() { text = ""; markdown = ""; html = nil }

    public func adopt(_ selected: NSAttributedString, source: Bool) {
        text = selected.string
        markdown = source ? text : PortableMarkdown.selectedMarkdown(selected)
        html = source ? PortableMarkdown.html(markdown).data(using: .utf8) : try? selected.data(
            from: NSRange(location: 0, length: selected.length),
            documentAttributes: [.documentType: NSAttributedString.DocumentType.html]
        )
    }

    public func capture(from editor: NSTextView, source: Bool) {
        let range = editor.selectedRange()
        guard let storage = editor.textStorage, range.length > 0,
              NSMaxRange(range) <= storage.length else { clear(); return }
        adopt(storage.attributedSubstring(from: range), source: source)
    }

    public func capture(in window: NSWindow, region: NSView, source: Bool) {
        guard let responder = window.firstResponder else { clear(); return }
        let visibleRegion = region.visibleRect.intersection(region.bounds)
        if let editor = responder as? NSTextView {
            let frame = editor.convert(editor.visibleRect.intersection(editor.bounds), to: region)
            guard visibleRegion.contains(NSPoint(x: frame.midX, y: frame.midY)) else { clear(); return }
            capture(from: editor, source: source)
            return
        }
        // Read the in-process accessibility selection without invoking Copy
        // or disturbing clipboard managers/Universal Clipboard.
        let focused = window.accessibilityFocusedUIElement
        let accessible = (focused as? NSAccessibilityProtocol) ?? (responder as? NSAccessibilityProtocol)
        guard let accessible, let plain = accessible.accessibilitySelectedText(), !plain.isEmpty else { clear(); return }
        let frame = region.convert(window.convertFromScreen(accessible.accessibilityFrame()), from: nil)
        guard visibleRegion.contains(NSPoint(x: frame.midX, y: frame.midY)) else { clear(); return }
        adopt(NSAttributedString(string: plain), source: source)
    }

    public func copyMarkdown(to board: NSPasteboard = .general) {
        guard !isEmpty else { return }
        board.clearContents()
        board.setString(markdown, forType: .string)
    }

    public func copyRich(to board: NSPasteboard = .general) {
        guard !isEmpty else { return }
        board.clearContents()
        board.setString(text, forType: .string)
        if let html { board.setData(html, forType: .html) }
    }
}

struct NoteSelectionRegion: NSViewRepresentable {
    let selection: NoteSelection
    var source = false
    var active = true

    func makeNSView(context: Context) -> SelectionRegionView { SelectionRegionView() }
    func updateNSView(_ view: SelectionRegionView, context: Context) {
        view.selection = selection; view.source = source; view.active = active
    }
    static func dismantleNSView(_ view: SelectionRegionView, coordinator: ()) { view.stop() }
}

@MainActor
final class SelectionRegionView: NSView {
    weak var selection: NoteSelection?
    var source = false
    var active = true
    private var monitor: Any?
    private var tracksKeyboard = false

    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        stop()
        guard window != nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseUp, .keyUp]) { [weak self] event in
            guard let self, self.active, self.window?.isKeyWindow == true else { return event }
            if event.type == .leftMouseUp {
                self.tracksKeyboard = self.visibleRect.intersection(self.bounds).contains(self.convert(event.locationInWindow, from: nil))
            }
            guard self.tracksKeyboard else { return event }
            DispatchQueue.main.async { [weak self] in
                guard let self, self.active, let window = self.window else { return }
                self.selection?.capture(in: window, region: self, source: self.source)
            }
            return event
        }
    }
    func stop() { if let monitor { NSEvent.removeMonitor(monitor) }; monitor = nil }
}
