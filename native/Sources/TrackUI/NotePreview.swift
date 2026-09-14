import AppKit
import SwiftUI
import TrackAPI

/// A preview has its own reader transaction and never changes the source note.
@MainActor
@Observable
public final class NotePreviewModel {
    public let reader: NoteReaderModel
    public private(set) var error: String?
    public private(set) var isLoading = false
    private var request = UUID()

    public init(client: TrackClient) { reader = NoteReaderModel(client: client) }

    public func load(id: TrackID? = nil, target: String = "", sourceID: TrackID? = nil) async {
        let token = UUID()
        request = token
        isLoading = true
        error = nil
        defer { if request == token { isLoading = false } }
        if let id {
            let opened = await reader.open(id)
            guard request == token, !Task.isCancelled else { return }
            if !opened { error = reader.saveError ?? "Could not load note" }
            return
        }
        let parsed = MarkdownAnchors.target(target)
        if parsed.key.isEmpty, let sourceID {
            let opened = await reader.open(sourceID)
            guard request == token, !Task.isCancelled else { return }
            if opened { reader.scroll(to: parsed.anchor) } else { error = "Could not load note" }
        } else {
            let vault = sourceID?.split().vault ?? ""
            let scoped = !vault.isEmpty && !parsed.key.contains(":") && !parsed.key.contains("~") ? "\(vault):\(target)" : target
            let opened = await reader.openWikilink(target: scoped)
            guard request == token, !Task.isCancelled else { return }
            if !opened { error = reader.saveError ?? "Note not found: \(parsed.key)" }
        }
    }
}

/// Native panels supply resizing, screen constraints, focus, and keyboard
/// closing. The coordinator owns panels after their source row disappears.
@MainActor
final class NotePreviewWindows {
    static let shared = NotePreviewWindows()
    private var pending: Task<Void, Never>?
    private var pendingID: UUID?
    private var sessions: [UUID: PreviewSession] = [:]

    @discardableResult
    func show(client: TrackClient, id: TrackID? = nil, target: String = "", sourceID: TrackID? = nil, pinned: Bool = false, onOpen: @escaping (TrackID) -> Void) -> UUID {
        let token = UUID()
        if pinned {
            present(token, client: client, id: id, target: target, sourceID: sourceID, pinned: true, onOpen: onOpen)
        } else {
            pending?.cancel()
            if let old = pendingID, sessions[old]?.pinned == false { close(old) }
            pendingID = token
            pending = Task { [weak self] in
                try? await Task.sleep(for: .milliseconds(350))
                guard !Task.isCancelled else { return }
                self?.present(token, client: client, id: id, target: target, sourceID: sourceID, pinned: false, onOpen: onOpen)
            }
        }
        return token
    }

    private func present(_ token: UUID, client: TrackClient, id: TrackID?, target: String, sourceID: TrackID?, pinned: Bool, onOpen: @escaping (TrackID) -> Void) {
        let session = PreviewSession(client: client, pinned: pinned)
        sessions[token] = session
        session.close = { [weak self] in self?.close(token) }
        session.leave = { [weak self] in self?.leave(token) }
        session.panel.title = id?.raw ?? target
        let host = NSHostingView(rootView: NotePreviewContent(session: session, onOpen: onOpen)
            .task { await session.model.load(id: id, target: target, sourceID: sourceID) })
        host.sizingOptions = []
        host.autoresizingMask = [.width, .height]
        session.panel.contentView = host
        let screen = NSScreen.screens.first { $0.frame.contains(NSEvent.mouseLocation) } ?? NSScreen.main
        let visible = screen?.visibleFrame ?? .zero
        let bounds = visible.width >= 320 && visible.height >= 220 ? visible : CGRect(x: 0, y: 0, width: 1000, height: 800)
        let point = NSEvent.mouseLocation
        let size = CGSize(width: min(460, bounds.width), height: min(460, bounds.height))
        let frame = CGRect(x: min(max(bounds.minX, point.x + 12), bounds.maxX - size.width), y: min(max(bounds.minY, point.y - size.height), bounds.maxY - size.height), width: size.width, height: size.height)
        session.panel.setFrame(frame, display: true)
        session.panel.delegate = session
        session.panel.orderFront(nil)
    }

    func leave(_ token: UUID?) {
        guard let token else { return }
        if pendingID == token { pending?.cancel() }
        Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(250))
            guard let self, let session = sessions[token], !session.pinned,
                  !session.panel.frame.contains(NSEvent.mouseLocation) else { return }
            close(token)
        }
    }

    private func close(_ id: UUID) {
        guard let session = sessions.removeValue(forKey: id) else { return }
        session.panel.delegate = nil
        session.panel.close()
        session.panel.contentView = nil
    }
}

private final class PreviewPanel: NSPanel {
    override func constrainFrameRect(_ frameRect: NSRect, to screen: NSScreen?) -> NSRect {
        // No display is reported briefly during display changes (and in CLT
        // snapshot checks); retain the requested usable frame until it returns.
        guard let screen, screen.visibleFrame.width > 0, screen.visibleFrame.height > 0 else { return frameRect }
        return super.constrainFrameRect(frameRect, to: screen)
    }
    override func cancelOperation(_ sender: Any?) { performClose(sender) }
}

