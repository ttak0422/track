import Foundation
import Observation
import TrackAPI

// View model for the MVP task board: the vault-wide dated listing plus the
// optimistic-concurrency write path (etag echo, 409 on stale view).
// Mirrors web/src/api.ts: listDatedTasks/listOpenTasks/setTaskState/setTaskDate.

@MainActor
@Observable
public final class TasksModel {
    public private(set) var rows: [TaskRow] = []
    public private(set) var error: String?
    public var showOpenOnly = false

    private let client: TrackClient
    /// Latest write etag per note (from TasksResponse). A task is a note id
    /// plus a file line on every surface (api.ts:344), so the etag keys by note.
    private var etags: [TrackID: String] = [:]
    /// Last state drawn per task, sent back as `expect` so the server can
    /// refuse a write against a line the view never saw (api.ts:371).
    private var drawn: [TaskKey: String] = [:]

    public init(client: TrackClient) {
        self.client = client
    }

    public func reload() async {
        error = nil
        do {
            let res = showOpenOnly
                ? try await client.listOpenTasks()
                : try await client.listDatedTasks()
            rows = res.tasks
            drawn = Dictionary(
                uniqueKeysWithValues: res.tasks.map {
                    (TaskKey(note: $0.noteID, line: $0.item.line), $0.item.state)
                }
            )
        } catch {
            self.error = error.localizedDescription
        }
    }

    public func setState(row: TaskRow, to state: String) async {
        let key = TaskKey(note: row.noteID, line: row.item.line)
        do {
            // Redraw from the response — no second request (api.ts:372).
            let res = try await client.setTaskState(
                id: row.noteID, line: row.item.line, state: state,
                expect: drawn[key] ?? row.item.state,
                etag: etags[row.noteID] ?? ""
            )
            applyWrite(note: row.noteID, title: row.title, fileKind: row.fileKind, response: res)
        } catch {
            self.error = error.localizedDescription
        }
    }

    public func setDate(row: TaskRow, field: DateField, date: String) async {
        do {
            let res = try await client.setTaskDate(
                id: row.noteID, line: row.item.line, field: field, date: date,
                expect: drawn[TaskKey(note: row.noteID, line: row.item.line)] ?? row.item.state,
                etag: etags[row.noteID] ?? ""
            )
            applyWrite(note: row.noteID, title: row.title, fileKind: row.fileKind, response: res)
        } catch {
            self.error = error.localizedDescription
        }
    }

    // MARK: - Private

    private func applyWrite(note: TrackID, title: String, fileKind: String, response: TasksResponse) {
        etags[note] = response.etag
        // The write response carries the note's own tasks (no note context),
        // so merge them back into the vault-wide rows by line.
        let byLine = Dictionary(uniqueKeysWithValues: response.items.map { ($0.line, $0) })
        for i in rows.indices where rows[i].noteID == note {
            if let fresh = byLine[rows[i].item.line] {
                rows[i].item = fresh
                drawn[TaskKey(note: note, line: fresh.line)] = fresh.state
            }
        }
    }
}

private struct TaskKey: Hashable {
    let note: TrackID
    let line: Int
}
