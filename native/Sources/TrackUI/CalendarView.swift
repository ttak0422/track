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
        async let nr = client.listNotes()
        async let tr = client.listDatedTasks()
        do {
            let notesRes = try await nr
            notes = notesRes.notes
        } catch {
            self.error = error.localizedDescription
            notes = []
        }
        // Tasks are supplementary data: a failed task query is an empty list.
        tasks = (try? await tr)?.tasks ?? []
    }

    public func shiftMonth(by months: Int) {
        guard let shifted = Calendar.current.date(byAdding: .month, value: months, to: month) else { return }
        month = shifted
    }

    /// Notes active on `day` (YYYY-MM-DD): those whose `days` contain it.
    public func notes(on day: String) -> [SearchResult] {
        notes.filter { $0.days?.contains(day) == true }
    }

    public func journal(on day: String) -> SearchResult? {
        notes.first { $0.ref.fileKind == "journal" && $0.ref.title == day.replacingOccurrences(of: "-", with: "") }
    }

    /// Tasks due or scheduled on `day`.
    public func tasks(on day: String) -> [TaskRow] {
        tasks.filter { $0.item.due == day || $0.item.scheduled == day }
    }

    /// Deadline tasks (due == day) — the `[!]` count drawn on a cell.
    public func deadlineCount(on day: String) -> Int {
        tasks.filter { $0.item.due == day }.count
    }

    /// Open deadlines that have already passed. Completed tasks do not keep a
    /// day looking urgent, while the total deadline count remains available as
    /// the cell's `[!] N` summary.
    public func overdueCount(on day: String) -> Int {
        let today = Self.dayString(Date())
        return tasks.filter { row in
            guard let due = row.item.due else { return false }
            return !row.item.done && due < today && due == day
        }.count
    }

    /// The strongest deadline on a day, expressed as a small due-bar fill.
    /// This deliberately mirrors the web's two-week urgency window without
    /// making the compact native cell show individual task rows.
    public func dueFill(on day: String) -> (fill: Double, overdue: Bool) {
        let today = Self.dayString(Date())
        let dated = tasks.compactMap { row -> (String, Bool)? in
            guard let due = row.item.due, !row.item.done, due == day else { return nil }
            return (due, due < today)
        }
        guard !dated.isEmpty else { return (0, false) }
        if dated.contains(where: { $0.1 }) { return (1, true) }
        let remaining = max(0, Self.daysBetween(today, day))
        return (max(0, min(1, 1 - Double(remaining) / 14)), false)
    }

    public func taskTexts(on day: String) -> [String] { tasks(on: day).map { $0.item.text } }
    public func noteTitles(on day: String) -> [String] { notes(on: day).map { $0.ref.title } }

    /// The existing notes listing already contains month summary journals.
    public func monthlyJournal() -> SearchResult? {
        let key = Self.monthKey(month)
        return notes.first { $0.ref.fileKind == "journal" && $0.ref.title == key }
    }

    // MARK: - Grid helpers

    public var monthStart: Date {
        Calendar.current.dateInterval(of: .month, for: month)?.start ?? month
    }

    /// Day dates covering the shown month padded to complete weeks with
    /// adjacent-month days, so the grid always draws full rows. Weeks start on
    /// Sunday, matching the web calendar (`WEEKDAYS Sun..Sat`,
    /// `leadingBlanks = month.getDay()`).
    public var gridDays: [Date] {
        let cal = Calendar.current
        let start = monthStart
        let weekday = cal.component(.weekday, from: start)
        let leading = weekday - 1
        let padStart = cal.date(byAdding: .day, value: -leading, to: start) ?? start
        let daysInMonth = cal.range(of: .day, in: .month, for: month)?.count ?? 30
        let total = Int(ceil(Double(leading + daysInMonth) / 7.0)) * 7
        return (0..<total).compactMap { cal.date(byAdding: .day, value: $0, to: padStart) }
    }

    public static func dayString(_ date: Date) -> String {
        formatter.string(from: date)
    }

    public static func monthKey(_ date: Date) -> String {
        monthFormatter.string(from: date)
    }

    private static func daysBetween(_ first: String, _ second: String) -> Int {
        guard let a = formatter.date(from: first), let b = formatter.date(from: second) else { return 0 }
        return Calendar.current.dateComponents([.day], from: a, to: b).day ?? 0
    }

    public static func isSameMonth(_ date: Date, as month: Date) -> Bool {
        Calendar.current.isDate(date, equalTo: month, toGranularity: .month)
    }

    /// Sunday-first weekday symbols, matching the web calendar's fixed
    /// `WEEKDAYS = ["Sun", …]` rather than the device locale's first weekday.
    public static var weekdaySymbols: [String] {
        Array(Calendar.current.veryShortStandaloneWeekdaySymbols.prefix(7))
    }

    private static let formatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f
    }()

    private static let monthFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyyMM"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f
    }()
}

