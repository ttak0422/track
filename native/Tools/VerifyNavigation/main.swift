import Foundation
import TrackAPI
import TrackUI

final class NavigationHTTP: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let id = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "id" }?.value ?? "1"
        let data: [String: Any] = url.path == "/api/render" ? ["markdown": "body"] : [
            "note": ["note_id": id, "file_kind": "note", "path": "test.md", "title": id, "body": "body", "etag": "etag"],
            "backlinks": [], "children": [],
        ]
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: try! JSONSerialization.data(withJSONObject: data))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@main
struct VerifyNavigation {
    @MainActor
    static func main() async {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [NavigationHTTP.self]
        let client = TrackClient(baseURL: URL(string: "http://navigation.invalid")!, session: URLSession(configuration: config))
        let reader = NoteReaderModel(client: client, confirmDiscard: { false })
        let navigation = WorkspaceNavigation(reader: reader)
        await navigation.open(TrackID("1"))
        navigation.readerDidOpen(reader.currentID) // SwiftUI observer must not duplicate history.
        precondition(!navigation.canGoBack)
        for surface in [WorkspaceSurface.calendar, .graph, .browse, .tasks, .voice, .requests] {
            navigation.select(surface)
            await navigation.open(TrackID("work~2"))
            navigation.readerDidOpen(reader.currentID)
            precondition(navigation.selected == .notes && reader.currentID?.raw == "work~2")
            let returned = await navigation.back()
            precondition(returned && navigation.selected == surface)
            let previous = await navigation.back()
            navigation.readerDidOpen(reader.currentID)
            precondition(previous && navigation.selected == .notes && reader.currentID?.raw == "1")
        }
        navigation.select(.calendar)
        await navigation.open(TrackID("work~2"))
        await navigation.back()
        reader.beginEditing()
        reader.draftBody = "unsaved"
        let cancelled = await navigation.back()
        precondition(!cancelled && navigation.selected == .calendar && reader.draftBody == "unsaved")
        let refused = await navigation.open(TrackID("3"))
        precondition(!refused && navigation.selected == .calendar && reader.currentID?.raw == "work~2")
        let reopened = await navigation.open(TrackID("work~2"))
        precondition(reopened && navigation.selected == .notes && reader.draftBody == "unsaved")
        precondition(WorkspaceNavigation.directNoteID("123")?.raw == "123")
        precondition(WorkspaceNavigation.directNoteID("日本~abc")?.raw == "日本~abc")
        precondition(WorkspaceNavigation.directNoteID("１２３") == nil)
        precondition(WorkspaceNavigation.directNoteID("work~123#Heading") == nil)
        precondition(WorkspaceNavigation.directNoteID("A title") == nil)
        precondition(WorkspaceNavigation.directNoteID("work~") == nil)
        print("Navigation checks passed: shared reader, source-surface return, history, cancelled drafts, qualified IDs.")
    }
}
