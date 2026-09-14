import Foundation
import TrackAPI
import TrackUI

private final class CalendarHTTP: URLProtocol, @unchecked Sendable {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var responses: [String: (Int, Data)] = [:]
    static func set(notes: [[String: Any]], tasks: [[String: Any]], failNotes: Bool = false, failTasks: Bool = false) throws {
        let next = [
            "/api/notes": (failNotes ? 503 : 200, try JSONSerialization.data(withJSONObject: ["notes": notes])),
            "/api/tasks": (failTasks ? 503 : 200, try JSONSerialization.data(withJSONObject: ["tasks": tasks])),
        ]
        lock.withLock { responses = next }
    }
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}
    override func startLoading() {
        let (status, data) = Self.lock.withLock { Self.responses[request.url!.path]! }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }
}

private func note(_ id: String, title: String? = nil, kind: String = "note", days: [String]? = nil) -> [String: Any] {
    var value: [String: Any] = ["note_id": id, "file_kind": kind, "title": title ?? id]
    if let days { value["days"] = days }
    return value
}

private func task(_ line: Int, due: String? = nil, scheduled: String? = nil, done: Bool = false) -> [String: Any] {
    var value: [String: Any] = ["note_id": "1", "file_kind": "note", "title": "Note", "line": line,
        "state": done ? "DONE" : "TODO", "done": done, "text": "Task \(line)"]
    if let due { value["due"] = due }
    if let scheduled { value["scheduled"] = scheduled }
    return value
}

@MainActor
private func calendarClient() -> TrackClient {
    let config = URLSessionConfiguration.ephemeral
    config.protocolClasses = [CalendarHTTP.self]
    return TrackClient(baseURL: URL(string: "http://calendar.test")!, session: URLSession(configuration: config))
}

private func same<T: Encodable>(_ left: T, _ right: T) -> Bool {
    let encoder = JSONEncoder()
    encoder.outputFormatting = .sortedKeys
    return try! encoder.encode(left) == encoder.encode(right)
}

@MainActor
private let calendarFormatter: DateFormatter = {
    let formatter = DateFormatter()
    formatter.dateFormat = "yyyy-MM-dd"
    formatter.locale = Locale(identifier: "en_US_POSIX")
    return formatter
}()

// Preserve the old full-scan behavior as an independent oracle and benchmark.
@MainActor
private struct ScanCalendar {
    let model: CalendarModel
    @inline(never) func notes(_ day: String) -> [SearchResult] { model.notes.filter { $0.days?.contains(day) == true } }
    @inline(never) func tasks(_ day: String) -> [TaskRow] { model.tasks.filter { $0.item.due == day || $0.item.scheduled == day } }
    func journal(_ day: String) -> SearchResult? {
        model.notes.first { $0.ref.fileKind == "journal" && $0.ref.title == day.replacingOccurrences(of: "-", with: "") }
    }
    @inline(never) func deadlineCount(_ day: String) -> Int { model.tasks.filter { $0.item.due == day }.count }
    @inline(never) func overdueCount(_ day: String, today: String) -> Int {
        model.tasks.filter { row in
            guard let due = row.item.due else { return false }
            return !row.item.done && due < today && due == day
        }.count
    }
    @inline(never) func dueFill(_ day: String, today: String) -> (Double, Bool) {
        let dated = model.tasks.compactMap { row -> (String, Bool)? in
            guard let due = row.item.due, !row.item.done, due == day else { return nil }
            return (due, due < today)
        }
        guard !dated.isEmpty else { return (0, false) }
        if dated.contains(where: { $0.1 }) { return (1, true) }
        let remaining: Int
        if let a = calendarFormatter.date(from: today), let b = calendarFormatter.date(from: day) {
            remaining = max(0, Calendar.current.dateComponents([.day], from: a, to: b).day ?? 0)
        } else { remaining = 0 }
        return (max(0, min(1, 1 - Double(remaining) / 14)), false)
    }
}

