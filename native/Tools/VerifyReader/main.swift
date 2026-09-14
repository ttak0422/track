import Foundation
import TrackAPI
import TrackUI

// A real URLSession with deterministic HTTP responses and deliberately reordered
// reads. Run with `swift run --package-path native VerifyReader` (CLT compatible).
final class StubHTTP: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let id = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "id" }?.value ?? "1"
        let method = request.httpMethod ?? "GET"
        let delay = id == "slow" || method == "PUT" ? 0.15 : 0.005
        DispatchQueue.global().asyncAfter(deadline: .now() + delay) { [self] in
            let status = id == "missing" ? 404 : id == "failure" || (id == "savefailure" && method == "PUT") ? 503 : (method == "PUT" && id == "conflict" ? 409 : 200)
            let data: [String: Any]
            if status != 200 {
                data = ["error": "test failure"]
            } else if url.path == "/api/render" {
                data = ["markdown": "rendered"]
            } else if method == "POST" {
                data = ["note_id": "created", "title": "Created"]
            } else if method == "PUT" {
                data = ["note_id": id, "etag": "saved", "saved": true]
            } else {
                data = ["note": ["note_id": id, "file_kind": "note", "path": "test.md", "title": id, "body": "disk-\(id)", "etag": "original"], "backlinks": [], "children": []]
            }
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: try! JSONSerialization.data(withJSONObject: data))
            client?.urlProtocolDidFinishLoading(self)
        }
    }
    override func stopLoading() {}
}

@MainActor
final class Confirmation {
    var allow = false
    var count = 0
}

@main
struct VerifyReader {
    @MainActor
    static func main() async throws {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubHTTP.self]
        let client = TrackClient(baseURL: URL(string: "http://reader.invalid")!, session: URLSession(configuration: config))
        let confirmation = Confirmation()
        let reader = NoteReaderModel(client: client, confirmDiscard: {
            confirmation.count += 1
            return confirmation.allow
        })
        await reader.open(TrackID("1"))
        precondition(reader.loadedBody == "disk-1")
        reader.beginEditing()
        reader.draftBody = "precious draft"
        // Search click/Enter, MRU, recent, preview and follow all call open.
        for _ in 0..<6 { await reader.open(TrackID("2")) }
        await reader.openWikilink(target: "Other#Heading")
        precondition(!reader.close())
        let created = await reader.createNote(title: "New")
        precondition(!created && confirmation.count == 9)
        precondition(reader.currentID?.raw == "1" && reader.draftBody == "precious draft" && reader.isEditing)
        await reader.refreshOpenNote()
        precondition(reader.draftBody == "precious draft")
        confirmation.allow = true
        await reader.open(TrackID("failure"))
        precondition(reader.draftBody == "precious draft" && reader.isEditing && reader.saveError != nil)
        await reader.open(TrackID("conflict"))
        reader.beginEditing()
        reader.draftBody = "conflicting draft"
        await reader.saveDraft()
        precondition(reader.draftBody == "conflicting draft" && reader.saveConflict != nil)
        if case .loaded(let response) = reader.state { precondition(response.note.etag == "original") }
        await reader.open(TrackID("failure"))
        precondition(reader.draftBody == "conflicting draft")
        await reader.open(TrackID("savefailure"))
        reader.beginEditing()
        reader.draftBody = "offline draft"
        await reader.saveDraft()
        precondition(reader.draftBody == "offline draft" && reader.saveError != nil)
        await reader.open(TrackID("1"))
        reader.beginEditing()
        reader.draftBody = "submitted"
        let save = Task { await reader.saveDraft() }
        try await Task.sleep(for: .milliseconds(30))
        reader.draftBody = "typed while saving"
        let moved = await reader.open(TrackID("2"))
        precondition(!moved && !reader.close())
        await save.value
        precondition(reader.draftBody == "typed while saving" && reader.isDirty)
        reader.discardDraft()
        let slow = Task { await reader.open(TrackID("slow")) }
        try await Task.sleep(for: .milliseconds(30))
        await reader.open(TrackID("2"))
        _ = await slow.value
        precondition(reader.currentID?.raw == "2" && reader.loadedBody == "disk-2")
        let pending = Task { await reader.open(TrackID("slow")) }
        try await Task.sleep(for: .milliseconds(30))
        precondition(reader.close())
        _ = await pending.value
        precondition(reader.currentID == nil && !reader.isLoaded)
        let didCreate = await reader.createNote(title: "Created")
        precondition(didCreate && reader.currentID?.raw == "created")
        let suite = "track.verify-workspace.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        let tabs = NoteTabs(defaults: defaults)
        tabs.opened(TrackID("work~1"), title: "Old title")
        tabs.opened(TrackID("personal~1"), title: "Same bare ID")
        tabs.opened(TrackID("work~1"), title: "Updated title")
        let restored = NoteTabs(defaults: defaults)
        precondition(restored.entries.map(\.id.raw) == ["work~1", "personal~1"])
        precondition(restored.entries.first?.title == "Updated title" && restored.activeID?.raw == "work~1")
        precondition(restored.remove(TrackID("work~1"))?.raw == "personal~1")
        restored.opened(TrackID("failure"), title: "Offline")
        restored.opened(TrackID("missing"), title: "Deleted")
        await restored.restore(using: client)
        precondition(restored.entries.map(\.id.raw) == ["failure", "personal~1"])
        precondition(restored.activeID?.raw == "failure")
        precondition(restored.entries.last?.title == "1")
        let reloaded = NoteTabs(defaults: defaults)
        precondition(reloaded.entries == restored.entries)

        let rows: [[String: Any]] = [
            ["note_id": "3", "file_kind": "note", "title": "Path", "match": "path"],
            ["note_id": "2", "file_kind": "note", "title": "Body", "match": "body"],
            ["note_id": "1", "file_kind": "note", "title": "Title"],
        ]
        let results = try JSONDecoder().decode([SearchResult].self, from: JSONSerialization.data(withJSONObject: rows))
        let sections = SearchPresentation.sections(results)
        precondition(sections.map(\.title) == ["Titles", "Full text", "File name"])
        precondition(sections.flatMap(\.results).map(\.ref.noteID.raw) == ["1", "2", "3"])
        precondition(SearchPresentation.step(-1, by: 1, count: 3) == 0)
        precondition(SearchPresentation.step(0, by: -1, count: 3) == 2)
        precondition(SearchPresentation.step(2, by: 1, count: 3) == 0)
        precondition(SearchPresentation.step(0, by: 1, count: 0) == -1)
        for (text, query, expected) in [
            ("alpha beta OR gamma", "alpha beta OR gamma", ["alpha", "beta", "gamma"]),
            ("a+b [c]", "a+b", ["a+b"]),
            ("İSTANBUL Σ #日本語", "istanbul σ #日本語", ["İSTANBUL", "Σ", "#日本語"]),
            ("Track the tracker", "TRAC", ["Trac", "trac"]),
        ] {
            let matches = SearchPresentation.highlightRanges(in: text, query: query).map { (text as NSString).substring(with: $0) }
            precondition(matches == expected)
        }
        precondition(SearchPresentation.highlightRanges(in: "AND OR", query: "AND OR").isEmpty)
        print("Reader checks passed: navigation cancellation, failed reads, conflicts, concurrent edits, stale opens, close, persistent tabs and ordered search.")
    }
}