// MARK: - Calendar view

public struct CalendarView: View {
    @State private var model: CalendarModel
    @State private var monthlyJournal: JournalPreview?
    @State private var monthlyJournalError: String?
    @State private var dayJumpText = ""
    @State private var isLoadingMonthlyJournal = false
    @State private var openedNoteID: TrackID?
    @State private var isShowingNote = false

    private let client: TrackClient
    private let columns = Array(repeating: GridItem(.flexible(), spacing: 4), count: 7)
    /// A day handed in from outside (Browse activity heatmap): applied to the
    /// model's selection on appear and on change, so a heatmap tap lands on
    /// the same day the calendar would have selected by hand.
    private let initialDay: String?

    public init(client: TrackClient, initialDay: String? = nil) {
        self.client = client
        self.initialDay = initialDay
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
        .task {
            await model.reload()
            if let initialDay { model.selectedDay = initialDay }
        }
        .onChange(of: initialDay) { _, newDay in
            if let newDay { model.selectedDay = newDay }
        }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            Task { await model.reload() }
        }
        .sheet(isPresented: $isShowingNote) {
            if let openedNoteID { NotePreviewView(client: client, noteID: openedNoteID) }
        }
    }

    private var header: some View {
        HStack {
            Button { model.shiftMonth(by: -1) } label: { Image(systemName: "chevron.left") }
                .buttonStyle(.plain)
            Spacer()
            if model.monthlyJournal() != nil {
                Button(monthTitle) { openMonthlyJournal() }
                    .buttonStyle(.plain)
                    .font(.headline)
                    .help("Open monthly journal")
            } else {
                Text(monthTitle).font(.headline)
            }
            Spacer()
            TextField("YYYY-MM-DD", text: $dayJumpText)
                .textFieldStyle(.roundedBorder)
                .frame(width: 110)
                .font(.caption)
                .onSubmit(jumpToDay)
                .help("Open day (web /day/$date)")
            Button("Today") { model.month = Calendar.current.startOfDay(for: Date()) }
                .buttonStyle(.plain)
                .font(.caption)
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
                            taskTexts: Array(model.taskTexts(on: CalendarModel.dayString(day)).prefix(3)),
                            taskCount: model.tasks(on: CalendarModel.dayString(day)).count,
                            noteTitles: Array(model.noteTitles(on: CalendarModel.dayString(day)).prefix(3)),
                            noteCount: model.notes(on: CalendarModel.dayString(day)).count,
                            deadlineCount: model.deadlineCount(on: CalendarModel.dayString(day)),
                            overdueCount: model.overdueCount(on: CalendarModel.dayString(day)),
                            dueFill: model.dueFill(on: CalendarModel.dayString(day)),
                            isToday: Calendar.current.isDateInToday(day),
                            isSelected: model.selectedDay == CalendarModel.dayString(day),
                            isActive: !model.notes(on: CalendarModel.dayString(day)).isEmpty
                                || !model.tasks(on: CalendarModel.dayString(day)).isEmpty
                                || model.journal(on: CalendarModel.dayString(day)) != nil,
                            onSelect: { select(day) }
                        )
                    }
                }
            }
            .padding(8)
        }
    }

    private func select(_ day: Date) {
        let key = CalendarModel.dayString(day)
        guard !model.notes(on: key).isEmpty || !model.tasks(on: key).isEmpty || model.journal(on: key) != nil else {
            return
        }
        if let journal = model.journal(on: key) {
            openedNoteID = journal.ref.noteID
            isShowingNote = true
        } else {
            model.selectedDay = key
        }
    }

    /// Day jump (web `/day/$date` parity): typing a YYYY-MM-DD date moves the
    /// month grid and selects the day's agenda, even when the day has no notes
    /// or tasks yet (so its journal can be opened from there).
    private func jumpToDay() {
        let key = dayJumpText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard key.range(of: #"^\d{4}-\d{2}-\d{2}$"#, options: .regularExpression) != nil else { return }
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        if let date = f.date(from: key) { model.month = date }
        model.selectedDay = key
        dayJumpText = ""
    }

    private var agenda: some View {
        Group {
            if let day = model.selectedDay {
                DayView(day: day, notes: model.notes(on: day), tasks: model.tasks(on: day), client: client)
            } else {
                Text("Select a day")
                    .font(.caption).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(8)
            }
            if isLoadingMonthlyJournal {
                ProgressView().controlSize(.small)
            } else if let monthlyJournalError {
                Text(monthlyJournalError).font(.caption).foregroundStyle(.red)
            } else if let monthlyJournal {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Opened monthly journal").font(.caption2).foregroundStyle(.secondary)
                    Text(monthlyJournal.title).font(.body).fontWeight(.medium)
                    ForEach(monthlyJournal.lines, id: \.self) { Text($0).font(.caption).foregroundStyle(.secondary) }
                }
            }
        }
        .frame(minHeight: 120)
    }

    private func openMonthlyJournal() {
        guard let result = model.monthlyJournal() else { return }
        isLoadingMonthlyJournal = true
        monthlyJournalError = nil
        Task {
            do {
                let note = try await client.getNote(result.ref.noteID)
                monthlyJournal = JournalPreview(
                    title: note.note.summary.ref.title,
                    lines: Self.previewLines(from: note.note.body),
                    created: false
                )
            } catch {
                monthlyJournalError = error.localizedDescription
            }
            isLoadingMonthlyJournal = false
        }
    }

    private static func previewLines(from body: String) -> [String] {
        body.split(separator: "\n", omittingEmptySubsequences: true).prefix(4).map(String.init)
    }
}

