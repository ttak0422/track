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
    @AppStorage("track.expandedHierarchy") private var expandedJSON = "[]"

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
                    HierarchyRow(node: node, onSelect: onSelect, expandedIDs: expandedBinding, isRoot: true)
                }
            }
        }
        .task { await model.loadHierarchy() }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard !model.isLoading else { return }
            Task { await model.loadHierarchy() }
        }
    }

    private var expandedBinding: Binding<Set<String>> {
        Binding {
            Set((try? JSONDecoder().decode([String].self, from: Data(expandedJSON.utf8))) ?? [])
        } set: { value in
            expandedJSON = String(decoding: (try? JSONEncoder().encode(Array(value))) ?? Data(), as: UTF8.self)
        }
    }
}

private struct HierarchyRow: View {
    let node: HierarchyNode
    let onSelect: (String) -> Void
    @Binding var expandedIDs: Set<String>
    let isRoot: Bool

    var body: some View {
        if let children = node.children, !children.isEmpty {
            DisclosureGroup(isExpanded: Binding(
                get: { isRoot || expandedIDs.contains(node.ref.noteID.raw) },
                set: { isExpanded in
                    // Top-level hierarchy nodes mirror the web menu: they are
                    // always visible and cannot be collapsed.
                    guard !isRoot else { return }
                    if isExpanded { expandedIDs.insert(node.ref.noteID.raw) }
                    else { expandedIDs.remove(node.ref.noteID.raw) }
                }
            )) {
                ForEach(children, id: \.ref.noteID) { child in
                    HierarchyRow(node: child, onSelect: onSelect, expandedIDs: $expandedIDs, isRoot: false)
                }
            } label: {
                titleButton
            }
        } else {
            Button { onSelect(node.ref.noteID.raw) } label: {
                HStack(spacing: 4) {
                    // Keep leaf titles aligned with DisclosureGroup labels.
                    Image(systemName: "chevron.right")
                        .frame(width: 16)
                        .opacity(0)
                    Text(node.ref.title)
                }
            }
            .buttonStyle(.plain)
            .help(node.ref.title)
        }
    }

    private var titleButton: some View {
        Button(node.ref.title) { onSelect(node.ref.noteID.raw) }
            .buttonStyle(.plain)
            .help(node.ref.title)
    }
}

// MARK: - Tag view

public struct TagView: View {
    @Bindable var model: BrowseModel
    @State private var selectedTag: String?
    let onSelect: (String) -> Void
    @State private var reading = ReadingStore()
    @Environment(\.colorScheme) private var colorScheme

    public init(model: BrowseModel, onSelect: @escaping (String) -> Void = { _ in }) {
        self.model = model
        self.onSelect = onSelect
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
                            .foregroundStyle(selectedTag == entry.tag ? TrackTheme.palette(for: colorScheme).mark : Color.primary)
                        }
                        .buttonStyle(.plain)
                    }
                    Divider()
                    List(notes(for: selectedTag), id: \.ref.noteID) { note in
                        Button {
                            reading.markSeen(note.ref.noteID.raw)
                            onSelect(note.ref.noteID.raw)
                        } label: {
                            HStack(spacing: 6) {
                                Text(note.ref.title).lineLimit(1)
                                Spacer()
                                badges(for: note.ref)
                            }
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
        .task { await model.loadNotes() }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard !model.isLoading else { return }
            Task { await model.loadNotes() }
        }
    }

    private func notes(for tag: String?) -> [SearchResult] {
        guard let tag else { return [] }
        return model.notes.filter { $0.tags?.contains(tag) == true }
    }

    @ViewBuilder private func badges(for ref: NoteRef) -> some View {
        if reading.isNew(ref) {
            TrackStateBadge("NEW")
        }
        if ref.flags?.contains(where: { $0.lowercased() == "stale" }) == true {
            TrackStateBadge("STALE", kind: .stale)
        }
    }
}

// A small standalone history surface, sharing the reader's persisted MRU.
// It is intentionally read-only here; opening is handed to the host.
public struct BrowseHistoryView: View {
    let onSelect: (String) -> Void
    @AppStorage("track.recentNotes") private var recentJSON = "[]"

    private struct Entry: Codable, Identifiable {
        let id: String
        let title: String
    }
    private var entries: [Entry] {
        (try? JSONDecoder().decode([Entry].self, from: Data(recentJSON.utf8))) ?? []
    }

