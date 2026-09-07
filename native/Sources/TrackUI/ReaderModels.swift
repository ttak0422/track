import Foundation
import Observation
import TrackAPI

// View models for the MVP reader (note + backlinks) and search.
// Rendering stays native: `note.body` is plain GFM from the engine and
// SwiftUI parses it locally — `/api/render` is never used.

// MARK: - Note reader

@MainActor
@Observable
public final class NoteReaderModel {
    public enum State: Sendable {
        case empty
        case loading
        case loaded(NoteResponse)
        case failed(String)
    }

    public private(set) var state: State = .empty
    /// The id `open` last succeeded with — the qualified id every write and
    /// the read report address. Cleared while loading and once nothing is open.
    public private(set) var currentID: TrackID?

    // Editing buffer for the note body (web NoteEditor parity). `draftBody`
    // mirrors the loaded body while the note is not being edited and diverges
    // while it is; a save gates on `isDirty` and echoes the loaded response's
    // etag (NoteDetail.etag) so the server can refuse (409) a stale overwrite.
    public var draftBody: String = ""
    public private(set) var isEditing = false

    // Write-path state (web useSaveNoteMutation / useDeleteNoteMutation /
    // createNote / useSaveNoteMetaMutation + the API's 409 conflict reply).
    // Each busy flag drives a disable + ProgressView-equivalent in the view.
    public private(set) var isSaving = false
    public private(set) var isDeleting = false
    public private(set) var isCreating = false
    public private(set) var isSavingMeta = false
    /// The most recent write-path failure (save/delete/create/meta save/meta
    /// load), shown until dismissed or until the next write clears it.
    public private(set) var saveError: String?
    /// Set when a body save is refused with 409: the note changed on disk, so
    /// the view reloaded the latest etag and the edit was NOT applied.
    public private(set) var saveConflict: String?

    private let client: TrackClient

    public init(client: TrackClient) {
        self.client = client
    }

    /// The body the open note has on disk (`""` with nothing loaded) — the
    /// reference `isDirty` is measured against (web `loadedRef.body`).
    public var loadedBody: String {
        if case .loaded(let response) = state { return response.note.body }
        return ""
    }

    /// `draftBody` has diverged from the loaded body (web `dirty`).
    public var isDirty: Bool { draftBody != loadedBody }

    /// A note is open, so Edit/Done and the note actions are available.
    public var isLoaded: Bool {
        if case .loaded = state { return true }
        return false
    }

