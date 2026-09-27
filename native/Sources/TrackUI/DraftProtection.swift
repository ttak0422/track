import AppKit
import SwiftUI

/// Preserve SwiftUI's delegates while intercepting document close and app quit.
struct WindowDraftProtection: NSViewRepresentable {
    let model: NoteReaderModel

    func makeNSView(context: Context) -> Observer { Observer(model: model) }
    func updateNSView(_ view: Observer, context: Context) {
        view.window?.isDocumentEdited = model.isDirty
    }

    final class Observer: NSView {
        let model: NoteReaderModel
        private weak var registeredWindow: NSWindow?
        init(model: NoteReaderModel) {
            self.model = model
            super.init(frame: .zero)
        }
        required init?(coder: NSCoder) { fatalError("init(coder:) is unavailable") }
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if let registeredWindow { DraftWindows.shared.unregister(model, window: registeredWindow) }
            registeredWindow = window
            if let window { DraftWindows.shared.register(model, window: window) }
        }
    }
}

@MainActor
private final class DraftWindows: NSObject, NSApplicationDelegate {
    static let shared = DraftWindows()
    private var windows: [WindowDelegate] = []
    // Objective-C delegate introspection is nonisolated; registration stays on the main actor.
    nonisolated(unsafe) private weak var original: (any NSApplicationDelegate)?

    func register(_ model: NoteReaderModel, window: NSWindow) {
        windows.removeAll { $0.window == nil }
        let delegate: WindowDelegate
        if let existing = windows.first(where: { $0.window === window }) {
            delegate = existing
        } else {
            delegate = WindowDelegate(window: window)
            windows.append(delegate)
            window.delegate = delegate
        }
        delegate.register(model)
        if NSApp.delegate !== self {
            original = NSApp.delegate
            NSApp.delegate = self
        }
    }

    func unregister(_ model: NoteReaderModel, window: NSWindow) {
        windows.first { $0.window === window }?.unregister(model)
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard windows.allSatisfy({ $0.authorizeClose() }) else { return .terminateCancel }
        return original?.applicationShouldTerminate?(sender) ?? .terminateNow
    }

    override func responds(to selector: Selector!) -> Bool {
        super.responds(to: selector) || original?.responds(to: selector) == true
    }
    override func forwardingTarget(for selector: Selector!) -> Any? { original }
}

@MainActor
private final class WindowDelegate: NSObject, NSWindowDelegate {
    weak var window: NSWindow?
    // Objective-C delegate introspection is nonisolated; registration stays on the main actor.
    nonisolated(unsafe) private weak var original: (any NSWindowDelegate)?
    private struct Reader { weak var model: NoteReaderModel? }
    private var readers: [Reader] = []

    init(window: NSWindow) {
        self.window = window
        original = window.delegate
    }
    func register(_ model: NoteReaderModel) {
        readers.removeAll { $0.model == nil }
        if !readers.contains(where: { $0.model === model }) { readers.append(Reader(model: model)) }
    }
    func unregister(_ model: NoteReaderModel) {
        readers.removeAll { $0.model == nil || $0.model === model }
    }
    func authorizeClose() -> Bool {
        guard window != nil else { return true }
        return readers.allSatisfy { $0.model?.authorizeDiscard() ?? true }
    }
    func windowShouldClose(_ sender: NSWindow) -> Bool {
        authorizeClose() && (original?.windowShouldClose?(sender) ?? true)
    }
    override func responds(to selector: Selector!) -> Bool {
        super.responds(to: selector) || original?.responds(to: selector) == true
    }
    override func forwardingTarget(for selector: Selector!) -> Any? { original }
}
