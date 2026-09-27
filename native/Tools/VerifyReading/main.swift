import Foundation
import TrackAPI
import TrackUI

func ref(_ id: String, kind: String = "note", seen: Int = 0, read: Int = 0) -> NoteRef {
    let data = try! JSONSerialization.data(withJSONObject: ["note_id": id, "file_kind": kind,
        "title": "Sample", "seen_at": seen, "read_at": read])
    return try! JSONDecoder().decode(NoteRef.self, from: data)
}

final class DayProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}
    override func startLoading() {
        let url = request.url!
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)!.queryItems ?? []
        let value: [String: Any]
        switch url.path {
        case "/api/note":
            value = ["vault": "other", "note": ["note_id": 20200103, "file_kind": "journal", "title": "Past journal", "body": "# Past", "etag": "one"], "backlinks": []]
        case "/api/render":
            value = ["markdown": "# Past", "includes": []]
        case "/api/agenda":
            precondition(query.contains { $0.name == "date" && $0.value == "2020-01-03" }, "use the opened journal date")
            precondition(query.contains { $0.name == "vault" && $0.value == "other" }, "use its vault")
            value = ["vault": "other", "date": "2020-01-03", "notes": [
                ["note_id": 20200103, "file_kind": "journal", "title": "Past journal"],
                ["note_id": 42, "file_kind": "note", "title": "Worked on that day"]]]
        default: fatalError("unexpected request \(url)")
        }
        let data = try! JSONSerialization.data(withJSONObject: value)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }
}

let suite = "VerifyReading-\(UUID())"
let defaults = UserDefaults(suiteName: suite)!
defer { defaults.removePersistentDomain(forName: suite) }
let store = ReadingStore(defaults: defaults)
store.markSeen("other~42")
let launch = ref("42")
let other = ref("other~42")
precondition(store.isNew(launch) && !store.isNew(other), "vaults must not share seen state")
store.adopt([ref("work~42", read: 100)])
precondition(store.isRead("work~42") && !store.isNew(ref("work~42")))
store.adopt([ref("work~42")])
precondition(store.isRead("work~42"), "server adoption must be monotonic")
let restored = ReadingStore(defaults: defaults)
precondition(restored.isRead("work~42") && restored.isNew(launch))
precondition(ReadingStore.readThreshold(for: String(repeating: "😀", count: 1000)) == 100, "match Web UTF-16 length")
precondition(!store.recordView("42", seconds: 19, text: "short"))
precondition(store.recordView("42", seconds: 1, text: "short"))
precondition(!store.recordView("42", seconds: 1, text: "short"), "read milestone only once")
precondition(NoteReaderModel.journalDate(ref("other~20200103", kind: "journal")) == "2020-01-03")
precondition(NoteReaderModel.journalDate(ref("20200103")) == nil)
precondition(NoteReaderModel.journalDate(ref("202001", kind: "journal")) == nil)

let config = URLSessionConfiguration.ephemeral
config.protocolClasses = [DayProtocol.self]
let client = TrackClient(baseURL: URL(string: "http://day.test")!, session: URLSession(configuration: config))
let reader = NoteReaderModel(client: client, confirmDiscard: { false })
await reader.open(TrackID("other~20200103"))
await reader.loadDayNotes()
precondition(reader.dayNotesError == nil && reader.dayNotes.count == 1)
precondition(reader.dayNotes[0].noteID.raw == "other~42", "exclude journal itself, include ordinary notes")
print("Reading checks passed: vault isolation, shared milestones, persistence, UTF-16 timing and journal activity")
try await verifyCalendarIndex()
if CommandLine.arguments.contains("--calendar-benchmark") { try await benchmarkCalendarIndex() }
