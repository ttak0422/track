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
            } else if url.path == "/api/resolve" {
                let term = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "term" }?.value ?? "1"
                data = ["found": true, "note": ["note_id": term, "file_kind": "note", "title": term]]
            } else if url.path == "/api/render" {
                let requestBody = (try? JSONSerialization.jsonObject(with: request.httpBody ?? Data())) as? [String: Any]
                data = ["markdown": requestBody?["body"] as? String ?? "# Start\n\nFirst paragraph\n\n## Last\n\nEnd"]
            } else if method == "POST" {
                data = ["note_id": "created", "title": "Created"]
            } else if method == "PUT" {
                data = ["note_id": id, "etag": "saved", "saved": true]
            } else {
                data = ["note": ["note_id": id, "file_kind": "note", "path": "test.md", "title": id, "body": id == "follow" ? "# Start\n\nFirst paragraph\n\n## Last\n\nEnd" : "disk-\(id)", "etag": "original"], "backlinks": [], "children": []]
            }
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: try! JSONSerialization.data(withJSONObject: data))
            client?.urlProtocolDidFinishLoading(self)
        }
    }
    override func stopLoading() {}
}

// Models the server's etag check, including an external write between PUT
// acknowledgment and the reader's follow-up GET.
final class SaveServer: @unchecked Sendable {
    private let lock = NSLock()
    private var body = "initial"
    private var etag = "initial-etag"
    private var writes = 0
    private var externalWrite = false
    private var failedRead = false

    func reset(externalWrite: Bool = false, failedRead: Bool = false) {
        lock.withLock {
            body = "initial"
            etag = "initial-etag"
            writes = 0
            self.externalWrite = externalWrite
            self.failedRead = failedRead
        }
    }

    func respond(to request: URLRequest) -> (Int, [String: Any]) {
        lock.withLock {
            if request.url!.path == "/api/render" { return (200, ["markdown": body]) }
            if request.httpMethod == "PUT" {
                var bytes = request.httpBody ?? Data()
                if let stream = request.httpBodyStream {
                    stream.open()
                    defer { stream.close() }
                    var buffer = [UInt8](repeating: 0, count: 1024)
                    while stream.hasBytesAvailable {
                        let count = stream.read(&buffer, maxLength: buffer.count)
                        if count <= 0 { break }
                        bytes.append(contentsOf: buffer.prefix(count))
                    }
                }
                let payload = try! JSONSerialization.jsonObject(with: bytes) as! [String: Any]
                guard payload["etag"] as? String == etag else { return (409, ["error": "etag mismatch"]) }
                writes += 1
                body = payload["body"] as! String
                etag = "saved-\(writes)"
                let saved = etag
                if externalWrite {
                    body = "external edit"
                    etag = "external-etag"
                    externalWrite = false
                }
                return (200, ["note_id": "1", "etag": saved, "saved": true])
            }
            if writes > 0, failedRead {
                failedRead = false
                return (503, ["error": "read unavailable"])
            }
            return (200, ["note": ["note_id": "1", "file_kind": "note", "path": "test.md", "title": "1", "body": body, "etag": etag], "backlinks": [], "children": []])
        }
    }
}

