import Foundation
import TrackAPI
import TrackUI

// CLT-only check: swift run --package-path native VerifyVaultScope.
// Two vaults intentionally reuse note IDs; every read's identity must survive
// the next write, while workspace queries must stay federated.
final class VaultProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}
    override func startLoading() {
        let url = request.url!
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)!.queryItems ?? []
        let scope = query.first { $0.name == "vault" }?.value ?? ""
        let id = query.first { $0.name == "id" }?.value
        let ref: [String: Any] = ["note_id": 42, "file_kind": "note", "title": "Same title"]
        let payload: [String: Any]
        switch url.path {
        case "/api/vaults":
            payload = ["active": ["name": "main", "path": "/main"],
                       "vaults": [["name": "main", "path": "/main", "active": true],
                                  ["name": "other", "path": "/other", "active": false]],
                       "unavailable": [["name": "offline", "path": "/offline", "error": "not mounted"]]]
        case "/api/search":
            precondition(scope.isEmpty, "workspace search must stay federated")
            payload = ["results": [ref.merging(["vault": "main"]) { _, rhs in rhs },
                                   ref.merging(["vault": "other"]) { _, rhs in rhs }]]
        case "/api/tasks":
            precondition(scope.isEmpty, "task list must keep the launch scope")
            payload = ["tasks": []]
        case "/api/hierarchy":
            precondition(scope.isEmpty, "hierarchy must keep the launch scope")
            payload = ["hierarchy": []]
        case "/api/notes":
            let created = query.contains { $0.name == "sort" && $0.value == "created" }
            precondition(created ? scope == "other" : scope.isEmpty)
            payload = ["vault": scope, "notes": [ref]]
        case "/api/activity":
            precondition(scope == "other")
            payload = ["activity": ["since": "2026-09-01", "until": "2026-09-14", "total": 1,
                                    "counts": [["date": "2026-09-14", "count": 1]]]]
        case "/api/agenda":
            precondition(scope == "other")
            payload = ["vault": scope, "date": "2026-09-14", "notes": [ref]]
        case "/api/graph":
            precondition(scope == "other")
            payload = ["vault": scope, "graph": ["center_id": 42, "nodes": [ref], "edges": [["source_id": 42, "target_id": 42]]]]
        case "/api/journal":
            precondition(scope == "other")
            payload = ["note_id": 20260914, "created": false]
        case "/api/note" where request.httpMethod == "POST":
            precondition(scope == "other")
            payload = ["note_id": 42, "title": "Same title"]
        case "/api/note" where request.httpMethod == "PUT":
            precondition(scope == "other" && id == "42", "save must retain source vault")
            payload = ["vault": scope, "note_id": 42, "etag": "saved", "saved": true]
        case "/api/note":
            precondition(scope == "other" && id == "42")
            payload = ["vault": scope,
                       "note": ref.merging(["body": "[[Same title]]", "etag": "read"]) { _, rhs in rhs },
                       "backlinks": [ref], "children": [ref], "trail": [ref],
                       "external": [["vault": "main", "note_id": 42, "file_kind": "note", "title": "Same title"]]]
        default: fatalError("Unexpected request: \(request)")
        }
        let data = try! JSONSerialization.data(withJSONObject: payload)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }
}

let configuration = URLSessionConfiguration.ephemeral
configuration.protocolClasses = [VaultProtocol.self]
let client = TrackClient(baseURL: URL(string: "http://vault.test")!, session: URLSession(configuration: configuration))
let suite = "VerifyVaultScope-\(UUID())"
let defaults = UserDefaults(suiteName: suite)!
defer { defaults.removePersistentDomain(forName: suite) }
let scope = VaultScope(client: client, defaults: defaults)
await scope.reload()
precondition(scope.label == "main" && scope.vaults.count == 2 && scope.unavailable.count == 1)
scope.select("other")
let restored = VaultScope(client: client, defaults: defaults)
await restored.reload()
precondition(restored.scope == "other" && restored.label == "other")
restored.select("removed")
await restored.reload()
precondition(restored.scope.isEmpty && defaults.string(forKey: VaultScope.storageKey) == nil)

do {
    let search = try await client.searchNotes(query: "Same title")
    precondition(search.results.map(\.qualifiedID.raw) == ["main~42", "other~42"])
    let note = try await client.getNote(search.results[1].qualifiedID)
    precondition(note.note.summary.qualifiedID.raw == "other~42")
    precondition(note.backlinks[0].noteID.raw == "other~42" && note.children![0].noteID.raw == "other~42")
    precondition(note.external![0].noteID.raw == "main~42", "nearest vault label must win")
    _ = try await client.saveNote(id: note.note.summary.qualifiedID, body: "Changed", etag: note.note.etag)
    let created = try await client.createNote(title: "Same title", vault: "other")
    precondition(created.noteID.raw == "other~42")
    let journal = try await client.openJournal(date: "2026-09-14", vault: "other")
    precondition(journal.noteID.raw == "other~20260914")
    let graph = try await client.getGraph(vault: "other")
    precondition(graph.graph.nodes[0].noteID.raw == "other~42" && graph.graph.edges[0].sourceID.raw == "other~42")
    let agenda = try await client.getAgenda(date: "2026-09-14", vault: "other")
    precondition(agenda.notes[0].noteID.raw == "other~42")
    _ = try await client.listNotes()
    _ = try await client.listNotes(sort: "created", vault: "other")
    _ = try await client.listDatedTasks()
    _ = try await client.getHierarchy()
    let browse = BrowseModel(client: client)
    await browse.loadNewNotes(vault: "other")
    precondition(browse.newNotes[0].qualifiedID.raw == "other~42")
    await browse.loadActivity(vault: "other")
    precondition(browse.activity["2026-09-14"] == 1)
    print("Vault scope checks passed: persistence, fallback, unavailable, federated queries, scoped creation/activity/graph, inherited IDs and save")
} catch {
    fatalError("Vault scope check failed: \(error)")
}
