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
            let status = id == "failure" || (id == "savefailure" && method == "PUT") ? 503 : (method == "PUT" && id == "conflict" ? 409 : 200)
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
        print("Reader checks passed: navigation cancellation, failed reads, conflicts, concurrent edits, stale opens and close.")
    }
}