final class SaveHTTP: URLProtocol, @unchecked Sendable {
    static let server = SaveServer()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let delay = request.httpMethod == "PUT" ? 0.15 : 0.005
        DispatchQueue.global().asyncAfter(deadline: .now() + delay) { [self] in
            let (status, data) = Self.server.respond(to: request)
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
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
    static func verifySaveBaseline() async throws {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [SaveHTTP.self]
        let client = TrackClient(baseURL: URL(string: "http://save.invalid")!, session: URLSession(configuration: config))
        for scenario in ["external", "normal", "failed-read"] {
            SaveHTTP.server.reset(externalWrite: scenario == "external", failedRead: scenario == "failed-read")
            let reader = NoteReaderModel(client: client)
            await reader.open(TrackID("1"))
            reader.beginEditing()
            reader.draftBody = "submitted"
            let save = Task { await reader.saveDraft() }
            try await Task.sleep(for: .milliseconds(30))
            reader.draftBody = "submitted plus typing"
            await save.value
            precondition(reader.draftBody == "submitted plus typing" && reader.isDirty)
            precondition(reader.loadedBody == "submitted", "\(scenario): preserve the acknowledged body baseline")
            if case .loaded(let response) = reader.state {
                precondition(response.note.etag == "saved-1", "\(scenario): preserve the PUT token for additional edits")
            } else { preconditionFailure("Saved note disappeared") }
            if scenario == "external" { precondition(reader.saveConflict != nil) }
            if scenario == "failed-read" { precondition(reader.saveError != nil) }
            await reader.saveDraft()
            let disk = try await client.getNote(TrackID("1"))
            if scenario == "external" {
                precondition(reader.saveConflict != nil && reader.isDirty)
                precondition(disk.note.body == "external edit", "Retry must not overwrite the external writer")
            } else {
                precondition(reader.saveError == nil && reader.saveConflict == nil && !reader.isDirty)
                precondition(disk.note.body == "submitted plus typing")
            }
        }
    }

    @MainActor
    static func main() async throws {
        try await verifySaveBaseline()
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
        reader.beginEditing()
        reader.draftBody = "anchor must keep this draft"
        confirmation.allow = false
        let confirmations = confirmation.count
        await reader.openWikilink(target: "#^Block-1")
        precondition(reader.scrollTarget == "block-Block-1" && reader.isDirty && confirmation.count == confirmations)
        reader.discardDraft()
        await reader.openWikilink(target: "archive:Target##設計")
        precondition(reader.currentID == TrackID.qualify(vault: "archive", id: "Target"))
        precondition(reader.scrollTarget == "h-設計")
        let staleAnchor = Task { await reader.openWikilink(target: "archive:slow#Old") }
        try await Task.sleep(for: .milliseconds(30))
        await reader.openWikilink(target: "archive:2#New")
        _ = await staleAnchor.value
        precondition(reader.currentID == TrackID.qualify(vault: "archive", id: "2") && reader.scrollTarget == "h-new")
        await reader.openWikilink(target: "other~42#^Block-1")
        precondition(reader.currentID?.raw == "other~42" && reader.scrollTarget == "block-Block-1")
        await reader.openWikilink(target: "43#設計")
        precondition(reader.currentID?.raw == "other~43" && reader.scrollTarget == "h-設計")
        let request = reader.scrollRequest
        await reader.openWikilink(target: "#New")
        precondition(reader.scrollRequest > request, "Repeated same-note jumps must scroll again")
        func position(_ id: String, line: Int = 7, top: Int = 5) throws -> FollowState {
            try JSONDecoder().decode(FollowState.self, from: JSONSerialization.data(withJSONObject: [
                "note_id": id, "line": line, "top_line": top, "line_count": 7
            ]))
        }
        await reader.open(TrackID("follow"))
        let followed = await reader.applyFollowState(try position("follow"))
        precondition(followed && reader.scrollTarget == "source-line-5")
        _ = await reader.applyFollowState(try position("follow", top: 0))
        precondition(reader.scrollTarget == "source-line-7")
        reader.beginEditing()
        reader.draftBody = "follow must preserve this draft"
        let followConfirmations = confirmation.count
        let blocked = await reader.applyFollowState(try position("other"))
        precondition(!blocked && reader.isDirty && reader.currentID?.raw == "follow" && confirmation.count == followConfirmations)
        reader.discardDraft()
        let cancelledFollow = Task { await reader.applyFollowState(try position("slow")) }
        try await Task.sleep(for: .milliseconds(30))
        cancelledFollow.cancel()
        let acceptedCancelledFollow = try await cancelledFollow.value
        precondition(!acceptedCancelledFollow && reader.currentID?.raw == "follow")
        print("Reader checks passed: draft/save protection, tabs/search, anchor navigation and editor follow")
    }
}