@MainActor
@Observable
private final class PreviewSession: NSObject, NSWindowDelegate {
    let model: NotePreviewModel
    let panel: NSPanel
    var pinned: Bool
    var close: () -> Void = {}
    var leave: () -> Void = {}

    init(client: TrackClient, pinned: Bool) {
        model = NotePreviewModel(client: client)
        self.pinned = pinned
        panel = PreviewPanel(contentRect: CGRect(x: 0, y: 0, width: 460, height: 428), styleMask: [.titled, .closable, .resizable, .nonactivatingPanel], backing: .buffered, defer: false)
        super.init()
        panel.isFloatingPanel = true
        panel.hidesOnDeactivate = true
        panel.isReleasedWhenClosed = false
        panel.minSize = CGSize(width: 320, height: 220)
        panel.isMovableByWindowBackground = false
    }

    func windowWillClose(_ notification: Notification) { close() }
    func windowDidMove(_ notification: Notification) { pinned = true }
    func windowDidResize(_ notification: Notification) { pinned = true }
}

private struct NotePreviewContent: View {
    @Bindable var session: PreviewSession
    let onOpen: (TrackID) -> Void
    @State private var navigationTask: Task<Void, Never>?
    @Environment(\.colorScheme) private var colorScheme
    @AppStorage(TrackAppearance.themeKey) private var themeRaw: String?
    @AppStorage(TrackAppearance.previewFontSizeKey) private var previewFontSize: Double = 13

    private var title: String {
        if case .loaded(let response) = session.model.reader.state { return response.note.summary.ref.title }
        return "Preview"
    }

    var body: some View {
        let reader = session.model.reader
        VStack(spacing: 0) {
            HStack {
                Text(title).font(.headline).lineLimit(1)
                Spacer()
                Button { session.pinned.toggle() } label: { Image(systemName: session.pinned ? "pin.fill" : "pin") }
                    .help(session.pinned ? "Unpin preview" : "Pin preview")
                    .accessibilityLabel(session.pinned ? "Unpin preview" : "Pin preview")
                Button("Open") { if let id = reader.currentID { onOpen(id) } }
                    .disabled(!reader.isLoaded)
                Button { session.close() } label: { Image(systemName: "xmark") }
                    .accessibilityLabel("Close preview")
            }
            .padding(10)
            Divider()
            if session.model.isLoading {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = session.model.error {
                ContentUnavailableView("Preview unavailable", systemImage: "doc.text.magnifyingglass", description: Text(error))
            } else if reader.isLoaded {
                ScrollViewReader { proxy in
                    ScrollView {
                        GFMBody(markdown: reader.didRender ? reader.renderedBody : reader.loadedBody,
                                baseURL: reader.client.baseURL, vault: reader.currentID?.split().vault ?? "",
                                noteID: reader.currentID, includes: reader.renderedIncludes, client: reader.client,
                                onWikilink: navigate)
                            .padding(16)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .task(id: reader.scrollRequest) { await Task.yield(); if let id = reader.scrollTarget { proxy.scrollTo(id, anchor: .top) } }
                }
                .environment(\.trackFontScale, TrackAppearance.scale(forFontSize: previewFontSize))
                .environment(\.openURL, OpenURLAction { url in
                    if let target = MarkdownAnchors.wikiTarget(url) { navigate(target); return .handled }
                    if url.scheme == "trackanchor" { reader.scroll(to: String(url.path.dropFirst())); return .handled }
                    return .systemAction
                })
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(TrackTheme.palette(for: colorScheme).bg)
        .preferredColorScheme(ThemeMode(stored: themeRaw).preferredColorScheme)
        .onHover { if !$0 { session.leave() } }
        .onDisappear { navigationTask?.cancel() }
    }

    private func navigate(_ target: String) {
        navigationTask?.cancel()
        navigationTask = Task { await session.model.load(target: target, sourceID: session.model.reader.currentID) }
    }
}

private struct NotePreviewHover: ViewModifier {
    let client: TrackClient
    let id: TrackID?
    let target: String
    let sourceID: TrackID?
    let onOpen: (TrackID) -> Void
    @State private var token: UUID?

    func body(content: Content) -> some View {
        content.onHover { inside in
            if inside { token = NotePreviewWindows.shared.show(client: client, id: id, target: target, sourceID: sourceID, onOpen: onOpen) }
            else { NotePreviewWindows.shared.leave(token) }
        }
        .onDisappear { NotePreviewWindows.shared.leave(token) }
        .contextMenu {
            Button("Preview") { NotePreviewWindows.shared.show(client: client, id: id, target: target, sourceID: sourceID, pinned: true, onOpen: onOpen) }
        }
    }
}

extension View {
    func notePreview(client: TrackClient, id: TrackID? = nil, target: String = "", sourceID: TrackID? = nil, onOpen: @escaping (TrackID) -> Void) -> some View {
        modifier(NotePreviewHover(client: client, id: id, target: target, sourceID: sourceID, onOpen: onOpen))
    }
}
