import Foundation
import Observation
import TrackAPI

public struct NoteTab: Codable, Equatable, Identifiable, Sendable {
    public let id: TrackID
    public var title: String
}

/// Open notes are most-recent-first, like the web strip. Drafts stay in the reader.
@MainActor
@Observable
public final class NoteTabs {
    public private(set) var entries: [NoteTab]
    public private(set) var activeID: TrackID?
    private let defaults: UserDefaults
    private static let storageKey = "track.native.tabs"
    private struct Stored: Codable { var entries: [NoteTab]; var activeID: TrackID? }

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        let saved = defaults.data(forKey: Self.storageKey).flatMap { try? JSONDecoder().decode(Stored.self, from: $0) }
        var seen = Set<TrackID>()
        entries = (saved?.entries ?? []).filter { !$0.id.raw.isEmpty && seen.insert($0.id).inserted }
        activeID = entries.first { $0.id == saved?.activeID }?.id ?? entries.first?.id
    }

    public func opened(_ id: TrackID, title: String) {
        entries.removeAll { $0.id == id }
        entries.insert(NoteTab(id: id, title: title), at: 0)
        activeID = id
        persist()
    }

    @discardableResult
    public func remove(_ id: TrackID) -> TrackID? {
        entries.removeAll { $0.id == id }
        if activeID == id { activeID = entries.first?.id }
        persist()
        return activeID
    }

    /// A missing note is removed; an offline/unavailable vault keeps its tabs.
    /// Mutate existing entries only, so a close during this fetch stays closed.
    public func restore(using client: TrackClient) async {
        for tab in entries {
            guard !Task.isCancelled else { return }
            do {
                let response = try await client.getNote(tab.id)
                guard let index = entries.firstIndex(where: { $0.id == tab.id }) else { continue }
                if entries[index].title == tab.title { entries[index].title = response.note.summary.ref.title }
            } catch let error as APIError where error.status == 404 {
                remove(tab.id)
            } catch {
                // Temporary read failures do not erase the saved workspace.
            }
        }
        persist()
    }

    private func persist() {
        if let data = try? JSONEncoder().encode(Stored(entries: entries, activeID: activeID)) {
            defaults.set(data, forKey: Self.storageKey)
        }
    }
}

public struct SearchSection: Identifiable {
    public let title: String
    public let results: [SearchResult]
    public var id: String { title }
}

public enum SearchPresentation {
    /// The engine's discriminator, also used by web SearchPanel. Preserve rank
    /// within each group, and share this exact order with keyboard navigation.
    public static func sections(_ results: [SearchResult]) -> [SearchSection] {
        [
            SearchSection(title: "Titles", results: results.filter { $0.match != "body" && $0.match != "path" }),
            SearchSection(title: "Full text", results: results.filter { $0.match == "body" }),
            SearchSection(title: "File name", results: results.filter { $0.match == "path" }),
        ].filter { !$0.results.isEmpty }
    }

    public static func step(_ current: Int, by delta: Int, count: Int) -> Int {
        guard count > 0 else { return -1 }
        if current < 0 { return delta > 0 ? 0 : count - 1 }
        return (current + delta + count) % count
    }

    /// Match Go's per-rune lowercase, including İ and Σ, while returning ranges
    /// in the original display text (web searchHighlight.ts).
    public static func highlightRanges(in text: String, query: String) -> [NSRange] {
        let terms = Set(query.split(whereSeparator: \.isWhitespace)
            .filter { $0 != "AND" && $0 != "OR" }.map { fold(String($0)) })
            .sorted { $0.utf16.count > $1.utf16.count }
        guard !terms.isEmpty else { return [] }
        var folded = ""
        var starts: [Int] = []
        var ends: [Int] = []
        var offset = 0
        for scalar in text.unicodeScalars {
            let original = String(scalar)
            let mapped = fold(original)
            folded += mapped
            starts += Array(repeating: offset, count: mapped.utf16.count)
            offset += original.utf16.count
            ends += Array(repeating: offset, count: mapped.utf16.count)
        }
        let pattern = terms.map(NSRegularExpression.escapedPattern(for:)).joined(separator: "|")
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return [] }
        return regex.matches(in: folded, range: NSRange(folded.startIndex..., in: folded)).map {
            let start = starts[$0.range.location]
            let end = ends[NSMaxRange($0.range) - 1]
            return NSRange(location: start, length: end - start)
        }
    }

    private static func fold(_ text: String) -> String {
        text.replacingOccurrences(of: "İ", with: "i").replacingOccurrences(of: "Σ", with: "σ").lowercased()
    }
}
