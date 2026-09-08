import SwiftUI
import TrackAPI

// Five-column kanban over the same vault-wide dated listing the list shows.
// Each card wears its own state picker, so moving a task is a `setState` write
// (no drag-and-drop); the columns are a fixed projection of `rows` by state,
// so a write lands back through the model's redraw-from-response path.

public struct TaskBoard: View {
    @Bindable var model: TasksModel

    private static let states = ["TODO", "DOING", "WAITING", "DONE", "CANCELLED"]

    public init(model: TasksModel) {
        self.model = model
    }

    public var body: some View {
        HStack(alignment: .top, spacing: 8) {
            ForEach(Self.states, id: \.self) { state in
                TaskColumn(
                    title: state,
                    rows: model.rows.filter { $0.item.state == state }
                ) { row, newState in
                    Task { await model.setState(row: row, to: newState) }
                }
            }
        }
        .padding(8)
    }
}

private struct TaskColumn: View {
    let title: String
    let rows: [TaskRow]
    let onChange: (TaskRow, String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("\(title) · \(rows.count)")
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
            ScrollView {
                VStack(spacing: 6) {
                    ForEach(rows, id: \.self) { row in
                        TaskCard(row: row, onChange: onChange)
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

private struct TaskCard: View {
    let row: TaskRow
    let onChange: (TaskRow, String) -> Void

    private static let states = ["TODO", "DOING", "WAITING", "DONE", "CANCELLED"]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                if let priority = row.item.priority {
                    Text("[#\(priority)]")
                        .font(.caption).fontWeight(.bold)
                }
                Text(row.item.text.isEmpty ? "(untitled task)" : row.item.text)
                    .strikethrough(row.item.done)
                Spacer()
                Picker("", selection: stateBinding) {
                    ForEach(Self.states, id: \.self) { state in
                        Text(state).tag(state)
                    }
                }
                .pickerStyle(.menu)
                .labelsHidden()
                .fixedSize()
            }
            Text(row.title)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            if let due = row.item.due {
                Text("! \(due)").font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(Color.secondary.opacity(0.15), lineWidth: 1))
    }

    private var stateBinding: Binding<String> {
        Binding(
            get: { row.item.state },
            set: { newValue in
                if newValue != row.item.state { onChange(row, newValue) }
            }
        )
    }
}