// MARK: - Day view

/// The dedicated day screen (web `/day/$date` parity): the notes active on
/// the day plus the dated tasks on it, with the day's journal reachable
/// through the same agenda surface the calendar embeds below. An invalid date
/// reports like the web's "Invalid date" instead of an empty agenda.
public struct DayView: View {
    let day: String
    let notes: [SearchResult]
    let tasks: [TaskRow]
    let client: TrackClient

    public init(day: String, notes: [SearchResult], tasks: [TaskRow], client: TrackClient) {
        self.day = day
        self.notes = notes
        self.tasks = tasks
        self.client = client
    }

    private var isValid: Bool {
        day.range(of: #"^\d{4}-\d{2}-\d{2}$"#, options: .regularExpression) != nil
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(day)
                .font(.headline)
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
            Divider()
            if !isValid {
                Text("Invalid date: \(day)")
                    .foregroundStyle(.red)
                    .padding(8)
            } else {
                AgendaView(day: day, notes: notes, tasks: tasks, client: client)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Subviews

private struct DayCell: View {
    let day: Date
    let inMonth: Bool
    let taskTexts: [String]
    let taskCount: Int
    let noteTitles: [String]
    let noteCount: Int
    let deadlineCount: Int
    let overdueCount: Int
    let dueFill: (fill: Double, overdue: Bool)
    let isToday: Bool
    let isSelected: Bool
    let isActive: Bool
    let onSelect: () -> Void
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.trackFontScale) private var fontScale

    var body: some View {
        let palette = TrackTheme.palette(for: colorScheme)
        return Button(action: onSelect) {
            VStack(alignment: .leading, spacing: 2) {
                Text("\(Calendar.current.component(.day, from: day))")
                    .font(.system(size: 13 * fontScale))
                    // Today is a filled mark disc, digits knocked out in panel
                    // (design.md Calendar) — never the system blue capsule.
                    .foregroundStyle(isToday ? palette.panel : (inMonth ? palette.text : palette.faint))
                    .frame(width: isToday ? 22 * fontScale : nil, height: isToday ? 22 * fontScale : nil)
                    .background(isToday ? palette.mark : Color.clear, in: Circle())
                if taskCount > taskTexts.count {
                    Text("+\(taskCount - taskTexts.count)").font(.system(size: 11 * fontScale)).foregroundStyle(palette.faint)
                }
                ForEach(taskTexts, id: \.self) { text in
                    Text(text).font(.system(size: 11 * fontScale)).lineLimit(1)
                }
                ForEach(noteTitles, id: \.self) { title in
                    Text(title).font(.system(size: 11 * fontScale)).foregroundStyle(palette.muted).lineLimit(1)
                }
                if noteCount > noteTitles.count {
                    Text("+\(noteCount - noteTitles.count)").font(.system(size: 11 * fontScale)).foregroundStyle(palette.faint)
                }
                if dueFill.fill > 0 {
                    GeometryReader { proxy in
                        ZStack(alignment: .leading) {
                            // Hairline ground; fill is chart-1 running down to
                            // the deadline, danger whole once overdue.
                            Capsule().fill(palette.line)
                            Capsule().fill(dueFill.overdue ? palette.danger : palette.chartPalette[0])
                                .frame(width: proxy.size.width * (dueFill.overdue ? 1 : dueFill.fill))
                        }
                    }
                    .frame(height: 3)
                }
                if deadlineCount > 0 {
                    Text(overdueCount > 0 ? "[!] \(deadlineCount) · overdue \(overdueCount)" : "[!] \(deadlineCount)")
                        .font(.system(size: 11 * fontScale))
                        .fontWeight(overdueCount > 0 ? .semibold : .regular)
                        .foregroundStyle(overdueCount > 0 ? palette.danger : palette.muted)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(6)
            .background(isSelected ? palette.mark.opacity(0.12) : Color.clear,
                        in: RoundedRectangle(cornerRadius: 6))
            .overlay(RoundedRectangle(cornerRadius: 6)
                .strokeBorder(isSelected ? palette.mark : Color.clear, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .disabled(!isActive)
        .opacity(isActive ? 1 : 0.45)
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
    @State private var openedNoteID: TrackID?
    @State private var isShowingNote = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(day).font(.caption).foregroundStyle(.secondary)
            if notes.isEmpty && tasks.isEmpty {
                Text("Nothing this day").font(.caption).foregroundStyle(.tertiary)
            }
            if !notes.isEmpty {
                Text("Notes").font(.caption2).foregroundStyle(.secondary)
                ForEach(notes, id: \.ref.noteID) { note in
                    Button(note.ref.title) { open(note.ref.noteID) }
                        .buttonStyle(.link)
                        .font(.body)
                }
            }
            if !tasks.isEmpty {
                Text("Tasks").font(.caption2).foregroundStyle(.secondary)
                ForEach(tasks, id: \.self) { task in
                    HStack(spacing: 6) {
                        Text(task.item.done ? "✓" : "○")
                            .foregroundStyle(task.item.done ? .secondary : .primary)
                        Button(task.item.text) { open(task.noteID) }
                            .buttonStyle(.link)
                            .lineLimit(2)
                        if let due = task.item.due {
                            Text("! \(due)").font(.caption).foregroundStyle(.secondary)
                        }
                        if let sched = task.item.scheduled {
                            Text("▷ \(sched)").font(.caption).foregroundStyle(.tertiary)
                        }
                        Button(task.title) { open(task.noteID) }
                            .buttonStyle(.link)
                            .font(.caption)
                            .foregroundStyle(.secondary)
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
        .sheet(isPresented: $isShowingNote) {
            if let openedNoteID { NotePreviewView(client: client, noteID: openedNoteID) }
        }
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

    private func open(_ noteID: TrackID) {
        openedNoteID = noteID
        isShowingNote = true
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

private struct NotePreviewView: View {
    let client: TrackClient
    let noteID: TrackID
    @Environment(\.dismiss) private var dismiss
    @State private var title = ""
    @State private var bodyText = ""
    @State private var error: String?
    @State private var isLoading = true

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(title.isEmpty ? "Note" : title).font(.headline)
                Spacer()
                Button("Done") { dismiss() }
            }
            Divider()
            if isLoading {
                ProgressView()
            } else if let error {
                Text(error).foregroundStyle(.red)
            } else {
                ScrollView {
                    Text(bodyText).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
        .padding(20)
        .frame(minWidth: 420, minHeight: 280)
        .task {
            do {
                let response = try await client.getNote(noteID)
                title = response.note.summary.ref.title
                bodyText = response.note.body
            } catch {
                self.error = error.localizedDescription
            }
            isLoading = false
        }
    }
}
