import Foundation

// Thin port of web/src/api.ts (live-server branch only; the static-site
// branch does not apply to the native app). One method per MVP endpoint.

public struct APIError: Error, Sendable {
    public let status: Int
    public let message: String
}

/// Write token for the task write path: the etag a read returned, echoed
/// back so the server can refuse (409) a write against a stale view.
public enum DateField: String, Sendable {
    // types.ts DateField: the JSON keys POST /api/task takes — "sched"/"due",
    // NOT the "scheduled"/"due" keys task reads come back under.
    case scheduled = "sched"
    case due = "due"
}

/// The two reading milestones `markRead` can report (reading.ts: postReadEvent):
/// "seen" when a device opened the note, "read" once viewing time crossed the
/// read threshold. Both are monotonic firsts server-side.
public enum ReadEvent: String, Sendable {
    case seen
    case read
}

public struct TrackClient: Sendable {
    public let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
    }

    // MARK: - Reads (api.ts: searchNotes/listDatedTasks/listOpenTasks/resolveTerm/getNote)

    public func searchNotes(query: String, limit: Int = 100) async throws -> SearchResponse {
        try await get(path: "/api/search", query: [
            URLQueryItem(name: "q", value: query),
            URLQueryItem(name: "limit", value: String(limit)),
        ])
    }

    public func listDatedTasks() async throws -> TaskListResponse {
        try await get(path: "/api/tasks")
    }

    public func listOpenTasks() async throws -> TaskListResponse {
        try await get(path: "/api/tasks", query: [URLQueryItem(name: "open", value: "1")])
    }

    public func resolveTerm(_ term: String, vault: String = "") async throws -> ResolveResponse {
        var items = [URLQueryItem(name: "term", value: term)]
        if !vault.isEmpty { items.append(URLQueryItem(name: "vault", value: vault)) }
        return try await get(path: "/api/resolve", query: items)
    }

    public func getNote(_ id: TrackID) async throws -> NoteResponse {
        try await get(path: "/api/note", query: idQuery(id))
    }

    /// `/api/notes`: the vault's notes, recently-updated first, with activity
    /// days riding along so the calendar can derive per-day lists (api.ts:
    /// listNotes). `limit` is optional — the server's default listing needs none.
    public func listNotes(limit: Int? = nil) async throws -> NotesResponse {
        var items: [URLQueryItem] = []
        if let limit { items.append(URLQueryItem(name: "limit", value: String(limit))) }
        return try await get(path: "/api/notes", query: items)
    }

    /// `GET /api/note/meta`: the note's editable sidecar metadata as one typed
    /// document (api.ts: getNoteMeta).
    public func getNoteMeta(_ id: TrackID) async throws -> NoteMetaResponse {
        try await get(path: "/api/note/meta", query: idQuery(id))
    }

    // MARK: - Calendar & link reads (api.ts: getActivity/getAgenda/getOgp)

    /// `/api/activity`: per-day note activity in the inclusive [since, until]
    /// window (YYYY-MM-DD). Both ends default server-side, so nil sends no query
    /// item (api.ts: getActivity).
    public func getActivity(since: String? = nil, until: String? = nil) async throws -> ActivityResponse {
        var items: [URLQueryItem] = []
        if let since { items.append(URLQueryItem(name: "since", value: since)) }
        if let until { items.append(URLQueryItem(name: "until", value: until)) }
        return try await get(path: "/api/activity", query: items)
    }

    /// `/api/agenda?date=`: the notes active on one calendar day (api.ts: getAgenda).
    public func getAgenda(date: String) async throws -> AgendaResponse {
        try await get(path: "/api/agenda", query: [URLQueryItem(name: "date", value: date)])
    }

    /// `/api/ogp?url=`: Open Graph metadata for a link's rich card. Fields the
    /// server could not find are absent, so the caller renders a plain link
    /// (api.ts: getOgp).
    public func getOgp(url: String) async throws -> OgpResponse {
        try await get(path: "/api/ogp", query: [URLQueryItem(name: "url", value: url)])
    }

    /// `POST /api/note/read`: records a shared reading milestone on the note's
    /// sidecar (api.ts via reading.ts: postReadEvent). Unlike the web's
    /// fire-and-forget variant, a failure throws so callers can decide.
    public func markRead(id: TrackID, event: ReadEvent) async throws -> ReadResponse {
        try await post(path: "/api/note/read", query: idQuery(id), body: ["event": event.rawValue])
    }

    /// `/api/hierarchy`: the vault's whole "up" tree, roots first, for the
    /// hierarchy menu (api.ts: getHierarchy). Asked for only when first opened.
    public func getHierarchy() async throws -> HierarchyResponse {
        try await get(path: "/api/hierarchy")
    }

    /// `/api/graph/local`: the 1-hop neighbourhood of a note, the center marked
    /// (api.ts: getLocalGraph).
    public func getLocalGraph(_ id: TrackID) async throws -> GraphResponse {
        try await get(path: "/api/graph/local", query: idQuery(id))
    }

    /// `/api/graph`: the vault's full link graph (api.ts: getGraph).
    public func getGraph() async throws -> GraphResponse {
        try await get(path: "/api/graph")
    }

    // MARK: - Writes (api.ts: setTaskState/setTaskDate)

    /// A task write returns the note's refreshed tasks + etag — redraw from
    /// the response, no second request (api.ts:372).
    public func setTaskState(id: TrackID, line: Int, state: String, expect: String, etag: String) async throws -> TasksResponse {
        try await post(path: "/api/task", query: idQuery(id), body: [
            "line": line, "state": state, "expect": expect, "etag": etag,
        ] as [String: Any])
    }

    /// `field` serializes to "sched"/"due"; empty date clears it (api.ts:344).
    public func setTaskDate(id: TrackID, line: Int, field: DateField, date: String, expect: String, etag: String) async throws -> TasksResponse {
        try await post(path: "/api/task", query: idQuery(id), body: [
            "line": line, field.rawValue: date, "expect": expect, "etag": etag,
        ] as [String: Any])
    }

    // MARK: - Note edits & creation (api.ts: saveNote/createNote/deleteNote/saveNoteMeta/openJournal)

    /// `PUT /api/note`: saves the body of an existing note, echoing back the
    /// etag a read returned so the server can refuse (409) a save against a
    /// stale view (api.ts: saveNote).
    public func saveNote(id: TrackID, body: String, etag: String) async throws -> SaveNoteResponse {
        try await put(path: "/api/note", query: idQuery(id), body: SaveNoteRequest(body: body, etag: etag))
    }

    /// `POST /api/note`: mints a note titled `title` with the default template.
    /// A title that already resolves is refused with 409, so callers can tell
    /// "already there" apart from a real failure (api.ts: createNote).
    public func createNote(title: String) async throws -> CreateNoteResponse {
        try await postEncodable(path: "/api/note", body: CreateNoteRequest(title: title))
    }

    /// `DELETE /api/note`: permanently removes the note — its file, its sidecar
    /// metadata, and its index row (api.ts: deleteNote).
    public func deleteNote(id: TrackID) async throws -> DeleteNoteResponse {
        try await delete(path: "/api/note", query: idQuery(id))
    }

    /// `POST /api/note/meta`: replaces a note's whole editable sidecar metadata
    /// (api.ts: saveNoteMeta). The response is the refreshed typed fields, the
    /// same shape `GET /api/note/meta` returns.
    public func saveNoteMeta(id: TrackID, request: SaveNoteMetaRequest) async throws -> NoteMetaResponse {
        try await postEncodable(path: "/api/note/meta", query: idQuery(id), body: request)
    }

    /// `POST /api/journal`: opens or creates the journal for a day and returns
    /// its note id (api.ts: openJournal). The endpoint takes no request body,
    /// but every post here is a JSON API call, so an empty object is sent.
    public func openJournal(date: String) async throws -> JournalResponse {
        try await post(path: "/api/journal", query: [URLQueryItem(name: "date", value: date)], body: [:])
    }

    // MARK: - Transport

    private func idQuery(_ id: TrackID) -> [URLQueryItem] {
        let (bare, vault) = id.split()
        var items = [URLQueryItem(name: "id", value: bare)]
        if !vault.isEmpty { items.append(URLQueryItem(name: "vault", value: vault)) }
        return items
    }

    private func get<T: Decodable>(path: String, query: [URLQueryItem] = []) async throws -> T {
        var comps = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        comps.queryItems = query.isEmpty ? nil : query
        var req = URLRequest(url: comps.url!)
        req.httpMethod = "GET"
        return try await send(req)
    }

    private func post<T: Decodable>(path: String, query: [URLQueryItem] = [], body: [String: Any]) async throws -> T {
        var comps = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        comps.queryItems = query.isEmpty ? nil : query
        var req = URLRequest(url: comps.url!)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        return try await send(req)
    }

    /// PUT with a JSON body, for Codable request types (api.ts: saveNote).
    private func put<T: Decodable>(path: String, query: [URLQueryItem] = [], body: some Encodable) async throws -> T {
        var comps = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        comps.queryItems = query.isEmpty ? nil : query
        var req = URLRequest(url: comps.url!)
        req.httpMethod = "PUT"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(body)
        return try await send(req)
    }

    /// DELETE with no body (api.ts: deleteNote).
    private func delete<T: Decodable>(path: String, query: [URLQueryItem] = []) async throws -> T {
        var comps = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        comps.queryItems = query.isEmpty ? nil : query
        var req = URLRequest(url: comps.url!)
        req.httpMethod = "DELETE"
        return try await send(req)
    }

    /// POST with a JSON body, for Codable request types — the typed pair of the
    /// dictionary-bodied `post` above (api.ts: saveNoteMeta / createNote).
    private func postEncodable<T: Decodable>(path: String, query: [URLQueryItem] = [], body: some Encodable) async throws -> T {
        var comps = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        comps.queryItems = query.isEmpty ? nil : query
        var req = URLRequest(url: comps.url!)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(body)
        return try await send(req)
    }

    private func send<T: Decodable>(_ req: URLRequest) async throws -> T {
        let (data, resp) = try await session.data(for: req)
        guard let http = resp as? HTTPURLResponse else {
            throw APIError(status: -1, message: "non-HTTP response")
        }
        guard (200..<300).contains(http.statusCode) else {
            let msg = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["error"] as? String
                ?? "\(http.statusCode) \(HTTPURLResponse.localizedString(forStatusCode: http.statusCode))"
            throw APIError(status: http.statusCode, message: msg)
        }
        let decoder = JSONDecoder()
        return try decoder.decode(T.self, from: data)
    }
}
