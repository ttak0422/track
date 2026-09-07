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
// The reading-time estimate itself (web CHARS_PER_SECOND accumulation) is not
// part of the native P0; shouldMarkRead answers with the fixed floor only.

@MainActor
@Observable
public final class ReadingStore {
    /// Raw note ids opened here or reported seen by the server.
    public private(set) var seen: Set<String> = []
    /// Raw note ids that crossed the read threshold.
    public private(set) var read: Set<String> = []

    /// Nothing counts as read in under this, however short the note — the web
    /// read floor (reading.ts MIN_READ_SEC; the native P0 fixes the default
    /// at 30 s).
    public static let readThreshold: TimeInterval = 30

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
        guard (ref.seenAt ?? 0) <= 0, (ref.readAt ?? 0) <= 0 else { return false }
        let id = ref.noteID.raw
        return !seen.contains(id) && !read.contains(id)
    }

    /// Whether this device (or an adopted server milestone) read the note.
    public func isRead(_ id: String) -> Bool {
        read.contains(id)
    }

    /// Whether the note crossed the read threshold, which fires "read"
    /// (web recordView). The floor is the fixed default; elapsed seconds below
    /// it never count as read.
    public func shouldMarkRead(after seconds: TimeInterval) -> Bool {
        seconds >= Self.readThreshold
    }

    // MARK: - Persistence

    private struct Snapshot: Codable {
        var seen: [String]
        var read: [String]
    }

    private func load() {
        guard let data = defaults.data(forKey: Self.storageKey),
              let snapshot = try? JSONDecoder().decode(Snapshot.self, from: data)
        else { return }
        seen = Set(snapshot.seen)
        read = Set(snapshot.read)
    }

    private func persist() {
        let snapshot = Snapshot(seen: seen.sorted(), read: read.sorted())
        defaults.set(try? JSONEncoder().encode(snapshot), forKey: Self.storageKey)
    }
}
