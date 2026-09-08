import SwiftUI
import TrackAPI

// Calendar for note activity, mirroring the web `/calendar` surface
// (docs/spec/web.md). The month grid and the selected day's agenda are both
// derived from the notes listing's activity `days` plus the vault-wide dated
// task listing — no dedicated endpoint (listNotes/listDatedTasks only).
// Days without activity are inert; journals carry no activity days, so only
// real notes are listed.

// MARK: - Calendar model

@MainActor
@Observable
public final class CalendarModel {
    public private(set) var notes: [SearchResult] = []
    public private(set) var tasks: [TaskRow] = []
    public private(set) var isLoading = false
    public private(set) var error: String?
    public var month: Date
    public var selectedDay: String?

    private let client: TrackClient

    public init(client: TrackClient, month: Date = Date()) {
        self.client = client
        self.month = month
    }

    public func reload() async {
        isLoading = true
        defer { isLoading = false }
        error = nil
        do {
            async let nr = client.listNotes()
            async let tr = client.listDatedTasks()
            let (notesRes, tasksRes) = try await (nr, tr)
            notes = notesRes.notes
            tasks = tasksRes.tasks
        } catch {
            self.error = error.localizedDescription
        }
    }

    public func shiftMonth(by months: Int) {
        guard let shifted = Calendar.current.date(byAdding: .month, value: months, to: month) else { return }
        month = shifted
    }

    /// Notes active on `day` (YYYY-MM-DD): those whose `days` contain it.
    public func notes(on day: String) -> [SearchResult] {
        notes.filter { $0.days?.contains(day) == true }
    }

    /// Tasks due or scheduled on `day`.
    public func tasks(on day: String) -> [TaskRow] {
        tasks.filter { $0.item.due == day || $0.item.scheduled == day }
    }

    /// Deadline tasks (due == day) — the `[!]` count drawn on a cell.
    public func deadlineCount(on day: String) -> Int {
        tasks.filter { $0.item.due == day }.count
    }

    // MARK: - Grid helpers

    public var monthStart: Date {
        Calendar.current.dateInterval(of: .month, for: month)?.start ?? month
    }

    /// Day dates covering the shown month padded to complete weeks with
    /// adjacent-month days, so the grid always draws full rows.
    public var gridDays: [Date] {
        let cal = Calendar.current
        let start = monthStart
        let weekday = cal.component(.weekday, from: start)
        let leading = weekday - cal.firstWeekday
        let padStart = cal.date(byAdding: .day, value: -leading, to: start) ?? start
        let daysInMonth = cal.range(of: .day, in: .month, for: month)?.count ?? 30
        let total = Int(ceil(Double(leading + daysInMonth) / 7.0)) * 7
        return (0..<total).compactMap { cal.date(byAdding: .day, value: $0, to: padStart) }
    }

    public static func dayString(_ date: Date) -> String {
        formatter.string(from: date)
    }

    public static func isSameMonth(_ date: Date, as month: Date) -> Bool {
        Calendar.current.isDate(date, equalTo: month, toGranularity: .month)
    }

    public static var weekdaySymbols: [String] {
        let cal = Calendar.current
        let symbols = cal.veryShortStandaloneWeekdaySymbols
        let start = cal.firstWeekday - 1
        return Array(symbols[start...] + symbols[..<start])
    }

    private static let formatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f
    }()
}

// MARK: - Calendar view

public struct CalendarView: View {
    @State private var model: CalendarModel

    private let client: TrackClient
    private let columns = Array(repeating: GridItem(.flexible(), spacing: 4), count: 7)

    public init(client: TrackClient) {
        self.client = client
        _model = State(initialValue: CalendarModel(client: client))
    }