    public init(onSelect: @escaping (String) -> Void = { _ in }) { self.onSelect = onSelect }

    public var body: some View {
        Group {
            if entries.isEmpty {
                Text("No recently opened notes").font(.caption).foregroundStyle(.secondary)
            } else {
                List(entries.prefix(20)) { entry in
                    Button { onSelect(entry.id) } label: {
                        Label(entry.title, systemImage: "clock")
                    }.buttonStyle(.plain)
                }
            }
        }
        .navigationTitle("History")
    }
}

// MARK: - Activity heatmap view

public struct ActivityHeatmapView: View {
    @Bindable var model: BrowseModel
    let onSelectDate: (String) -> Void
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.trackFontScale) private var fontScale

    public init(model: BrowseModel, onSelectDate: @escaping (String) -> Void = { _ in }) {
        self.model = model
        self.onSelectDate = onSelectDate
    }

    public var body: some View {
        Group {
            if model.isLoading && model.notes.isEmpty {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = model.error, model.notes.isEmpty {
                ContentUnavailableView("Could not load activity", systemImage: "exclamationmark.triangle", description: Text(error))
            } else {
                let counts = Self.dayCounts(notes: model.notes)
                let days = Self.lastDays(365)
                let weeks = Self.weeks(days)
                let palette = TrackTheme.palette(for: colorScheme)
                let maxCount = max(1, counts.values.max() ?? 1)
                VStack(alignment: .leading, spacing: 6) {
                    Text("Activity · last year").trackSectionLabel()
                    HStack(alignment: .top, spacing: 4) {
                        VStack(alignment: .trailing, spacing: 4) {
                            Text("").frame(height: 14)
                            ForEach(["M", "W", "F"], id: \.self) { Text($0).font(.system(size: 11 * fontScale, design: .monospaced)).foregroundStyle(palette.faint).frame(height: 12) }
                        }
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(alignment: .top, spacing: 4) {
                                ForEach(weeks.indices, id: \.self) { index in
                                    VStack(spacing: 4) {
                                        Text(Self.monthLabel(for: weeks[index].first ?? ""))
                                            .font(.system(size: 11 * fontScale, design: .monospaced)).foregroundStyle(palette.faint).frame(height: 14)
                                        ForEach(weeks[index].indices, id: \.self) { dayIndex in
                                            let day = weeks[index][dayIndex]
                                            Button { onSelectDate(day) } label: {
                                                RoundedRectangle(cornerRadius: 2).fill(palette.heatColor(count: counts[day] ?? 0, max: maxCount)).frame(width: 12, height: 12)
                                            }.buttonStyle(.plain).disabled(day.isEmpty).help(day.isEmpty ? "" : "\(day): \(counts[day] ?? 0) notes")
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task { await model.loadNotes() }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard !model.isLoading else { return }
            Task { await model.loadNotes() }
        }
    }

    /// Five-step chart-ramp color is drawn inline via the palette (web heatmap:
    /// chart-1 mixed over panel-soft); this helper stays for previews/tests.
    private func color(for count: Int) -> Color {
        TrackTheme.palette(for: colorScheme).heatColor(count: count)
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

    static func weeks(_ days: [String]) -> [[String]] {
        let cal = Calendar.current
        let formatter = DateFormatter(); formatter.dateFormat = "yyyy-MM-dd"; formatter.locale = Locale(identifier: "en_US_POSIX")
        guard let first = days.first, let date = formatter.date(from: first) else { return [] }
        let leading = (cal.component(.weekday, from: date) + 5) % 7
        var padded = Array(repeating: "", count: leading) + days
        while padded.count % 7 != 0 { padded.append("") }
        return stride(from: 0, to: padded.count, by: 7).map { Array(padded[$0..<$0 + 7]) }
    }

    static func monthLabel(for day: String) -> String {
        guard let date = DateFormatter.iso.date(from: day) else { return "" }
        return DateFormatter.month.string(from: date)
    }
}

private extension DateFormatter {
    static let iso: DateFormatter = { let f = DateFormatter(); f.dateFormat = "yyyy-MM-dd"; f.locale = Locale(identifier: "en_US_POSIX"); return f }()
    static let month: DateFormatter = { let f = DateFormatter(); f.dateFormat = "MMM"; return f }()
}
