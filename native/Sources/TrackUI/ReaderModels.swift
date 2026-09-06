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
    private let client: TrackClient

    public init(client: TrackClient) {
        self.client = client
    }

    public func open(_ id: TrackID) async {
        state = .loading
        do {
            state = .loaded(try await client.getNote(id))
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    /// Backlink taps resolve through the same id space the lists use.
    public func openRef(_ ref: NoteRef) async {
        await open(TrackID.qualify(vault: "", id: ref.noteID.raw))
    }
}

// MARK: - Search

@MainActor
@Observable
public final class SearchModel {
    public private(set) var results: [SearchResult] = []
    public private(set) var error: String?
    private let client: TrackClient

    public init(client: TrackClient) {
        self.client = client
    }

    public func search(query: String) async {
        error = nil
        guard !query.isEmpty else { results = []; return }
        do {
            // api.ts: searchNotes(query, limit) — title hits first, server-side.
            results = try await client.searchNotes(query: query).results
        } catch {
            self.error = error.localizedDescription
        }
    }
}