@MainActor
func verifyCalendarIndex() async throws {
    let day = "2026-09-15", next = "2026-09-16", future = "2026-09-22"
    let model = CalendarModel(client: calendarClient(), month: calendarFormatter.date(from: day)!)
    let notes = [note("work~1", days: [day, day, next]), note("plain"), note("personal~1", days: [next, day]),
                 note("empty", days: []), note("journal-first", title: "20260915", kind: "journal"),
                 note("journal-second", title: "20260915", kind: "journal"),
                 note("month-first", title: "202609", kind: "journal"), note("month-second", title: "202609", kind: "journal")]
    let tasks = [task(1, due: day, scheduled: day), task(2, scheduled: day), task(3, due: day, scheduled: next, done: true),
                 task(4, due: future), task(5), task(6, due: "", scheduled: ""), task(7, due: next)]
    try CalendarHTTP.set(notes: notes, tasks: tasks)
    await model.reload()
    precondition(model.error == nil)
    let scan = ScanCalendar(model: model)
    for key in [day, next, future, "2026-09-01", ""] {
        precondition(same(model.notes(on: key), scan.notes(key)))
        precondition(same(model.tasks(on: key), scan.tasks(key)))
        precondition(same(model.journal(on: key), scan.journal(key)))
        precondition(model.noteTitles(on: key) == scan.notes(key).map { $0.ref.title })
        precondition(model.taskTexts(on: key) == scan.tasks(key).map { $0.item.text })
        precondition(model.deadlineCount(on: key) == scan.deadlineCount(key))
        // Advance the clock without reloading: cached membership is stable,
        // while overdue and the future due-bar change as before.
        for today in ["2026-09-14", day, next, "2026-09-30"] {
            precondition(model.overdueCount(on: key, today: today) == scan.overdueCount(key, today: today))
            let current = model.dueFill(on: key, today: today), expected = scan.dueFill(key, today: today)
            precondition(current.fill == expected.0 && current.overdue == expected.1)
        }
    }
    precondition(model.notes(on: day).map { $0.ref.noteID.raw } == ["work~1", "personal~1"])
    precondition(model.tasks(on: day).map { $0.item.line } == [1, 2, 3])
    precondition(model.journal(on: day)?.ref.noteID.raw == "journal-first")
    precondition(model.monthlyJournal()?.ref.noteID.raw == "month-first")
    precondition(model.overdueCount(on: day, today: day) == 0 && model.overdueCount(on: day, today: next) == 1)
    precondition(model.dueFill(on: future, today: day).fill == 0.5)
    let actualToday = CalendarModel.dayString(Date())
    precondition(model.overdueCount(on: day) == scan.overdueCount(day, today: actualToday))
    precondition(model.dueFill(on: future).fill == scan.dueFill(future, today: actualToday).0)

    try CalendarHTTP.set(notes: [note("replacement", days: [next])], tasks: [task(8, due: next, done: true)])
    await model.reload()
    precondition(model.notes(on: day).isEmpty && model.tasks(on: day).isEmpty && model.journal(on: day) == nil && model.monthlyJournal() == nil)
    precondition(model.notes(on: next).map { $0.ref.noteID.raw } == ["replacement"] && model.tasks(on: next).map { $0.item.line } == [8])
    precondition(model.deadlineCount(on: next) == 1 && model.dueFill(on: next, today: "2026-09-30").fill == 0)
    try CalendarHTTP.set(notes: [note("kept", days: [next])], tasks: [], failTasks: true)
    await model.reload()
    precondition(model.error == nil && model.notes(on: next).count == 1 && model.tasks(on: next).isEmpty && model.deadlineCount(on: next) == 0)
    try CalendarHTTP.set(notes: [], tasks: [task(9, due: future)], failNotes: true)
    await model.reload()
    precondition(model.error != nil && model.notes(on: next).isEmpty && model.tasks(on: future).count == 1)
    try CalendarHTTP.set(notes: [], tasks: [], failNotes: true, failTasks: true)
    await model.reload()
    precondition(model.error != nil && model.notes.isEmpty && model.tasks.isEmpty)
    precondition(model.notes(on: next).isEmpty && model.tasks(on: next).isEmpty && model.deadlineCount(on: next) == 0)
    print("Calendar checks passed: scan equivalence, ordering, duplicate dates, journal precedence, reload replacement, failed reload and midnight urgency")
}

private struct CalendarCell: Equatable {
    let noteTitles: [String], taskTexts: [String]
    let notes: Int, tasks: Int, deadlines: Int, overdue: Int
    let fill: Double, isOverdue: Bool, active: Bool
    var checksum: Int { noteTitles.count + taskTexts.count + notes + tasks + deadlines + overdue + (active ? 1 : 0) }
}

