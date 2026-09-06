import SwiftUI
import TrackAPI

// MVP task board: dated listing, state cycling, due/scheduled editing.
// Cell controls wear the text they show (design.md: Task table) — the state
// cell is a stripped picker, the date cell a stripped button opening a
// native DatePicker popover.

public struct TasksView: View {
    @Bindable var model: TasksModel
    @State private var dateRow: TaskRow?

    public init(model: TasksModel) {
        self.model = model
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Toggle("Open only", isOn: $model.showOpenOnly)
                .toggleStyle(.checkbox)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .onChange(of: model.showOpenOnly) {
                    Task { await model.reload() }
                }
            Divider()
            List(model.rows, id: \.self) { row in
                TaskRowView(
                    row: row,
                    onCycleState: { Task { await model.setState(row: row, to: nextState(after: row.item.state)) } },
                    onPickDate: { dateRow = row }
                )
            }
        }
        .task { await model.reload() }
        .popover(item: $dateRow) { row in
            TaskDateEditor(row: row) { field, date in
                Task { await model.setDate(row: row, field: field, date: date) }
            }
            .padding()
        }
    }
}

extension TaskRow: Hashable {
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

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Button(row.item.state) { onCycleState() }
                .buttonStyle(.plain)
                .foregroundStyle(row.item.done ? .secondary : .primary)
            VStack(alignment: .leading, spacing: 2) {
                Text(row.item.text)
                    .strikethrough(row.item.done)
                Text(row.title)
                    .font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            if let due = row.item.due {
                Button(due) { onPickDate() }
                    .buttonStyle(.plain).font(.caption).foregroundStyle(.secondary)
            } else if let sched = row.item.scheduled {
                Button(sched) { onPickDate() }
                    .buttonStyle(.plain).font(.caption).foregroundStyle(.tertiary)
            }
        }
    }
}

private struct TaskDateEditor: View {
    let row: TaskRow
    let onSave: (DateField, String) -> Void
    @State private var date = Date()
    @State private var field: DateField = .due
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Field", selection: $field) {
                Text("Due").tag(DateField.due)
                Text("Scheduled").tag(DateField.scheduled)
            }
            .pickerStyle(.segmented)
            DatePicker("Date", selection: $date, displayedComponents: .date)
            HStack {
                Button("Clear") {
                    onSave(field, "")
                    dismiss()
                }
                Spacer()
                Button("Save") {
                    onSave(field, Self.format(date))
                    dismiss()
                }
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
