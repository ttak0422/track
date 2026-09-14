import Foundation
import TrackAPI
import TrackUI

var transcript = VoiceTranscriptState()
transcript.edit("Typed introduction")
transcript.receive("こんにちは", isFinal: false)
precondition(transcript.text == "Typed introduction\nこんにちは")
transcript.edit("Edited introduction\nこんにちは")
transcript.receive("こんにちは世界", isFinal: true)
precondition(transcript.text == "Edited introduction\nこんにちは世界\n", "speech must not overwrite typed edits")
transcript.receive("second phrase", isFinal: false)
transcript.edit("Edited introduction\nこんにちは世界\nmy wording")
transcript.receive("second phrase continues", isFinal: true)
precondition(transcript.text.hasSuffix("my wording continues\n"), "editing an interim must not append it twice")
let beforeRestart = transcript.text
transcript.receive("next session", isFinal: true)
precondition(transcript.text == beforeRestart + "next session\n", "restarting retains earlier text")
transcript.receive("visible interim", isFinal: false)
transcript.finishSegment()
precondition(transcript.text.hasSuffix("visible interim\n"), "stop fallback keeps visible unfinalized words")
transcript.receive("clear this", isFinal: false)
transcript.clear()
transcript.receive("clear this", isFinal: true)
precondition(transcript.text.isEmpty, "late final cannot resurrect cleared speech")
transcript.receive("new words", isFinal: true)
precondition(transcript.text == "new words\n")

var corrected = VoiceTranscriptState()
corrected.receive("I scream", isFinal: false)
corrected.edit("ice")
corrected.edit("ice cream")
corrected.receive("ice cream today", isFinal: true)
precondition(corrected.text == "ice cream today\n", "a corrected prefix must retain new words without repeating edited words")
corrected.receive("new draft", isFinal: false)
corrected.edit("ice cream today\nmy wording")
corrected.receive("a revised draft with more", isFinal: false)
corrected.receive("a revised draft with more words", isFinal: true)
precondition(corrected.text == "ice cream today\nmy wording\n\n[認識の訂正候補・要確認]\na revised draft with more words\n", "ambiguous corrections retain one complete labelled candidate, never each interim")
corrected.receive("clear this", isFinal: false)
corrected.clear()
corrected.receive("a corrected version of cleared speech", isFinal: true)
precondition(corrected.text.isEmpty, "a correction must not resurrect explicitly cleared speech")

var buffered = VoiceSpeechBuffer()
var interrupted = VoiceTranscriptState()
interrupted.receive("visible draft", isFinal: false)
buffered.append("first segment", isFinal: false)
buffered.finishSegment(fallback: interrupted.interim) // error during selection/IME
buffered.append("second draft", isFinal: false) // recognition restarted while still interacting
buffered.append("second segment", isFinal: false)
for event in buffered.drain() { interrupted.receive(event.text, isFinal: event.isFinal) }
interrupted.finishSegment() // stop/manual-save snapshot
precondition(interrupted.text == "first segment\nsecond segment\n", "a restart must not replace the previous deferred segment")
precondition(buffered.drain().isEmpty, "flushing the buffer twice cannot duplicate speech")
precondition(VoiceTranscriptState.selectedText(in: "日本語 🐈 text", range: NSRange(location: 4, length: 2)) == "🐈")
precondition(VoiceTranscriptState.selectedText(in: "entire transcript", range: NSRange(location: 3, length: 0)).isEmpty, "no selection never searches the whole transcript")
precondition(VoiceTranscriptState.selectedText(in: "text", range: NSRange(location: NSNotFound, length: 0)).isEmpty)

