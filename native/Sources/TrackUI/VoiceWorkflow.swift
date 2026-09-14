import Foundation
import Observation
import TrackAPI

/// Only the recognizer's provisional suffix is replaceable. User edits become
/// confirmed text and never get overwritten by a later recognition result.
public struct VoiceTranscriptState {
    public private(set) var confirmed = ""
    public private(set) var interim = ""
    private var absorbed = ""
    private var editedAbsorbed = ""
    private var absorbedBase = ""
    private var clearedSegment = false
    public init() {}
    public static func selectedText(in text: String, range: NSRange) -> String {
        let source = text as NSString
        guard range.location <= source.length, range.length <= source.length - range.location else { return "" }
        return source.substring(with: range).trimmingCharacters(in: .whitespacesAndNewlines)
    }
    private var remaining: String {
        if clearedSegment || interim.isEmpty { return "" }
        if absorbed.isEmpty { return interim }
        if interim.hasPrefix(absorbed) { return String(interim.dropFirst(absorbed.count)) }
        if !editedAbsorbed.isEmpty, interim.hasPrefix(editedAbsorbed) {
            return String(interim.dropFirst(editedAbsorbed.count))
        }
        // Recognition can revise words before the edit boundary. There is no
        // reliable split in that case: retain one replaceable, labelled candidate
        // for manual reconciliation instead of dropping words or guessing a merge.
        return "\n\n[認識の訂正候補・要確認]\n" + interim
    }
    private var suffix: String {
        let tail = remaining
        guard !tail.isEmpty else { return "" }
        let separator = absorbed.isEmpty && !confirmed.isEmpty && !confirmed.hasSuffix("\n") ? "\n" : ""
        return separator + tail
    }
    public var text: String { confirmed + suffix }

    public mutating func edit(_ value: String) {
        let shadow = suffix
        if !shadow.isEmpty, value.hasSuffix(shadow) {
            confirmed = String(value.dropLast(shadow.count))
        } else {
            let previous = confirmed
            confirmed = value
            if !interim.isEmpty {
                if absorbed.isEmpty { absorbedBase = previous }
                absorbed = interim
                editedAbsorbed = value.hasPrefix(absorbedBase)
                    ? String(value.dropFirst(absorbedBase.count)).trimmingCharacters(in: .newlines)
                    : ""
            }
        }
    }
    public mutating func receive(_ speech: String, isFinal: Bool) {
        interim = speech
        if isFinal { finishSegment() }
    }
    public mutating func finishSegment() {
        let visible = text
        confirmed = visible.isEmpty || visible.hasSuffix("\n") ? visible : visible + "\n"
        interim = ""
        absorbed = ""
        editedAbsorbed = ""
        absorbedBase = ""
        clearedSegment = false
    }
    public mutating func clear() {
        confirmed = ""
        // The current utterance has already been displayed and explicitly cleared.
        // Suppress it through its final callback; a fresh segment can append again.
        absorbed = interim
        editedAbsorbed = ""
        absorbedBase = ""
        clearedSegment = !interim.isEmpty
    }
}

/// Coalesce interim updates only within one recognition segment. Errors create
/// the same boundary as a final result, even while selection or IME defers display.
public struct VoiceSpeechBuffer {
    private var events: [(text: String, isFinal: Bool)] = []
    public init() {}
    public mutating func append(_ text: String, isFinal: Bool) {
        if events.last?.isFinal == false { events.removeLast() }
        events.append((text, isFinal))
    }
    public mutating func finishSegment(fallback: String) {
        if events.isEmpty { events.append((fallback, true)) }
        else if events.last?.isFinal == false { events[events.count - 1].isFinal = true }
    }
    public mutating func drain() -> [(text: String, isFinal: Bool)] {
        defer { events.removeAll() }
        return events
    }
}

/// Both manual save and stop use this snapshot writer. A pending PUT is retried
/// with the original etag; after a lost response, read-back confirms its effect.
@MainActor
@Observable
public final class VoiceJournalWriter {
    public private(set) var isSaving = false
    public private(set) var error: String?
    public private(set) var message: String?
    public private(set) var journalID: TrackID?
    public private(set) var savedTranscript = ""
    private let client: TrackClient
    private struct Append {
        var snapshot: String
        var id: TrackID
        var body: String
        var etag: String
    }
    private var pending: Append?
    public init(client: TrackClient) { self.client = client }

    public func reset() {
        guard !isSaving, pending == nil else { return }
        savedTranscript = ""
        message = nil
        error = nil
    }
    public func restoreCheckpoint(_ snapshot: String) {
        guard !isSaving, pending == nil else { return }
        savedTranscript = snapshot
    }
    public var hasPendingSave: Bool { pending != nil }

    public func save(_ snapshot: String, vault: String, date: String) async {
        guard !isSaving else { return }
        isSaving = true
        error = nil
        message = nil
        defer { isSaving = false }
        do {
            // Finish an earlier uncertain write before accepting a new snapshot.
            if let pending {
                try await finish(pending)
                guard self.pending == nil else { return }
            }
            // ponytail: append-only checkpoint; saved-prefix edits need an explicit correction workflow.
            guard snapshot.hasPrefix(savedTranscript) else {
                error = "Previously saved text was edited. Copy the transcript or restore its saved prefix before appending; the journal was not changed."
                return
            }
            let tail = String(snapshot.dropFirst(savedTranscript.count))
            guard !tail.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
            let journal = try await client.openJournal(date: date, vault: vault)
            journalID = journal.noteID
            let note = try await client.getNote(journal.noteID)
            let base = note.note.body.replacingOccurrences(of: "\n+$", with: "", options: .regularExpression)
            let joined = base.isEmpty ? tail : base + "\n\n" + tail
            let body = joined.hasSuffix("\n") ? joined : joined + "\n"
            let append = Append(snapshot: snapshot, id: journal.noteID, body: body, etag: note.note.etag)
            pending = append
            try await finish(append)
        } catch {
            self.error = (error as? APIError)?.message ?? error.localizedDescription
        }
    }

    private func finish(_ append: Append) async throws {
        // If a save reached the server before its response was lost, its exact
        // body (possibly followed by someone else's append) already proves success.
        let current = try await client.getNote(append.id)
        if current.note.body == append.body || current.note.body.hasPrefix(append.body) {
            confirm(append)
            return
        }
        guard current.note.etag == append.etag else {
            error = "The journal changed while confirming a save. Open it to check the pending text; the transcript is retained and has not been appended again."
            return
        }
        do {
            _ = try await client.saveNote(id: append.id, body: append.body, etag: append.etag)
            confirm(append)
        } catch {
            // A definitive 409 never applied this write. Keep the pending
            // snapshot for inspection rather than silently appending it twice.
            throw error
        }
    }
    private func confirm(_ append: Append) {
        savedTranscript = append.snapshot
        journalID = append.id
        pending = nil
        message = "Saved to journal"
        NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
    }
}