    public func open(_ id: TrackID) async {
        state = .loading
        currentID = nil
        // Opening always leaves editing behind: a draft belongs to the note
        // that was open, and switching notes discards it (web adopt path).
        isEditing = false
        draftBody = ""
        saveError = nil
        saveConflict = nil
        do {
            let response = try await client.getNote(id)
            currentID = id
            draftBody = response.note.body
            state = .loaded(response)
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    // MARK: - Editing

    /// Enter the editor seeded from the loaded body.
    public func beginEditing() {
        guard isLoaded else { return }
        draftBody = loadedBody
        isEditing = true
    }

    /// Drop the draft and return to reading the loaded body. Any failure or
    /// conflict banner belongs to the abandoned edit, so it goes too.
    public func discardDraft() {
        draftBody = loadedBody
        isEditing = false
        saveError = nil
        saveConflict = nil
    }

    // MARK: - Writes (web NoteEditor / NoteMetaDialog / NoteActionsMenu parity)

    /// Dismiss the write-failure banner.
    public func dismissSaveError() {
        saveError = nil
    }

    /// Dismiss the "note changed on disk" conflict notice.
    public func dismissConflict() {
        saveConflict = nil
    }

    /// Save the draft against the loaded etag (web submit → saveNote). On
    /// success the note is refetched and the draft reseeded from the fresh
    /// body. A 409 leaves the draft intact, reloads the latest etag so a retry
    /// has a fresh token, and sets `saveConflict` (the edit was not applied).
    public func saveDraft() async {
        guard isDirty, !isSaving, let id = currentID else { return }
        isSaving = true
        saveError = nil
        saveConflict = nil
        defer { isSaving = false }
        do {
            _ = try await client.saveNote(id: id, body: draftBody, etag: loadedEtag)
            let fresh = try await client.getNote(id)
            draftBody = fresh.note.body
            state = .loaded(fresh)
        } catch let api as APIError where api.status == 409 {
            saveConflict = "This note changed since it was loaded. Reloading the latest version; your edit was not saved."
            if let fresh = try? await client.getNote(id) {
                state = .loaded(fresh)
            }
        } catch {
            saveError = Self.message(for: error)
        }
    }

    /// Delete the open note (web deleteNote). The view keeps the destructive
    /// button disabled until the retyped title matches; the model re-checks
    /// `confirmedTitle` so a stale call can never delete. Returns true once
    /// the note is gone (`state == .empty`).
    @discardableResult
    public func deleteCurrent(confirmedTitle: String) async -> Bool {
        guard case .loaded(let response) = state, !isDeleting, let id = currentID else { return false }
        guard confirmedTitle.trimmingCharacters(in: .whitespaces)
            == response.note.summary.ref.title.trimmingCharacters(in: .whitespaces) else {
            saveError = "Type the note's title exactly to confirm deletion."
            return false
        }
        isDeleting = true
        saveError = nil
        saveConflict = nil
        defer { isDeleting = false }
        do {
            _ = try await client.deleteNote(id: id)
            currentID = nil
            isEditing = false
            draftBody = ""
            state = .empty
            return true
        } catch {
            saveError = Self.message(for: error)
            return false
        }
    }

    /// Mint a note titled `title` with the default template (web createNote)
    /// and open it. Returns true on success (the new note is open and any
    /// staging sheet can clear + dismiss). A title that already resolves is
    /// refused with 409 — that and any other failure set `saveError` and
    /// return false so the caller keeps its creation dialog open.
    @discardableResult
    public func createNote(title: String) async -> Bool {
        let trimmed = title.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else {
            saveError = "Enter a title to create the note."
            return false
        }
        guard !isCreating else { return false }
        isCreating = true
        saveError = nil
        defer { isCreating = false }
        do {
            let created = try await client.createNote(title: trimmed)
            await open(created.noteID)
            return true
        } catch let api as APIError where api.status == 409 {
            // VoiceView parity: a duplicate is a notice, not an error.
            saveError = "A note with the same title already exists"
            return false
        } catch {
            saveError = Self.message(for: error)
            return false
        }
    }

    /// Replace the open note's whole editable sidecar metadata (web
    /// saveNoteMeta). On success the note is refetched so the header's
    /// title/tags/flags/props track the saved metadata. Returns true on
    /// success; a rejected edit sets `saveError` and returns false so the
    /// meta sheet stays open (web dialog keeps open, changing nothing).
    @discardableResult
    public func saveMeta(_ request: SaveNoteMetaRequest) async -> Bool {
        guard !isSavingMeta, let id = currentID else { return false }
        isSavingMeta = true
        saveError = nil
        defer { isSavingMeta = false }
        do {
            _ = try await client.saveNoteMeta(id: id, request: request)
            await refreshOpenNote()
            return true
        } catch {
            saveError = Self.message(for: error)
            return false
        }
    }

    /// Fetch the open note's editable metadata so the meta sheet can seed its
    /// form (web useNoteMetaQuery). Returns nil — with `saveError` set — when
    /// the fetch fails or nothing is open.
    public func fetchMeta() async -> NoteMetaResponse? {
        guard let id = currentID else { return nil }
        saveError = nil
        do {
            return try await client.getNoteMeta(id)
        } catch {
            saveError = Self.message(for: error)
            return nil
        }
    }

    /// Report the shared "seen" reading milestone for the open note
    /// (reading.ts markSeen). Fire-and-forget from the reader's `.task`; a
    /// failure must not disturb the note (try? swallows it).
    public func reportSeen() async {
        guard let id = currentID else { return }
        _ = try? await client.markRead(id: id, event: .seen)
    }

    /// The loaded response's etag — the write token a save echoes back.
    private var loadedEtag: String {
        if case .loaded(let response) = state { return response.note.etag }
        return ""
    }

    /// Re-fetch the open note after a write, adopting the fresh body into the
    /// draft only when the buffer is clean — a background refresh must not
    /// clobber unsaved edits (web adopt guard body === loadedRef.body). Best
    /// effort: the write itself succeeded, so a failed refresh keeps the
    /// last-known state rather than failing the note.
    private func refreshOpenNote() async {
        guard let id = currentID else { return }
        do {
            let fresh = try await client.getNote(id)
            if !isDirty { draftBody = fresh.note.body }
            state = .loaded(fresh)
        } catch {
            // Swallowed — see above.
        }
    }

    private static func message(for error: any Error) -> String {
        (error as? APIError)?.message ?? error.localizedDescription
    }

    /// Backlink taps resolve through the same id space the lists use.
    public func openRef(_ ref: NoteRef) async {
        await open(TrackID.qualify(vault: "", id: ref.noteID.raw))
    }

    /// Resolve and open a `[[wikilink]]` target. The target grammar
    /// (`web/src/components/markdown/plugins.ts: splitWikiTarget`) allows an
    /// optional leading `vault:` and a trailing `#anchor`/`#^block`; the
    /// anchor is stripped for resolution (it names a destination inside the
    /// note, not a different note). Cross-vault targets resolve through the
    /// same `/api/resolve` the web reader uses.
    public func openWikilink(target: String) async {
        let (vault, term) = Self.splitWikilink(target)
        do {
            let resolved = try await client.resolveTerm(term, vault: vault)
            guard resolved.found else { return }
            await open(TrackID.qualify(vault: vault, id: resolved.note.noteID.raw))
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    /// `vault:title#anchor` → (`vault`, `title`); a bare `title` keeps the
    /// empty vault. The anchor (block or heading) is dropped for lookup.
    private static func splitWikilink(_ target: String) -> (vault: String, term: String) {
        let trimmed = target.trimmingCharacters(in: .whitespaces)
        let noAnchor = trimmed.split(separator: "#", maxSplits: 1).first.map(String.init) ?? trimmed
        if let colon = noAnchor.firstIndex(of: ":") {
            let vault = String(noAnchor[..<colon])
            let term = String(noAnchor[noAnchor.index(after: colon)...])
            return (vault, term)
        }
        return ("", noAnchor)
    }
}

// MARK: - Search

@MainActor
@Observable
public final class SearchModel {
    public private(set) var results: [SearchResult] = []
    public private(set) var isLoading = false
    public private(set) var error: String?
    /// Vaults the server could not search, mirroring `SearchPanel`'s
    /// "vault … could not be searched" note (the count drives a banner).
    public private(set) var unavailableCount = 0
    private let client: TrackClient

    public init(client: TrackClient) {
        self.client = client
    }

    public func search(query: String) async {
        error = nil
        guard !query.isEmpty else { results = []; unavailableCount = 0; isLoading = false; return }
        isLoading = true
        defer { isLoading = false }
        do {
            // api.ts: searchNotes(query, limit) — title hits first, server-side.
            let response = try await client.searchNotes(query: query)
            results = response.results
            unavailableCount = response.unavailable?.count ?? 0
        } catch {
            self.error = error.localizedDescription
            results = []
            unavailableCount = 0
        }
    }
}