final class JournalServer: @unchecked Sendable {
    let lock = NSLock()
    var body = "Existing journal\n"
    var revision = 1
    var writes = 0
    var loseResponse = true
    var failBeforeWrite = false
    func respond(_ request: URLRequest) throws -> [String: Any] {
        try lock.withLock {
            let url = request.url!
            let query = URLComponents(url: url, resolvingAgainstBaseURL: false)!.queryItems ?? []
            precondition(query.contains { $0.name == "vault" && $0.value == "other" })
            if url.path == "/api/journal" { return ["vault": "other", "note_id": 20260914, "created": false] }
            precondition(query.contains { $0.name == "id" && $0.value == "20260914" })
            if request.httpMethod == "GET" {
                return ["vault": "other", "note": ["note_id": 20260914, "file_kind": "journal", "title": "20260914", "body": body, "etag": "\(revision)"], "backlinks": []]
            }
            precondition(request.httpMethod == "PUT")
            if failBeforeWrite { failBeforeWrite = false; throw URLError(.notConnectedToInternet) }
            let input = try JSONSerialization.jsonObject(with: Self.requestBody(request)) as! [String: Any]
            precondition(input["etag"] as? String == "\(revision)", "write must use the read etag")
            body = input["body"] as! String
            revision += 1
            writes += 1
            if loseResponse { loseResponse = false; throw URLError(.networkConnectionLost) }
            return ["vault": "other", "note_id": 20260914, "etag": "\(revision)", "saved": true]
        }
    }
    private static func requestBody(_ request: URLRequest) -> Data {
        if let body = request.httpBody { return body }
        let stream = request.httpBodyStream!
        stream.open(); defer { stream.close() }
        var data = Data()
        var bytes = [UInt8](repeating: 0, count: 4096)
        while true {
            let count = stream.read(&bytes, maxLength: bytes.count)
            if count <= 0 { break }
            data.append(bytes, count: count)
        }
        return data
    }
}
final class JournalProtocol: URLProtocol, @unchecked Sendable {
    static let server = JournalServer()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}
    override func startLoading() {
        do {
            let data = try JSONSerialization.data(withJSONObject: Self.server.respond(request))
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }
}
let configuration = URLSessionConfiguration.ephemeral
configuration.protocolClasses = [JournalProtocol.self]
let client = TrackClient(baseURL: URL(string: "http://voice.test")!, session: URLSession(configuration: configuration))
let writer = VoiceJournalWriter(client: client)
let server = JournalProtocol.server
await writer.save("First words\n", vault: "other", date: "2026-09-14")
precondition(writer.error != nil && writer.hasPendingSave)
await writer.save("First words\n", vault: "other", date: "2026-09-14")
precondition(writer.savedTranscript == "First words\n" && !writer.hasPendingSave)
precondition(server.lock.withLock { server.writes == 1 }, "lost response must not append twice")
await writer.save("First words\nSecond words\n", vault: "other", date: "2026-09-14")
precondition(server.lock.withLock { server.body == "Existing journal\n\nFirst words\n\nSecond words\n" && server.writes == 2 })
await writer.save("First words\nSecond words\n", vault: "other", date: "2026-09-14")
precondition(server.lock.withLock { server.writes == 2 }, "manual save and stop share the checkpoint")
server.lock.withLock { server.failBeforeWrite = true }
await writer.save("First words\nSecond words\nThird words\n", vault: "other", date: "2026-09-14")
precondition(writer.hasPendingSave)
await writer.save("First words\nSecond words\nThird words\nFourth words\n", vault: "other", date: "2026-09-14")
precondition(writer.savedTranscript.hasSuffix("Fourth words\n") && server.lock.withLock { server.writes == 4 }, "retry saves its original snapshot before newer words")
await writer.save("Edited saved prefix", vault: "other", date: "2026-09-14")
precondition(writer.error != nil && server.lock.withLock { server.writes == 4 }, "editing a saved prefix must not duplicate the whole transcript")
writer.reset()
server.lock.withLock { server.failBeforeWrite = true }
await writer.save("Pending words", vault: "other", date: "2026-09-14")
server.lock.withLock { server.body = "Externally changed\n"; server.revision += 1 }
await writer.save("Pending words and newer words", vault: "other", date: "2026-09-14")
precondition(writer.hasPendingSave && writer.error != nil && writer.savedTranscript.isEmpty)
precondition(server.lock.withLock { server.body == "Externally changed\n" && server.writes == 4 }, "uncertain saves must not overwrite external edits or forget their pending snapshot")
print("Voice checks passed: selected-only search, editable speech, restart/late-final handling, shared checkpoint, lost-response retry, newer snapshot retention and conflict safety")
