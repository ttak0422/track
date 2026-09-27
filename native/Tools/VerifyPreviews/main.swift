import AppKit
import Foundation
import TrackAPI
@testable import TrackUI

final class PreviewHTTP: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        let id = query.first { $0.name == "id" }?.value ?? query.first { $0.name == "term" }?.value ?? "1"
        DispatchQueue.global().asyncAfter(deadline: .now() + (id == "slow" ? 0.15 : 0.005)) { [self] in
            let payload: [String: Any]
            let title = "プレビューの本文"
            let body = "## 見出し\n\n本文を最後まで読めます。表や図も通常の本文と同じ描画です。\n\n| 項目 | 値 |\n| --- | --- |\n| 日本語 | 42 |\n\n" + (1...20).map { "段落 \($0)。プレビューを固定して内容を読み進めます。\n\n" }.joined()
            if url.path == "/api/resolve" {
                payload = ["found": id != "missing", "note": ["note_id": id, "file_kind": "note", "title": title]]
            } else if url.path == "/api/render" {
                payload = ["markdown": body]
            } else if url.path == "/api/graph/local" {
                payload = ["graph": ["center_id": id, "nodes": [["note_id": id, "file_kind": "note", "title": id]], "edges": []]]
            } else {
                payload = ["note": ["note_id": id, "file_kind": "note", "path": "test.md", "title": title, "body": body, "etag": "test"], "backlinks": [], "children": []]
            }
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: try! JSONSerialization.data(withJSONObject: payload))
            client?.urlProtocolDidFinishLoading(self)
        }
    }
    override func stopLoading() {}
}

@main
struct VerifyPreviews {
    @MainActor
    static func main() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [PreviewHTTP.self]
        let client = TrackClient(baseURL: URL(string: "http://preview.invalid")!, session: URLSession(configuration: configuration))
        let source = NoteReaderModel(client: client)
        await source.open(TrackID("original"))
        source.beginEditing()
        source.draftBody = "Unsaved source draft"
        let model = NotePreviewModel(client: client)
        let slow = Task { await model.load(id: TrackID("slow")) }
        try await Task.sleep(for: .milliseconds(20))
        await model.load(id: TrackID("2"))
        await slow.value
        precondition(model.reader.currentID == TrackID("2"), "Late preview replaced newer note")
        let cancelled = Task { await model.load(id: TrackID("slow")) }
        try await Task.sleep(for: .milliseconds(20))
        cancelled.cancel()
        await cancelled.value
        precondition(model.reader.currentID == TrackID("2"))
        await model.load(target: "Title##見出し", sourceID: TrackID("work~1"))
        precondition(model.reader.currentID == TrackID("work~Title"))
        precondition(model.reader.scrollTarget == "h-見出し")
        await model.load(target: "missing", sourceID: TrackID("work~1"))
        precondition(model.error != nil, "Unresolved preview must not show previous content")
        precondition(source.isDirty && source.isEditing && source.draftBody == "Unsaved source draft" && source.currentID == TrackID("original"))
        let graphModel = GraphModel(client: client)
        let oldGraph = Task { await graphModel.loadLocal(id: TrackID("slow")) }
        try await Task.sleep(for: .milliseconds(20))
        await graphModel.loadLocal(id: TrackID("2"))
        await oldGraph.value
        precondition(graphModel.local?.centerID == TrackID("2"))

        let nodes = try (0..<400).map { index in
            try JSONDecoder().decode(GraphNode.self, from: JSONSerialization.data(withJSONObject: ["note_id": "\(index)", "file_kind": "note", "title": "Note \(index)"]))
        }
        let edges = try (1..<399).map { index in
            try JSONDecoder().decode(GraphEdge.self, from: JSONSerialization.data(withJSONObject: ["source_id": "0", "target_id": "\(index)"]))
        }
        let slice = GraphModel.canvasSlice(nodes: nodes, edges: edges, centerID: TrackID("399"), selectedID: TrackID("398"))
        precondition(slice.nodes.count == 300 && slice.hidden == 100)
        precondition(slice.nodes.contains { $0.noteID == TrackID("399") })
        precondition(slice.nodes.contains { $0.noteID == TrackID("398") })
        precondition(GraphModel.noteID(for: nodes[1], vault: "work") == TrackID("work~1"))
        let ids = Set(slice.nodes.map(\.noteID))
        precondition(slice.edges.allSatisfy { ids.contains($0.sourceID) && ids.contains($0.targetID) })
        let positions = [TrackID("1"): CGPoint(x: 250, y: 80), TrackID("2"): CGPoint(x: 320, y: 130)]
        let radii: [TrackID: CGFloat] = [TrackID("1"): 8, TrackID("2"): 16]
        precondition(GraphModel.hitNode(at: CGPoint(x: 256, y: 85), positions: positions, radii: radii) == TrackID("1"))
        precondition(GraphModel.hitNode(at: CGPoint(x: 338, y: 130), positions: positions, radii: radii) == TrackID("2"))
        precondition(GraphModel.hitNode(at: .zero, positions: positions, radii: radii) == nil)

        NSApplication.shared.setActivationPolicy(.accessory)
        let windows = NotePreviewWindows()
        let fleeting = windows.show(client: client, id: TrackID("fleeting")) { _ in }
        windows.leave(fleeting)
        try await Task.sleep(for: .milliseconds(450))
        precondition(!NSApp.windows.contains { $0.title == "fleeting" && $0.isVisible }, "Hover intent did not cancel")
        windows.show(client: client, id: TrackID("snapshot"), pinned: true) { _ in }
        windows.show(client: client, id: TrackID("second"), pinned: true) { _ in }
        try await Task.sleep(for: .milliseconds(800))
        precondition(NSApp.windows.contains { $0.title == "second" && $0.isVisible }, "Opening a second pin must preserve the first")
        guard let panel = NSApp.windows.first(where: { $0.title == "snapshot" && $0.isVisible }) as? NSPanel else { preconditionFailure("Pinned preview did not open") }
        precondition(panel.contentView!.bounds.height > 300, "Preview collapsed to its toolbar")
        precondition(panel.styleMask.contains(.resizable) && panel.styleMask.contains(.closable))
        if let index = CommandLine.arguments.firstIndex(of: "--snapshot-dir"), CommandLine.arguments.count > index + 1 {
            let directory = URL(fileURLWithPath: CommandLine.arguments[index + 1])
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            for (name, size) in [("preview", NSSize(width: 460, height: 460)), ("preview-wide", NSSize(width: 680, height: 600))] {
                panel.setContentSize(size)
                try await Task.sleep(for: .milliseconds(300))
                let host = panel.contentView!
                host.layoutSubtreeIfNeeded()
                let rep = host.bitmapImageRepForCachingDisplay(in: host.bounds)!
                host.cacheDisplay(in: host.bounds, to: rep)
                try rep.representation(using: .png, properties: [:])!.write(to: directory.appendingPathComponent("\(name).png"))
            }
        }
        panel.cancelOperation(nil)
        NSApp.windows.first { $0.title == "second" }?.close()
        precondition(!panel.isVisible)
        print("Preview/graph checks passed: stale/cancelled loads, vault anchors, unresolved state, hover intent, native panel lifecycle, cap reachability and hit testing.")
    }
}