    public var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            Group {
                if model.isLoading && model.notes.isEmpty && model.tasks.isEmpty {
                    ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if let error = model.error, model.notes.isEmpty {
                    ContentUnavailableView("Could not load calendar", systemImage: "exclamationmark.triangle", description: Text(error))
                } else {
                    monthGrid
                }
            }
            Divider()
            agenda
        }
        .task { await model.reload() }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            Task { await model.reload() }
        }
    }

    private var header: some View {
        HStack {
            Button { model.shiftMonth(by: -1) } label: { Image(systemName: "chevron.left") }
                .buttonStyle(.plain)
            Spacer()
            Text(monthTitle).font(.headline)
            Spacer()
            Button { model.shiftMonth(by: 1) } label: { Image(systemName: "chevron.right") }
                .buttonStyle(.plain)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private var monthTitle: String {
        let f = DateFormatter()
        f.dateFormat = "yyyy / MM"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.string(from: model.month)
    }

    private var monthGrid: some View {
        ScrollView {
            VStack(spacing: 4) {
                LazyVGrid(columns: columns, spacing: 4) {
                    ForEach(CalendarModel.weekdaySymbols, id: \.self) { symbol in
                        Text(symbol).font(.caption2).foregroundStyle(.secondary)
                    }
                }
                LazyVGrid(columns: columns, spacing: 4) {
                    ForEach(model.gridDays, id: \.self) { day in
                        DayCell(
                            day: day,
                            inMonth: CalendarModel.isSameMonth(day, as: model.month),
                            noteCount: model.notes(on: CalendarModel.dayString(day)).count,
                            deadlineCount: model.deadlineCount(on: CalendarModel.dayString(day)),
                            isSelected: model.selectedDay == CalendarModel.dayString(day),
                            onSelect: { model.selectedDay = CalendarModel.dayString(day) }
                        )
                    }
                }
            }
            .padding(8)
        }
    }

    private var agenda: some View {
        Group {
            if let day = model.selectedDay {
                AgendaView(day: day, notes: model.notes(on: day), tasks: model.tasks(on: day), client: client)
            } else {
                Text("Select a day")
                    .font(.caption).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(8)
            }
        }
        .frame(minHeight: 120)
    }
}

// MARK: - Subviews

private struct DayCell: View {
    let day: Date
    let inMonth: Bool
    let noteCount: Int
    let deadlineCount: Int
    let isSelected: Bool
    let onSelect: () -> Void

    var body: some View {
        Button(action: onSelect) {
            VStack(alignment: .leading, spacing: 2) {
                Text("\(Calendar.current.component(.day, from: day))")
                    .font(.caption)
                    .foregroundStyle(inMonth ? Color.primary : Color.secondary)
                HStack(spacing: 2) {
                    ForEach(0..<min(noteCount, 3), id: \.self) { _ in
                        Circle().fill(Color.accentColor).frame(width: 4, height: 4)
                    }
                }
                if deadlineCount > 0 {
                    Text("[!] \(deadlineCount)").font(.caption2).foregroundStyle(.red)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(6)
            .background(isSelected ? Color.accentColor.opacity(0.15) : Color.clear,
                        in: RoundedRectangle(cornerRadius: 6))
            .overlay(RoundedRectangle(cornerRadius: 6)
                .strokeBorder(isSelected ? Color.accentColor : Color.clear, lineWidth: 1))
        }
        .buttonStyle(.plain)
    }
}

private struct AgendaView: View {
    let day: String
    let notes: [SearchResult]
    let tasks: [TaskRow]
    let client: TrackClient

    @State private var journal: JournalPreview?
    @State private var journalError: String?
    @State private var isLoadingJournal = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(day).font(.caption).foregroundStyle(.secondary)
            if notes.isEmpty && tasks.isEmpty {
                Text("Nothing this day").font(.caption).foregroundStyle(.tertiary)
            }
            if !notes.isEmpty {
                Text("Notes").font(.caption2).foregroundStyle(.secondary)
                ForEach(notes, id: \.ref.noteID) { note in
                    Text(note.ref.title).font(.body)
                }
            }
            if !tasks.isEmpty {
                Text("Tasks").font(.caption2).foregroundStyle(.secondary)
                ForEach(tasks, id: \.self) { task in
                    HStack(spacing: 6) {
                        Text(task.item.done ? "✓" : "○")
                            .foregroundStyle(task.item.done ? .secondary : .primary)
                        Text(task.item.text)
                        if let due = task.item.due {
                            Text("! \(due)").font(.caption).foregroundStyle(.secondary)
                        }
                        if let sched = task.item.scheduled {
                            Text("▷ \(sched)").font(.caption).foregroundStyle(.tertiary)
                        }
                        Text(task.title).font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            Divider()
            journalPreview
            Button("Open journal") { openJournal() }
                .buttonStyle(.plain)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The day's journal once "Open journal" resolved it: its title and the
    /// first lines of its body, labelled by whether it was opened or created.
    @ViewBuilder
    private var journalPreview: some View {
        if isLoadingJournal {
            ProgressView().controlSize(.small)
        } else if let error = journalError {
            Text(error).font(.caption).foregroundStyle(.red)
        } else if let journal {
            VStack(alignment: .leading, spacing: 2) {
                Text(journal.created ? "Created journal" : "Opened journal")
                    .font(.caption2).foregroundStyle(.secondary)
                Text(journal.title).font(.body).fontWeight(.medium)
                ForEach(journal.lines, id: \.self) { line in
                    Text(line).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    private func openJournal() {
        isLoadingJournal = true
        journalError = nil
        Task {
            do {
                let res = try await client.openJournal(date: day)
                let note = try await client.getNote(res.noteID)
                journal = JournalPreview(
                    title: note.note.summary.ref.title,
                    lines: Self.previewLines(from: note.note.body),
                    created: res.created
                )
            } catch {
                journalError = error.localizedDescription
            }
            isLoadingJournal = false
        }
    }

    private static func previewLines(from body: String) -> [String] {
        body.split(separator: "\n", omittingEmptySubsequences: true)
            .prefix(4)
            .map(String.init)
    }
}

private struct JournalPreview {
    let title: String
    let lines: [String]
    let created: Bool
}
