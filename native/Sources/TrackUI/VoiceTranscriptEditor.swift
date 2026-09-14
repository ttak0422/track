import AppKit
import SwiftUI

/// NSTextView provides native selection, undo and IME. Speech arrivals preserve
/// the viewport and caret while editing; an idle view follows only from its tail.
struct VoiceTranscriptEditor: NSViewRepresentable {
    var text: String
    var onEdit: (String) -> Void
    var onSelection: (String) -> Void
    var onInteraction: (Bool) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(self) }
    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSScrollView()
        let editor = TranscriptTextView()
        editor.isRichText = false
        editor.isAutomaticQuoteSubstitutionEnabled = false
        editor.isAutomaticDashSubstitutionEnabled = false
        editor.allowsUndo = true
        editor.font = NSFont.preferredFont(forTextStyle: .body)
        editor.textContainerInset = NSSize(width: 8, height: 10)
        editor.isVerticallyResizable = true
        editor.isHorizontallyResizable = false
        editor.autoresizingMask = [.width]
        editor.textContainer?.widthTracksTextView = true
        editor.textContainer?.containerSize = NSSize(width: 0, height: CGFloat.greatestFiniteMagnitude)
        editor.setAccessibilityLabel("Voice transcript")
        editor.delegate = context.coordinator
        editor.interaction = { active in context.coordinator.parent.onInteraction(active) }
        scroll.documentView = editor
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = true
        return scroll
    }
    func updateNSView(_ scroll: NSScrollView, context: Context) {
        context.coordinator.parent = self
        guard let editor = scroll.documentView as? NSTextView, editor.string != text, !editor.hasMarkedText() else { return }
        let selection = editor.selectedRange()
        let visible = scroll.contentView.bounds
        let follows = editor.window?.firstResponder !== editor && editor.bounds.maxY - visible.maxY < 24
        context.coordinator.isUpdating = true
        editor.string = text
        let count = (text as NSString).length
        let location = min(selection.location, count)
        editor.setSelectedRange(NSRange(location: location, length: min(selection.length, count - location)))
        editor.layoutManager?.ensureLayout(for: editor.textContainer!)
        if follows { editor.scrollRangeToVisible(NSRange(location: count, length: 0)) }
        else { scroll.contentView.scroll(to: visible.origin); scroll.reflectScrolledClipView(scroll.contentView) }
        context.coordinator.isUpdating = false
        let selected = VoiceTranscriptState.selectedText(in: editor.string, range: editor.selectedRange())
        let onSelection = self.onSelection
        Task { @MainActor in onSelection(selected) }
    }
    final class Coordinator: NSObject, NSTextViewDelegate {
        var parent: VoiceTranscriptEditor
        var isUpdating = false
        init(_ parent: VoiceTranscriptEditor) { self.parent = parent }
        func textDidChange(_ notification: Notification) {
            guard let editor = notification.object as? NSTextView, !isUpdating else { return }
            if editor.hasMarkedText() { parent.onInteraction(true); return }
            parent.onEdit(editor.string)
            parent.onInteraction(false)
            updateSelection(editor)
        }
        func textViewDidChangeSelection(_ notification: Notification) {
            guard let editor = notification.object as? NSTextView, !isUpdating, !editor.hasMarkedText() else { return }
            updateSelection(editor)
        }
        private func updateSelection(_ editor: NSTextView) {
            parent.onSelection(VoiceTranscriptState.selectedText(in: editor.string, range: editor.selectedRange()))
        }
    }
    final class TranscriptTextView: NSTextView {
        var interaction: ((Bool) -> Void)?
        override func mouseDown(with event: NSEvent) {
            interaction?(true)
            super.mouseDown(with: event)
            interaction?(false)
        }
    }
}
