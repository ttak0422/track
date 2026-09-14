import Foundation
import TrackAPI
import TrackUI

// No real agent dispatch: simulate persistence followed by a lost HTTP response
// at create, retry and save, then verify that repeating the action cannot duplicate it.
struct MockHTTPError: Error { let status: Int; let message: String }

final class RequestServer: @unchecked Sendable {
    let lock = NSLock()
    var records: [String: [String: Any]] = [:]
    var createKeys: [String: String] = [:]
    var createInputs: [[String: Any]] = []
    var saveKeys: [String] = []
    var retryCount = 0
    var savedCount = 0
    var loseCreate = true
    var loseSave = true
    var loseRetry = true

    func respond(_ request: URLRequest) throws -> [String: Any] {
        try lock.withLock {
            let url = request.url!
            let query = URLComponents(url: url, resolvingAgainstBaseURL: false)!.queryItems ?? []
            if url.path != "/api/agents" { precondition(query.contains { $0.name == "vault" && $0.value == "other" }) }
            if url.path == "/api/agents" {
                return ["agents": [["id": "assistant", "name": "Assistant", "operations": ["explain", "research", "update"], "agmsg_available": true],
                                    ["id": "offline", "operations": [], "agmsg_available": false]]]
            }
            if request.httpMethod == "GET" {
                if url.path == "/api/requests" { return ["requests": Array(records.values), "next_cursor": ""] }
                return records[url.lastPathComponent]!
            }
            precondition(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
            let input = try JSONSerialization.jsonObject(with: Self.body(request)) as! [String: Any]
            if url.path == "/api/requests" {
                createInputs.append(input)
                if input["instruction"] as? String == "stale update" { throw MockHTTPError(status: 409, message: "Update target changed since it was read") }
                let key = input["client_request_id"] as! String
                if let id = createKeys[key] { return ["request": records[id]!, "reused": true] }
                let context = input["context"] as! [String: Any]
                if let note = context["note"] as? [String: Any] {
                    precondition(note["note_id"] as? Int == 42, "outgoing note ID must be a bare JSON integer")
                    precondition(note["vault"] as? String == "other")
                }
                if input["intent"] as? String == "update" {
                    let target = input["update_target"] as! [String: Any]
                    precondition(target["note_id"] as? Int == 42 && target["etag"] as? String == "etag-1")
                }
                let id = "req-\(records.count + 1)"
                var record = input
                record["id"] = id; record["vault"] = "other"; record["status"] = "queued"
                records[id] = record; createKeys[key] = id
                if loseCreate { loseCreate = false; throw URLError(.networkConnectionLost) }
                return ["request": record, "reused": false]
            }
            let id = url.deletingLastPathComponent().lastPathComponent
            var record = records[id]!
            switch url.lastPathComponent {
            case "cancel": record["status"] = "cancelled"
            case "retry":
                retryCount += 1
                record["status"] = "queued"
                records[id] = record
                if loseRetry { loseRetry = false; throw URLError(.networkConnectionLost) }
            case "save":
                let key = input["client_request_id"] as! String
                saveKeys.append(key)
                var result = record["result"] as! [String: Any]
                if result["saved"] == nil {
                    savedCount += 1
                    result["saved"] = ["vault": "destination", "note_id": 99, "title": input["title"]!, "status": "saved"]
                    record["result"] = result
                    records[id] = record
                    if loseSave { loseSave = false; throw URLError(.networkConnectionLost) }
                }
            default: fatalError("Unexpected action \(url)")
            }
            records[id] = record
            return record
        }
    }
    func settle(_ id: String, status: String, result: [String: Any]) {
        lock.withLock { records[id]!["status"] = status; records[id]!["result"] = result }
    }
    private static func body(_ request: URLRequest) -> Data {
        if let data = request.httpBody { return data }
        guard let stream = request.httpBodyStream else { return Data("{}".utf8) }
        stream.open(); defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while true {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(buffer, count: count)
        }
        return data
    }
}
final class RequestProtocol: URLProtocol, @unchecked Sendable {
    static let server = RequestServer()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}
    override func startLoading() {
        do {
            let result = try Self.server.respond(request)
            let data = try JSONSerialization.data(withJSONObject: result)
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch let error as MockHTTPError {
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: error.status, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: try! JSONSerialization.data(withJSONObject: ["error": error.message]))
            client?.urlProtocolDidFinishLoading(self)
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }
}
let configuration = URLSessionConfiguration.ephemeral
configuration.protocolClasses = [RequestProtocol.self]
let client = TrackClient(baseURL: URL(string: "http://agent-request.test")!, session: URLSession(configuration: configuration))
let note = RequestNoteRef(id: TrackID("other~42"), title: "Source", body: "Original", etag: "etag-1", fileKind: "note")
let target = AgentRequestTarget(vault: "other", title: "Source", quote: "Selected words", note: note)
let model = AgentRequestsModel(client: client, target: target)
await model.loadAgents()
precondition(model.agentID == "assistant" && model.canSend)
model.agentID = "offline"
precondition(!model.canSend, "unavailable agents must not receive requests")
model.chooseAgent()
await model.send()
precondition(model.actionError != nil && !model.instruction.isEmpty, "transport failure preserves the composer")
await model.send()
precondition(model.selected?.id == "req-1" && model.instruction.isEmpty)
let server = RequestProtocol.server
precondition(server.lock.withLock { server.records.count == 1 && server.createInputs.count == 2 })
precondition(server.lock.withLock { server.createInputs[0]["client_request_id"] as? String == server.createInputs[1]["client_request_id"] as? String })
server.settle("req-1", status: "completed", result: ["answer_markdown": "# Answer", "sources": ["https://example.test/source"], "uncertain_points": "Uncertain", "unresolved": ["Unresolved"]])
await model.refresh()
precondition(model.selected?.result?.uncertain == ["Uncertain"] && model.selected?.canSave == true)
model.saveTitle = "Saved answer"
model.saveVault = "destination"
await model.saveAnswer()
precondition(model.actionError != nil)
await model.saveAnswer()
precondition(model.selected?.result?.saved?.noteID.raw == "destination~99")
precondition(server.lock.withLock { server.savedCount == 1 && server.saveKeys.count == 2 && server.saveKeys[0] == server.saveKeys[1] })
model.followUp(model.selected!)
model.instruction = "Tell me more"
await model.send()
precondition(model.selected?.parentRequestID == "req-1")
precondition(server.lock.withLock { (server.createInputs.last!["context"] as! [String: Any])["prior_answers"] != nil })
server.settle(model.selected!.id, status: "running", result: [:])
await model.refresh()
precondition(model.selected?.canCancel == true)
await model.cancel(model.selected!)
precondition(model.selected?.status == "cancelled")
model.intent = .update
model.instruction = "stale update"
await model.send()
precondition(model.actionError == "Update target changed since it was read" && model.instruction == "stale update")
model.instruction = "Update the source"
await model.send()
precondition(server.lock.withLock { server.createInputs.suffix(2).compactMap { $0["client_request_id"] as? String }.first != server.createInputs.last!["client_request_id"] as? String })
let updateID = model.selected!.id
server.settle(updateID, status: "conflict", result: ["proposed_body": "Proposed", "apply": ["reason": "Target changed", "before_body": "Original", "before_etag": "etag-1"]])
await model.refresh()
precondition(model.selected?.canRetry == true && model.selected?.canSave == false && model.selected?.result?.apply?.reason == "Target changed")
let stale = model.selected!
await model.retry(stale)
precondition(model.actionError != nil)
await model.retry(stale)
precondition(model.selected?.status == "queued" && server.lock.withLock { server.retryCount == 1 })
let reopened = AgentRequestsModel(client: client, target: target)
await reopened.refresh()
precondition(reopened.requests.count == 3, "history survives panel recreation")
do {
    _ = try await client.getAgentRequest(id: "../elsewhere", vault: "other")
    fatalError("path injection accepted")
} catch let error as APIError { precondition(error.status == 400) }
catch { fatalError("unexpected error \(error)") }
print("Agent request checks passed: scoped numeric IDs, create/save replay, follow-up context, cancel, update conflict, retry reconciliation, unavailable agents, reopened history")
