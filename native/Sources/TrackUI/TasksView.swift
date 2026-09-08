import SwiftUI
import TrackAPI

// MVP task board: dated listing, state cycling, due/scheduled editing.
// Cell controls wear the text they show (design.md: Task table) — the state
// cell is a stripped picker, the date cell a stripped button opening a
// native DatePicker popover.

public struct TasksView: View {
    @Bindable var model: TasksModel
    @State private var dateRow: TaskRow?
    @State private var mode: Mode = .list

    public init(model: TasksModel) {
        self.model = model
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                Toggle("Open only", isOn: $model.showOpenOnly)
                    .toggleStyle(.checkbox)
                    .onChange(of: model.showOpenOnly) {
                        Task { await model.reload() }
                    }
                Spacer()
                Picker("View", selection: $mode) {
                    Text("List").tag(Mode.list)
                    Text("Board").tag(Mode.board)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 160)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            Divider()
            Group {
                if let conflict = model.lastConflict {
                    Banner(message: conflict, isError: false) { model.dismissConflict() }
                } else if let message = model.error {
                    Banner(message: message, isError: true) { model.dismissError() }
                }
            }
            if model.isLoading && model.rows.isEmpty {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if model.rows.isEmpty {
                emptyState
            } else if mode == .board {
                TaskBoard(model: model)
            } else {
                List(model.rows, id: \.self) { row in
                    TaskRowView(
                        row: row,
                        onCycleState: { Task { await model.setState(row: row, to: nextState(after: row.item.state)) } },
                        onPickDate: { dateRow = row }
                    )
                }
            }
        }
        .task { await model.reload() }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            Task { await model.reload() }
        }
        .popover(item: $dateRow) { row in
            TaskDateEditor(row: row) { field, date in
                Task { await model.setDate(row: row, field: field, date: date) }
            }
            .padding()
        }
    }

    /// What the web calls "Nothing to do." — but the native board can show the
    /// dated listing too, so the copy matches the mode actually on screen.
    @ViewBuilder
    private var emptyState: some View {
        Text(model.showOpenOnly ? "Nothing to do." : "No tasks")
            .font(.caption)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private enum Mode: String, CaseIterable, Identifiable {
        case list
        case board

        var id: String { rawValue }
    }
}

/// One-line notice drawn above the list: a read error (red) or the conflict
/// notice from a refused write, each dismissible.
private struct Banner: View {
    let message: String
    let isError: Bool
    let onDismiss: () -> Void

    var body: some View {
        HStack(spacing: 8) {
            Text(message)
                .font(.caption)
                .foregroundStyle(isError ? Color.red : Color.secondary)
            Spacer()
            Button("Dismiss") { onDismiss() }
                .buttonStyle(.plain)
                .font(.caption)
                .foregroundStyle(isError ? Color.red : Color.secondary)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(isError ? Color.red.opacity(0.1) : Color.secondary.opacity(0.08))
        .overlay(alignment: .bottom) { Divider() }
    }
}

extension TaskRow: Hashable, Identifiable {
    public var id: String { "\(noteID.raw)#\(item.line)" }

    public static func == (lhs: TaskRow, rhs: TaskRow) -> Bool {
        lhs.noteID == rhs.noteID && lhs.item.line == rhs.item.line
    }

    public func hash(into hasher: inout Hasher) {
        hasher.combine(noteID)
        hasher.combine(item.line)
    }
}

private struct TaskRowView: View {
    let row: TaskRow
    let onCycleState: () -> Void
    let onPickDate: () -> Void
    @Environment(\.openURL) private var openURL

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Button(row.item.state) { onCycleState() }
                .buttonStyle(.plain)
                .foregroundStyle(row.item.done ? .secondary : .primary)
            VStack(alignment: .leading, spacing: 2) {
                // Priority leads the text the way it drives the row's place in
                // the engine's order (web taskMark: [#A] before [#B] before
                // unprioritized).
                if let priority = row.item.priority {
                    Text("[#\(priority)]")
                        .font(.caption).fontWeight(.bold)
                        .foregroundStyle(.primary)
                }
                Text(row.item.text)
                    .strikethrough(row.item.done)
                if let completed = row.item.completed {
                    Text("✓ \(completed)")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                Text(row.title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            dateStack
        }
        .contentShape(Rectangle())
        // The row is the navigation target, while the state and date buttons
        // above remain their own controls and consume their edit gestures.
        .onTapGesture { _ = openURL(noteURL(for: row)) }
    }

    /// The date area is still the cell's own control (design.md Task table): a
    /// stripped button opening the native picker. Both dates are offered, with
    /// the due date shown first — the deadline outranks the scheduled date, as
    /// the web row marks with "!" and "▷".
    @ViewBuilder
    private var dateStack: some View {
        HStack(spacing: 8) {
            if let due = row.item.due {
                Button("! \(due)") { onPickDate() }
                    .buttonStyle(.plain).font(.caption).foregroundStyle(.secondary)
            }
            if let sched = row.item.scheduled {
                Button("▷ \(sched)") { onPickDate() }
                    .buttonStyle(.plain).font(.caption).foregroundStyle(.tertiary)
            }
        }
    }
}

/// The reader handles this same URL scheme for wikilinks.
private func noteURL(for row: TaskRow) -> URL {
    let target = row.noteID.raw.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? row.noteID.raw
    return URL(string: "trackwiki://\(target)")!
}

private struct TaskDateEditor: View {
    let row: TaskRow
    let onSave: (DateField, String) -> Void
    @State private var date = Date()
    @State private var field: DateField = .due
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        // Workspace's own calendar cell (design.md Task table): the native
        // DatePicker cannot draw today's mark ring or the working choice's
        // mark fill, so the closest native idiom is the mark tint — today
        // keeps the system ring, the choice wears the salient.
        let palette = TrackTheme.palette(for: colorScheme)
        return VStack(alignment: .leading, spacing: 12) {
            Picker("Field", selection: $field) {
                Text("Due").tag(DateField.due)
                Text("Scheduled").tag(DateField.scheduled)
            }
            .pickerStyle(.segmented)
            DatePicker("Date", selection: $date, displayedComponents: .date)
                .tint(palette.mark)
            HStack {
                Button("DELETE") {
                    onSave(field, "")
                    dismiss()
                }
                .buttonStyle(.plain)
                .font(.caption)
                .foregroundStyle(palette.muted)
                Spacer()
                Button("SAVE") {
                    onSave(field, Self.format(date))
                    dismiss()
                }
                .buttonStyle(.plain)
                .font(.caption)
                .foregroundStyle(palette.text)
                .fontWeight(.medium)
            }
        }
        .frame(minWidth: 280)
    }

    private static func format(_ date: Date) -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.string(from: date)
    }
}

/// Next state in the fixed table (`web/src/taskStates.ts`, mirroring the
/// engine's `task.States`): TODO → DOING → WAITING → DONE → CANCELLED → TODO.
private func nextState(after state: String) -> String {
    let order = ["TODO", "DOING", "WAITING", "DONE", "CANCELLED"]
    guard let i = order.firstIndex(of: state) else { return "TODO" }
    return order[(i + 1) % order.count]
}
