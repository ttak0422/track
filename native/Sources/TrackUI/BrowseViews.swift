import SwiftUI
import TrackAPI

// Browsing surfaces: the whole-vault hierarchy tree, a tag index, and an
// activity heatmap. The hierarchy and tags come from getHierarchy/listNotes;
// the heatmap derives its day counts from the notes listing's activity `days`
// (listNotes), not a dedicated activity endpoint.

// MARK: - Browse model

@MainActor
@Observable
public final class BrowseModel {
    public private(set) var hierarchy: [HierarchyNode] = []
    public private(set) var notes: [SearchResult] = []
    public private(set) var isLoading = false
    public private(set) var error: String?

    private let client: TrackClient

    public init(client: TrackClient) {
        self.client = client
    }

    public func loadHierarchy() async {
        isLoading = true
        defer { isLoading = false }
        error = nil
        do {
            hierarchy = try await client.getHierarchy().hierarchy
        } catch {
            self.error = error.localizedDescription
        }
    }

    public func loadNotes() async {
        isLoading = true
        defer { isLoading = false }
        error = nil
        do {
            notes = try await client.listNotes().notes
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Every tag across the notes with its frequency, sorted by count
    /// descending then tag name ascending. Nil/absent tags contribute nothing.
    public static func tagFrequencies(notes: [SearchResult]) -> [(tag: String, count: Int)] {
        var counts: [String: Int] = [:]
        for note in notes {
            for tag in note.tags ?? [] {
                counts[tag, default: 0] += 1
            }
        }
        return counts
            .map { (tag: $0.key, count: $0.value) }
            .sorted { $0.count != $1.count ? $0.count > $1.count : $0.tag < $1.tag }
    }
}

// MARK: - Hierarchy view

public struct HierarchyView: View {
    @Bindable var model: BrowseModel
    let onSelect: (String) -> Void

    public init(model: BrowseModel, onSelect: @escaping (String) -> Void = { _ in }) {
        self.model = model
        self.onSelect = onSelect
    }

    public var body: some View {
        Group {
            if model.isLoading && model.hierarchy.isEmpty {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = model.error, model.hierarchy.isEmpty {
                ContentUnavailableView("Could not load hierarchy", systemImage: "exclamationmark.triangle", description: Text(error))
            } else if model.hierarchy.isEmpty {
                Text("No hierarchy")
                    .font(.caption).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                List(model.hierarchy, id: \.ref.noteID) { node in
                    HierarchyRow(node: node, onSelect: onSelect)
                }
            }
        }
        .task { await model.loadHierarchy() }
    }
}

private struct HierarchyRow: View {
    let node: HierarchyNode
    let onSelect: (String) -> Void

    var body: some View {
        if let children = node.children, !children.isEmpty {
            DisclosureGroup {
                ForEach(children, id: \.ref.noteID) { child in
                    HierarchyRow(node: child, onSelect: onSelect)
                }
            } label: {
                Button(node.ref.title) { onSelect(node.ref.noteID.raw) }
                    .buttonStyle(.plain)
            }
        } else {
            Button(node.ref.title) { onSelect(node.ref.noteID.raw) }
                .buttonStyle(.plain)
        }
    }
}

// MARK: - Tag view

public struct TagView: View {
    @Bindable var model: BrowseModel
    @State private var selectedTag: String?

    public init(model: BrowseModel) {
        self.model = model
    }

    public var body: some View {
        Group {
            if model.isLoading && model.notes.isEmpty {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = model.error, model.notes.isEmpty {
                ContentUnavailableView("Could not load notes", systemImage: "exclamationmark.triangle", description: Text(error))
            } else {
                let tags = BrowseModel.tagFrequencies(notes: model.notes)
                HStack(alignment: .top, spacing: 0) {
                    List(tags, id: \.tag) { entry in
                        Button {
                            selectedTag = entry.tag
                        } label: {
                            HStack {
                                Text("#\(entry.tag)")
                                Spacer()
                                Text("\(entry.count)").font(.caption).foregroundStyle(.secondary)
                            }
                            .foregroundStyle(selectedTag == entry.tag ? Color.accentColor : Color.primary)
                        }
                        .buttonStyle(.plain)
                    }
                    Divider()
                    List(notes(for: selectedTag), id: \.ref.noteID) { note in
                        Text(note.ref.title)
                    }
                }
            }
        }
        .task { await model.loadNotes() }
    }

    private func notes(for tag: String?) -> [SearchResult] {
        guard let tag else { return [] }
        return model.notes.filter { $0.tags?.contains(tag) == true }
    }
}

// MARK: - Activity heatmap view

public struct ActivityHeatmapView: View {
    @Bindable var model: BrowseModel

    private let columns = Array(repeating: GridItem(.fixed(14), spacing: 4), count: 7)

    public init(model: BrowseModel) {
        self.model = model
    }

    public var body: some View {
        Group {
            if model.isLoading && model.notes.isEmpty {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = model.error, model.notes.isEmpty {
                ContentUnavailableView("Could not load activity", systemImage: "exclamationmark.triangle", description: Text(error))
            } else {
                let counts = Self.dayCounts(notes: model.notes)
                let days = Self.lastDays(28)
                VStack(alignment: .leading, spacing: 6) {
                    Text("Last 28 days").font(.caption).foregroundStyle(.secondary)
                    LazyVGrid(columns: columns, alignment: .leading, spacing: 4) {
                        ForEach(days, id: \.self) { day in
                            RoundedRectangle(cornerRadius: 2)
                                .fill(color(for: counts[day] ?? 0))
                                .frame(width: 12, height: 12)
                        }
                    }
                }
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task { await model.loadNotes() }
    }

    private func color(for count: Int) -> Color {
        switch count {
        case 0: return Color(.quaternarySystemFill)
        case 1: return Color.accentColor.opacity(0.3)
        case 2: return Color.accentColor.opacity(0.6)
        default: return Color.accentColor
        }
    }

    /// Local-day note counts across the notes listing (`days` arrays).
    static func dayCounts(notes: [SearchResult]) -> [String: Int] {
        var counts: [String: Int] = [:]
        for note in notes {
            for day in note.days ?? [] {
                counts[day, default: 0] += 1
            }
        }
        return counts
    }

    /// The `n` consecutive local days ending today, oldest first.
    static func lastDays(_ n: Int) -> [String] {
        let cal = Calendar.current
        let today = cal.startOfDay(for: Date())
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        return (0..<n).reversed().compactMap { offset in
            cal.date(byAdding: .day, value: -offset, to: today).map(f.string(from:))
        }
    }
}
