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
