import Foundation
import Synchronization
import TrackAPI
import TrackUI

final class TaskHTTP: URLProtocol, @unchecked Sendable {
    struct State { var state = "TODO"; var due = "2026-09-14"; var text = "Task"; var writes = 0; var conflict = false; var failRead = false; var failRender = false }
    static let state = Mutex(State())
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}
    override func startLoading() {
        let url = request.url!
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)!.queryItems ?? []
        var status = 200
        let value: [String: Any] = Self.state.withLock { state in
            if url.path == "/api/task" {
                let stream = request.httpBodyStream!
                stream.open(); defer { stream.close() }
                var bytes = [UInt8](repeating: 0, count: 4096)
                let count = stream.read(&bytes, maxLength: bytes.count)
                let payload = try! JSONSerialization.jsonObject(with: Data(bytes.prefix(count))) as! [String: Any]
                precondition(query.contains { $0.name == "vault" && $0.value == "other" })
                guard let etag = payload["etag"] as? String, !etag.isEmpty else {
                    status = 400; return ["error": "etag is required"]
                }
                if state.conflict || etag != "v\(state.writes)" || payload["expect"] as? String != state.state {
                    status = 409; return ["error": "changed"]
                }
                state.writes += 1
                if let next = payload["state"] as? String { state.state = next }
                if let due = payload["due"] as? String { state.due = due }
            }
            let done = state.state == "DONE" || state.state == "CANCELLED"
            var item: [String: Any] = ["line": 1, "state": state.state, "done": done, "text": state.text]
            if !state.due.isEmpty { item["due"] = state.due }
            let body = "- [\(done ? "x" : " ")] Task"
            switch url.path {
            case "/api/tasks":
                var row = item
                row.merge(["note_id": "other~42", "title": "Own note", "file_kind": "note"]) { _, new in new }
                let open = query.contains { $0.name == "open" }
                return ["tasks": (open ? !done : !state.due.isEmpty) ? [row] : []]
            case "/api/note":
                if state.failRead { status = 503; return ["error": "offline"] }
                return ["vault": "other", "note": ["note_id": 42, "file_kind": "note", "title": "Own note", "body": body, "etag": "v\(state.writes)", "tasks": ["items": [item]]], "backlinks": []]
            case "/api/render":
                if state.failRender { status = 503; return ["error": "renderer offline"] }
                return ["markdown": body, "includes": []]
            case "/api/task": return ["items": [item], "etag": "v\(state.writes)"]
            default: fatalError("unexpected request \(url)")
            }
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: try! JSONSerialization.data(withJSONObject: value))
        client?.urlProtocolDidFinishLoading(self)
    }
}

let config = URLSessionConfiguration.ephemeral
config.protocolClasses = [TaskHTTP.self]
let client = TrackClient(baseURL: URL(string: "http://task.test")!, session: URLSession(configuration: config))
let inline = TasksModel(client: client, noteID: TrackID("other~42"))
TaskHTTP.state.withLock { $0.due = "" }
await inline.reload()
precondition(inline.rows.count == 1 && inline.rows[0].noteID.raw == "other~42", "inline board includes its own undated tasks")
await inline.setState(row: inline.rows[0], to: "DONE")
precondition(inline.rows.count == 1 && inline.rows[0].item.done)
let all = TasksModel(client: client)
all.showOpenOnly = true
TaskHTTP.state.withLock { $0.state = "TODO" }
await all.reload()
await all.setState(row: all.rows[0], to: "DONE")
precondition(all.rows.isEmpty, "completed tasks leave open-only listing")
TaskHTTP.state.withLock { $0.due = "2026-09-14" }
all.showOpenOnly = false
await all.reload()
await all.setDate(row: all.rows[0], field: .due, date: "")
precondition(all.rows.isEmpty, "clearing the last date removes dated rows")
TaskHTTP.state.withLock { $0.state = "TODO"; $0.due = "2026-09-14" }
await all.reload()
let retained = all.rows[0]
let beforeStale = TaskHTTP.state.withLock { $0.writes }
TaskHTTP.state.withLock { $0.text = "A different task at the same line" }
await all.reload()
await all.setDate(row: retained, field: .due, date: "2027-01-01")
precondition(all.lastConflict != nil && TaskHTTP.state.withLock { $0.writes } == beforeStale,
             "a retained popover must not edit a replacement task, even with the same state")
TaskHTTP.state.withLock { $0.text = "Task" }
let reader = NoteReaderModel(client: client, confirmDiscard: { false })
await reader.open(TrackID("other~42"))
await reader.setTaskState(line: 1, to: "TODO")
precondition(reader.loadedBody == "- [ ] Task" && !reader.isDirty, "refresh body and draft together after task write")
reader.beginEditing()
reader.draftBody = "precious draft"
let writes = TaskHTTP.state.withLock { $0.writes }
await reader.setTaskState(line: 1, to: "DONE")
precondition(TaskHTTP.state.withLock { $0.writes } == writes && reader.draftBody == "precious draft")
reader.discardDraft()
TaskHTTP.state.withLock { $0.conflict = true }
await reader.setTaskState(line: 1, to: "DONE")
precondition(reader.saveConflict != nil && reader.loadedBody == "- [ ] Task")
let baseline: String
if case .loaded(let response) = reader.state { baseline = response.note.etag } else { fatalError("reader lost note") }
TaskHTTP.state.withLock { $0.conflict = false; $0.failRead = true }
await reader.setTaskState(line: 1, to: "DONE")
if case .loaded(let response) = reader.state {
    precondition(response.note.etag == baseline, "failed body refresh must not bless stale body with the write etag")
}
TaskHTTP.state.withLock { $0.failRead = false; $0.failRender = true }
await reader.refreshOpenNote()
precondition(!reader.didRender && reader.renderedIncludes == nil && reader.loadedBody == "- [x] Task",
             "render failure must show fresh raw body, never old task lines with a fresh etag")
let currentWrites = TaskHTTP.state.withLock { $0.writes }
await reader.setTaskDate(line: 1, field: .due, date: "2027-01-01", expectedID: TrackID("other~42"), expectedETag: baseline)
await reader.setTaskDate(line: 1, field: .due, date: "2027-01-01", expectedID: TrackID("42"))
precondition(TaskHTTP.state.withLock { $0.writes } == currentWrites,
             "retained reader controls must not write a changed baseline or another note")
print("Task checks passed: own-note board, list membership, body refresh, dirty protection and conflicts")
