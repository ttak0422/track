import Foundation
import TrackAPI

// Fixture decoder checks for TrackAPI. Fixture shapes were captured live from
// `track web` (docs/help vault, 2026-09-06): numeric ids, TaskListResponse
// `{tasks:[rows]}` for GET vs TasksResponse `{tasks:{items},etag}` for POST.
//
// swift-testing / XCTest are absent from the Command Line Tools toolchain, so
// this is a plain executable (`swift run VerifyFixtures`) instead of a test
// target. Any failure prints to stderr and exits nonzero.

var failures = 0

@MainActor
func check(_ cond: Bool, _ label: String) {
    if cond {
        print("ok - \(label)")
    } else {
        fputs("FAIL - \(label)\n", stderr)
        failures += 1
    }
}

func fixture(_ name: String) throws -> Data {
    guard let url = Bundle.module.url(forResource: name, withExtension: "json") else {
        throw NSError(domain: "VerifyFixtures", code: 1, userInfo: [NSLocalizedDescriptionKey: "missing fixture \(name)"])
    }
    return try Data(contentsOf: url)
}

do {
    let search = try JSONDecoder().decode(SearchResponse.self, from: fixture("search"))
    check(search.results.count == 2, "search: 2 results")
    check(search.results[0].ref.noteID == TrackID("1781359469000"), "search: numeric id becomes opaque string")
    check(search.results[0].tags == ["A", "B", "tag"], "search: tags")
    check(search.results[0].match == "title", "search: match kind")
    check(search.results[0].ref.seenAt == 1788361672, "search: seen_at")
    check(search.results[1].icon == "🧭", "search: icon")
    check(search.results[0].qualifiedID == TrackID("main~1781359469000"), "search: vault-qualified id")
    check(search.results[1].qualifiedID == TrackID("1785024008000"), "search: bare id stays bare")

    let list = try JSONDecoder().decode(TaskListResponse.self, from: fixture("tasklist"))
    check(list.tasks.count == 2, "tasklist: 2 rows")
    check(list.tasks[0].noteID == TrackID("1785024015000"), "tasklist: row note")
    check(list.tasks[0].title == "Tasks", "tasklist: row title")
    check(list.tasks[0].item.line == 173, "tasklist: 1-based line")
    check(list.tasks[0].item.state == "DOING", "tasklist: state")
    check(list.tasks[0].item.due == "2026-07-24", "tasklist: due")
    check(list.tasks[1].item.scheduled == "2026-07-18", "tasklist: scheduled")

    let note = try JSONDecoder().decode(NoteResponse.self, from: fixture("note"))
    check(note.note.summary.ref.title == "Tasks", "note: title")
    check(note.note.body.contains("[[track]]"), "note: raw GFM body")
    check(note.note.etag == "abc123", "note: etag")
    check(note.backlinks.count == 1 && note.backlinks[0].title == "track", "note: backlinks")

    let written = try JSONDecoder().decode(TasksResponse.self, from: fixture("taskwrite"))
    check(written.etag == "def456", "taskwrite: etag")
    check(written.items.count == 1 && written.items[0].state == "DOING", "taskwrite: refreshed items")

    let dashboard = try JSONDecoder().decode(MetricsDashboardResponse.self, from: fixture("metrics-dashboard"))
    check(dashboard.entities == ["api-a", "api-b"] && dashboard.panels.count == 4 && dashboard.metrics == ["latency", "requests"], "dashboard: generic targets and panels")
    check(dashboard.panels[0].values[0].display == "120 ms" && dashboard.panels[0].values[0].threshold == 100 && dashboard.panels[0].values[0].state == "Threshold ≥ 100 ms", "dashboard: formatted value and threshold")
    check(dashboard.panels[1].grid.x == 8 && dashboard.panels[1].grid.w == 16, "dashboard: grid placement preserved")
    let option = try JSONSerialization.jsonObject(with: Data(dashboard.panels[1].echartsJSON!.utf8)) as! [String: Any]
    check((option["series"] as? [[String: Any]])?.count == 1, "dashboard: ECharts object preserved")
    check(dashboard.panels[1].values[0].value == 0 && dashboard.panels[1].values[0].state == "Baseline", "dashboard: real zero remains data")
    check(dashboard.panels[2].error == "data file missing" && dashboard.panels[3].values.isEmpty && dashboard.panels[3].echartsJSON == nil, "dashboard: errors and empty data stay explicit")

    check(TrackID("1785").split().vault == "", "id: bare has no vault")
    let q = TrackID.qualify(vault: "blog", id: "1786")
    check(q.raw == "blog~1786", "id: qualify uses ~")
    let (bare, vault) = q.split()
    check(bare == "1786" && vault == "blog", "id: split round-trips")
} catch {
    fputs("ERROR - \(error)\n", stderr)
    failures += 1
}

if failures > 0 {
    fputs("\(failures) check(s) failed\n", stderr)
    exit(1)
}
print("all checks passed")
