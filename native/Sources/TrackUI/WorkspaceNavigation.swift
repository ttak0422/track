import Foundation
import Observation
import TrackAPI

public enum WorkspaceSurface: Hashable, CaseIterable {
    case notes, calendar, graph, browse, tasks, voice, requests, settings

    public var title: String {
        switch self {
        case .notes: "Notes"
        case .calendar: "Calendar"
        case .graph: "Graph"
        case .browse: "Browse"
        case .tasks: "Tasks"
        case .voice: "Voice"
        case .requests: "Agent requests"
        case .settings: "Settings"
        }
    }
    public var symbol: String {
        switch self {
        case .notes: "doc.text"
        case .calendar: "calendar"
        case .graph: "network"
        case .browse: "folder"
        case .tasks: "checklist"
        case .voice: "mic"
        case .requests: "bubble.left.and.text.bubble.right"
        case .settings: "gearshape"
        }
    }
}

/// One reader per window. Surface state stays in its mounted view; Back changes
/// the visible surface and restores the previous note through the same dirty guard.
@MainActor
@Observable
public final class WorkspaceNavigation {
    public let reader: NoteReaderModel
    public private(set) var selected: WorkspaceSurface = .notes
    public private(set) var visited: Set<WorkspaceSurface> = [.notes]
    private struct Location: Equatable { let surface: WorkspaceSurface; let note: TrackID? }
    private var location = Location(surface: .notes, note: nil)
    private var history: [Location] = []
    public var canGoBack: Bool { !history.isEmpty }

    public init(reader: NoteReaderModel) { self.reader = reader }

    public func select(_ surface: WorkspaceSurface) {
        show(Location(surface: surface, note: surface == .notes ? reader.currentID : nil))
    }

    /// Also receives direct search/tab/wikilink opens from the shared reader.
    public func readerDidOpen(_ id: TrackID?) {
        guard let id else { return }
        show(Location(surface: .notes, note: id))
    }

    @discardableResult
    public func open(_ id: TrackID) async -> Bool {
        if reader.currentID != id, !(await reader.open(id)) { return false }
        readerDidOpen(id)
        return true
    }

    @discardableResult
    public func back() async -> Bool {
        guard let target = history.last else { return false }
        if let id = target.note, reader.currentID != id, !(await reader.open(id)) { return false }
        history.removeLast()
        show(target, remember: false)
        return true
    }

    /// A numeric ID is already resolved. Qualified IDs retain their source
    /// vault; title/anchor targets continue through the engine's wiki resolver.
    public static func directNoteID(_ target: String) -> TrackID? {
        guard !target.contains("#") else { return nil }
        let id = TrackID(target)
        let bare = id.split().id
        if target.contains("~") { return !id.split().vault.isEmpty && !bare.isEmpty ? id : nil }
        return !bare.isEmpty && bare.utf8.allSatisfy { (48...57).contains($0) } ? id : nil
    }

    private func show(_ target: Location, remember: Bool = true) {
        guard location != target else { return }
        if remember, location.surface != .notes || location.note != nil { history.append(location) }
        location = target
        selected = target.surface
        visited.insert(target.surface)
    }
}
