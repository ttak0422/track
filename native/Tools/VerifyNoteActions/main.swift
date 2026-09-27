import AppKit
import Foundation
import TrackAPI
import TrackUI

final class MetaServer: @unchecked Sendable {
    private let lock = NSLock()
    private var requestCount = 0
    private var lastRequest: [String: Any] = [:]
    var count: Int { lock.withLock { requestCount } }
    var payload: [String: Any] { lock.withLock { lastRequest } }
    func response(_ request: URLRequest) -> (Int, [String: Any]) {
        lock.withLock {
            let query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems ?? []
            let id = query.first { $0.name == "id" }?.value ?? "1"
            let vault = query.first { $0.name == "vault" }?.value ?? ""
            if request.url!.path == "/api/render" { return (200, ["markdown": "body remains unchanged"]) }
            if request.url!.path == "/api/note" {
                return (200, ["note": ["note_id": id, "vault": vault, "file_kind": "note", "title": "Note", "body": "body remains unchanged", "etag": "body-token"], "backlinks": [], "children": []])
            }
            var meta: [String: Any] = ["title": "Note", "kind": "note", "tags": [], "description": "", "image": "", "icon": "", "flags": [], "props": "rating: 8\nactive: true\nunknown: [one, two]\n", "etag": "meta-token"]
            if request.httpMethod == "POST" {
                requestCount += 1
                var data = request.httpBody ?? Data()
                if let stream = request.httpBodyStream {
                    stream.open(); defer { stream.close() }
                    var bytes = [UInt8](repeating: 0, count: 1024)
                    while stream.hasBytesAvailable {
                        let n = stream.read(&bytes, maxLength: bytes.count)
                        if n <= 0 { break }
                        data.append(contentsOf: bytes.prefix(n))
                    }
                }
                lastRequest = try! JSONSerialization.jsonObject(with: data) as! [String: Any]
                guard lastRequest["etag"] as? String == "meta-token" else { return (409, ["error": "metadata changed"]) }
                if lastRequest["props"] as? String == "rating: invalid" { return (400, ["error": "property rating: must be a number"]) }
                if lastRequest["title"] as? String == "Duplicate" { return (400, ["error": "title already in use"]) }
                meta.merge(lastRequest) { _, new in new }
            }
            return (200, meta)
        }
    }
}

final class MetaHTTP: URLProtocol, @unchecked Sendable {
    static let server = MetaServer()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let (status, payload) = Self.server.response(request)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: try! JSONSerialization.data(withJSONObject: payload))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@main
