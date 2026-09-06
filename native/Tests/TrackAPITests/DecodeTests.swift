import Foundation
import Testing
@testable import TrackAPI

// Fixture shapes were captured live from `track web` (docs/help vault,
// 2026-09-06): numeric ids, TaskListResponse `{tasks:[rows]}` for GET vs
// TasksResponse `{tasks:{items},etag}` for POST /api/task.

private func fixture(_ name: String) -> Data {
    let url = Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures")!
    return try! Data(contentsOf: url)
}

@Suite struct DecodeTests {
    @Test func searchDecodesNumericIDs() throws {
        let res = try JSONDecoder().decode(SearchResponse.self, from: fixture("search"))
        #expect(res.results.count == 2)
        // Server numbers become opaque strings (api.ts: stringifyIDs).
        #expect(res.results[0].ref.noteID == TrackID("1781359469000"))
        #expect(res.results[0].tags == ["A", "B", "tag"])
        #expect(res.results[0].match == "title")
        #expect(res.results[0].ref.seenAt == 1788361672)
        #expect(res.results[1].ref.icon == "🧭")
        #expect(res.results[0].qualifiedID == TrackID("main~1781359469000"))
        #expect(res.results[1].qualifiedID == TrackID("1785024008000"))
    }

    @Test func taskListDecodesRows() throws {
        let res = try JSONDecoder().decode(TaskListResponse.self, from: fixture("tasklist"))
        #expect(res.tasks.count == 2)
        let first = res.tasks[0]
        #expect(first.noteID == TrackID("1785024015000"))
        #expect(first.title == "Tasks")
        #expect(first.item.line == 173)
        #expect(first.item.state == "DOING")
        #expect(first.item.due == "2026-07-24")
        #expect(res.tasks[1].item.scheduled == "2026-07-18")
    }

    @Test func noteDecodesBodyAndBacklinks() throws {
        let res = try JSONDecoder().decode(NoteResponse.self, from: fixture("note"))
        #expect(res.note.summary.ref.title == "Tasks")
        #expect(res.note.body.contains("[[track]]"))
        #expect(res.note.etag == "abc123")
        #expect(res.backlinks.count == 1)
        #expect(res.backlinks[0].title == "track")
    }

    @Test func taskWriteDecodesItemsAndEtag() throws {
        let res = try JSONDecoder().decode(TasksResponse.self, from: fixture("taskwrite"))
        #expect(res.etag == "def456")
        #expect(res.items.count == 1)
        #expect(res.items[0].state == "DOING")
    }

    @Test func trackIDSplitting() {
        #expect(TrackID("1785").split().vault == "")
        let q = TrackID.qualify(vault: "blog", id: "1786")
        #expect(q.raw == "blog~1786")
        #expect(q.split() == ("1786", "blog"))
    }
}