@MainActor
private func cell(_ model: CalendarModel, day: Date, today: String, scanning: Bool) -> CalendarCell {
    let key = CalendarModel.dayString(day)
    if scanning {
        let scan = ScanCalendar(model: model)
        let titles = Array(scan.notes(CalendarModel.dayString(day)).map { $0.ref.title }.prefix(3))
        let texts = Array(scan.tasks(CalendarModel.dayString(day)).map { $0.item.text }.prefix(3))
        let count = scan.notes(CalendarModel.dayString(day)).count
        let tasks = scan.tasks(CalendarModel.dayString(day)).count
        let deadlines = scan.deadlineCount(CalendarModel.dayString(day))
        let overdue = scan.overdueCount(CalendarModel.dayString(day), today: today)
        let fill = scan.dueFill(CalendarModel.dayString(day), today: today)
        let active = !scan.notes(CalendarModel.dayString(day)).isEmpty || !scan.tasks(CalendarModel.dayString(day)).isEmpty || scan.journal(CalendarModel.dayString(day)) != nil
        return CalendarCell(noteTitles: titles, taskTexts: texts, notes: count, tasks: tasks, deadlines: deadlines, overdue: overdue, fill: fill.0, isOverdue: fill.1, active: active)
    }
    let notes = model.notes(on: key), tasks = model.tasks(on: key), fill = model.dueFill(on: key, today: today)
    return CalendarCell(noteTitles: notes.prefix(3).map { $0.ref.title }, taskTexts: tasks.prefix(3).map { $0.item.text },
        notes: notes.count, tasks: tasks.count, deadlines: model.deadlineCount(on: key), overdue: model.overdueCount(on: key, today: today),
        fill: fill.fill, isOverdue: fill.overdue, active: !notes.isEmpty || !tasks.isEmpty || model.journal(on: key) != nil)
}

@MainActor
func benchmarkCalendarIndex() async throws {
    print("Calendar benchmark: today=2026-09-15 zone=\(TimeZone.current.identifier) warmups=5 samples=31")
    let epoch = calendarFormatter.date(from: "2026-01-01")!
    let days = (0..<365).map { CalendarModel.dayString(Calendar.current.date(byAdding: .day, value: $0, to: epoch)!) }
    let notes: [[String: Any]] = (0..<3598).map { i in
        let count = i < 1476 ? 3 : 2
        let activeDays: [String] = (0..<count).map { offset in days[(i * 17 + offset * 97) % days.count] }
        return note(String(i), days: activeDays)
    }
    var checksum = 0
    for taskCount in [0, 512] {
        let tasks: [[String: Any]] = (0..<taskCount).map { i in
            task(i + 1, due: days[(i * 11) % 365], scheduled: days[(i * 7) % 365], done: i % 3 == 0)
        }
        try CalendarHTTP.set(notes: notes, tasks: tasks)
        let model = CalendarModel(client: calendarClient(), month: calendarFormatter.date(from: "2026-08-15")!)
        await model.reload()
        precondition(model.notes.count == 3598 && model.notes.reduce(0) { $0 + ($1.days?.count ?? 0) } == 8672 && model.tasks.count == taskCount)
        let dates = model.gridDays, today = "2026-09-15"
        precondition(dates.count == 42)
        precondition(dates.map { cell(model, day: $0, today: today, scanning: true) } == dates.map { cell(model, day: $0, today: today, scanning: false) })
        for scanning in [true, false] {
            var samples: [Double] = []
            for iteration in 0..<36 {
                let start = DispatchTime.now().uptimeNanoseconds
                checksum &+= dates.reduce(0) { $0 + cell(model, day: $1, today: today, scanning: scanning).checksum }
                if iteration >= 5 { samples.append(Double(DispatchTime.now().uptimeNanoseconds - start) / 1_000_000) }
            }
            samples.sort()
            print(String(format: "calendar cells=42 notes=3598 activity_days=8672 tasks=%d method=%@ median_ms=%.3f p95_ms=%.3f", taskCount, scanning ? "scan" : "index", samples[15], samples[29]))
        }
    }
    print("calendar benchmark checksum=\(checksum); synthetic data, not app drag latency")
}
