import Foundation
import Observation
import TrackAPI

// ReadingStore — the local half of the read-state cache (web/src/reading.ts +
// ADR 0072). The shared milestones live on each note's sidecar and come back
// on every listing as seen_at/read_at; this store keeps the local mirror so
// NEW/read badges draw without waiting on the server. It is deliberately
// monotonic — marks only ever add an id, matching reading.ts's
// "a note cannot flip back to NEW" — and persists to UserDefaults under a
// single "track.reading" key like the web's one localStorage cache.
//
// NEW means "no device has opened the note yet": the ref reports no milestone
// (seen_at/read_at are nil or 0) and no seen mark is held locally.
//
// Viewing time is kept locally, just like web/src/reading.ts. It is only an
// estimate used to decide when to emit the read milestone; the shared
// seen/read milestones remain the source of truth across devices.

@MainActor
@Observable
public final class ReadingStore {
    /// Raw note ids opened here or reported seen by the server.
    public private(set) var seen: Set<String> = []
    /// Raw note ids that crossed the read threshold.
    public private(set) var read: Set<String> = []

    /// Accumulated viewing seconds for notes on this Mac.
    public private(set) var viewedSeconds: [String: TimeInterval] = [:]

    /// Compatibility floor for callers that do not have note text yet.
    public static let readThreshold: TimeInterval = 30

    /// Nothing counts as read in under this, however short the note.
    public static let minimumReadThreshold: TimeInterval = 20

    /// Rough Japanese reading pace, matching web/src/reading.ts.
    public static let charsPerSecond: Double = 10

    /// Single storage key, like web reading.ts's "track.reading".
    public static let storageKey = "track.reading"

    private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        load()
    }

    // MARK: - Marking

    /// Records that the note was opened at all, so NEW goes away even before
    /// the read threshold is reached (web markSeen). Returns true when the
    /// mark was new — the caller reports the milestone to the server once.
    @discardableResult
    public func markSeen(_ id: String) -> Bool {
        guard !id.isEmpty else { return false }
        guard seen.insert(id).inserted else { return false }
        persist()
        return true
    }

    /// Records that viewing crossed the read threshold. Reading implies
    /// having been opened (ADR 0072 read_at implies seen_at), so the id joins
    /// `seen` too. Returns true when the read mark was new.
    @discardableResult
    public func markRead(_ id: String) -> Bool {
        guard !id.isEmpty else { return false }
        guard read.insert(id).inserted else { return false }
        seen.insert(id)
        persist()
        return true
    }

    // MARK: - Querying

    /// NEW = no device has opened the note yet: the server reports no
    /// milestone on the ref and no seen mark is held locally. read implies
    /// seen, so a local read mark also clears NEW.
    public func isNew(_ ref: NoteRef) -> Bool {
        isNew(noteID: ref.noteID, seenAt: ref.seenAt, readAt: ref.readAt)
    }

    /// Shared NEW predicate for every list presentation (search, tags,
    /// backlinks, and recents). Keeping the server milestones and local
    /// cache check in one overload prevents those views from drifting apart.
    public func isNew(noteID: TrackID, seenAt: Int? = nil, readAt: Int? = nil) -> Bool {
        guard (seenAt ?? 0) <= 0, (readAt ?? 0) <= 0 else { return false }
        let id = noteID.raw
        return !seen.contains(id) && !read.contains(id)
    }

    /// Whether this device (or an adopted server milestone) read the note.
    public func isRead(_ id: String) -> Bool {
        read.contains(id)
    }

    /// The estimated viewing seconds that makes a note read. This mirrors the
    /// web's readThresholdFor: half of the estimated reading time, with a
    /// minimum floor. `String.count` is intentional; it counts the same
    /// user-visible characters as the web's `text.length` for CJK notes.
    public static func readThreshold(for text: String) -> TimeInterval {
        // `.toNearestOrAwayFromZero` matches JavaScript Math.round for these
        // positive values (not Swift's default ties-to-even rounding).
        let estimate = max(minimumReadThreshold, (Double(text.count) / charsPerSecond).rounded(.toNearestOrAwayFromZero))
        return max(minimumReadThreshold, (estimate / 2).rounded(.toNearestOrAwayFromZero))
    }

    /// Whether elapsed viewing time has crossed the fixed fallback threshold.
    /// New code should pass note text so long notes use the character estimate.
    public func shouldMarkRead(after seconds: TimeInterval) -> Bool {
        seconds >= Self.readThreshold
    }

    /// Whether elapsed viewing time has crossed the text-based threshold.
    public func shouldMarkRead(after seconds: TimeInterval, text: String) -> Bool {
        seconds >= Self.readThreshold(for: text)
    }

    /// Accumulates a coarse visible-time tick and marks the note read when its
    /// text-based threshold is crossed. Returns true only for that first
    /// crossing, matching web recordView's milestone behaviour.
    @discardableResult
    public func recordView(_ id: String, seconds: TimeInterval, text: String) -> Bool {
        guard !id.isEmpty, seconds > 0, !read.contains(id) else { return false }
        viewedSeconds[id, default: 0] += seconds
        let crossed = shouldMarkRead(after: viewedSeconds[id] ?? 0, text: text)
        persist()
        if crossed { return markRead(id) }
        return false
    }

    // MARK: - Persistence

    private struct Snapshot: Codable {
        var seen: [String]
        var read: [String]
        var viewedSeconds: [String: TimeInterval]

        init(seen: [String], read: [String], viewedSeconds: [String: TimeInterval]) {
            self.seen = seen
            self.read = read
            self.viewedSeconds = viewedSeconds
        }

        init(from decoder: Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            seen = try container.decode([String].self, forKey: .seen)
            read = try container.decode([String].self, forKey: .read)
            viewedSeconds = try container.decodeIfPresent([String: TimeInterval].self, forKey: .viewedSeconds) ?? [:]
        }
    }

    private func load() {
        guard let data = defaults.data(forKey: Self.storageKey),
              let snapshot = try? JSONDecoder().decode(Snapshot.self, from: data)
        else { return }
        seen = Set(snapshot.seen)
        read = Set(snapshot.read)
        viewedSeconds = snapshot.viewedSeconds
    }

    private func persist() {
        let snapshot = Snapshot(seen: seen.sorted(), read: read.sorted(), viewedSeconds: viewedSeconds)
        defaults.set(try? JSONEncoder().encode(snapshot), forKey: Self.storageKey)
    }
}