struct VerifyNoteActions {
    @MainActor
    static func main() async throws {
        let selection = NoteSelection()
        let editor = NSTextView()
        editor.string = "outside\n**日本語**\nlast"
        editor.setSelectedRange((editor.string as NSString).range(of: "**日本語**\n"))
        selection.capture(from: editor, source: true)
        precondition(selection.markdown == "**日本語**\n" && selection.text == "**日本語**\n")
        let board = NSPasteboard(name: NSPasteboard.Name("track.verify-note-actions.\(UUID().uuidString)"))
        defer { board.releaseGlobally() }
        selection.copyMarkdown(to: board)
        precondition(board.string(forType: .string) == "**日本語**\n")
        selection.copyRich(to: board)
        precondition(board.string(forType: .html)?.contains("<strong>日本語</strong>") == true)
        precondition(board.string(forType: .html)?.contains("outside") == false)
        editor.setSelectedRange(NSRange(location: 0, length: 0))
        selection.capture(from: editor, source: true)
        precondition(selection.isEmpty)

        let rich = NSMutableAttributedString(string: "before selected after")
        rich.addAttribute(.font, value: NSFont.boldSystemFont(ofSize: 14), range: NSRange(location: 7, length: 8))
        editor.textStorage!.setAttributedString(rich)
        editor.setSelectedRange(NSRange(location: 7, length: 8))
        selection.capture(from: editor, source: false)
        precondition(selection.text == "selected" && selection.markdown == "**selected**")
        selection.copyRich(to: board)
        precondition(board.string(forType: .string) == "selected" && board.data(forType: .html) != nil)

        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 500, height: 300), styleMask: .borderless, backing: .buffered, defer: false)
        let root = NSView(frame: NSRect(x: 0, y: 0, width: 500, height: 300))
        let region = NSView(frame: NSRect(x: 0, y: 0, width: 220, height: 300))
        window.contentView = root
        root.addSubview(region)
        editor.frame = NSRect(x: 10, y: 10, width: 190, height: 100)
        region.addSubview(editor)
        window.makeFirstResponder(editor)
        selection.capture(in: window, region: region, source: false)
        precondition(selection.text == "selected")
        let search = NSTextView(frame: NSRect(x: 250, y: 10, width: 200, height: 100))
        search.string = "search text"
        search.setSelectedRange(NSRange(location: 0, length: 6))
        root.addSubview(search)
        precondition(window.makeFirstResponder(search))
        selection.capture(in: window, region: region, source: true)
        precondition(selection.isEmpty, "Keyboard focus moving outside the body must clear the quote: \(search.convert(search.visibleRect, to: region)) / \(region.visibleRect) / \(String(describing: window.firstResponder))")
        selection.adopt(NSAttributedString(string: "selected"), source: false)

        let table = NSTextTable()
        table.numberOfColumns = 3
        let cells = NSMutableAttributedString(string: "")
        for column in 1...2 {
            let paragraph = NSMutableParagraphStyle()
            paragraph.textBlocks = [NSTextTableBlock(table: table, startingRow: 2, rowSpan: 1, startingColumn: column, columnSpan: 1)]
            cells.append(NSAttributedString(string: "cell\(column)\n", attributes: [.paragraphStyle: paragraph]))
        }
        precondition(PortableMarkdown.selectedMarkdown(cells) == "| cell1 | cell2 |")
        let source = "| A | B |\n| --- | --- |\n| one | two |\n\n![[work:Note#Heading]] :only-contents\n\n[[other~42|Alias]]"
        let html = PortableMarkdown.html(source)
        precondition(html.contains("<table>") && html.contains("two</td>"), html)
        precondition(html.contains("work:Note") && html.contains("Alias") && !html.contains("![["))
        let breaks = PortableMarkdown.html("one<br>two\n\n`<br>`\n\n~~removed~~\n\n- [x] done")
        precondition(breaks.contains("one<br />\ntwo") && breaks.contains("<code>&lt;br&gt;</code>"), breaks)
        precondition(breaks.contains("<del>removed</del>") && breaks.contains("type=\"checkbox\""), breaks)
        precondition(PortableMarkdown.confluencePlainText("| A | B |\n| --- | --- |\n| x<br>y | z |") == "| A | B |\n| x\ny | z |")
        precondition(ShareLinks.wikilink(id: TrackID("work~42"), title: "Same title") == "[[work~42|Same title]]")
        precondition(ShareLinks.xIntentURL(title: "Note", publishedURL: nil) == nil)
        precondition(ShareLinks.xIntentURL(title: "Note", publishedURL: URL(string: "file:///tmp/note")) == nil)
        let published = URL(string: "https://notes.example.org/existing/path/")!
        let intent = ShareLinks.xIntentURL(title: "Note", publishedURL: published)!
        precondition(URLComponents(url: intent, resolvingAgainstBaseURL: false)?.queryItems?.first?.value == "Note\n\n\(published.absoluteString)")

        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MetaHTTP.self]
        let client = TrackClient(baseURL: URL(string: "http://meta.invalid")!, session: URLSession(configuration: config))
        let id = TrackID("work~42")
        let reader = NoteReaderModel(client: client)
        await reader.open(id)
        let meta = try await client.getNoteMeta(id)
        var request = SaveNoteMetaRequest(title: meta.title, tags: ["tag"], description: "", image: "", icon: "", flags: [], props: meta.props, etag: meta.etag)
        var saved = await reader.saveMeta(request, expectedID: id)
        precondition(saved && reader.loadedBody == "body remains unchanged")
        precondition(MetaHTTP.server.payload["props"] as? String == meta.props)
        precondition(MetaHTTP.server.payload["etag"] as? String == "meta-token")
        request.props = "rating: invalid"
        saved = await reader.saveMeta(request, expectedID: id)
        precondition(!saved && reader.saveError?.contains("number") == true)
        request.props = ""
        request.title = "Duplicate"
        saved = await reader.saveMeta(request, expectedID: id)
        precondition(!saved && reader.saveError?.contains("already in use") == true)
        request.title = "Note"
        saved = await reader.saveMeta(request, expectedID: id)
        precondition(saved && MetaHTTP.server.payload["props"] as? String == "")
        request.etag = "stale"
        for _ in 0..<2 {
            saved = await reader.saveMeta(request, expectedID: id)
            precondition(!saved && reader.saveError?.contains("Metadata changed") == true)
        }
        let count = MetaHTTP.server.count
        saved = await reader.saveMeta(request, expectedID: TrackID("other~42"))
        precondition(!saved && MetaHTTP.server.count == count, "A stale sheet must never write a different note")
        let target = AgentRequestTarget(note: try await client.getNote(id), id: id, quote: selection.text)
        precondition(target.quote == "selected" && target.vault == "work")
        print("Note action checks passed: native selections, Unicode, partial table cells, rich GFM copy, actual share URLs, typed props, validation, metadata etags and stale sheets.")
    }
}
