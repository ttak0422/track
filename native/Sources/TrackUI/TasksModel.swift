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
    /// Set when a write is refused because the note changed underneath the view
    /// (409) — the reloaded list is what the server sees now, and the change was
    /// not applied. Shown until dismissed (queries.ts handleTaskWriteError).
    public private(set) var lastConflict: String?
    /// True while a reload is in flight (initial load, the open-only toggle, and
    /// the refetch after a conflict), so the view never flashes an empty state.
    public private(set) var isLoading = false
    public var showOpenOnly = false
    public private(set) var isWriting = false
    private let noteID: TrackID?
    private var loadGeneration = 0

    private let client: TrackClient
    /// Latest write etag per note (from TasksResponse). A task is a note id
    /// plus a file line on every surface (api.ts:344), so the etag keys by note.
    private var etags: [TrackID: String] = [:]
    /// Last state drawn per task, sent back as `expect` so the server can
    /// refuse a write against a line the view never saw (api.ts:371).
    private var drawn: [TaskKey: String] = [:]

    public init(client: TrackClient, noteID: TrackID? = nil) {
        self.client = client
        self.noteID = noteID
    }

    public func reload() async {
        loadGeneration += 1
        let token = loadGeneration
        isLoading = true
        defer { if token == loadGeneration { isLoading = false } }
        error = nil
        do {
            let fresh: [TaskRow]
            var freshEtags: [TrackID: String] = [:]
            if let noteID {
                let response = try await client.getNote(noteID)
                let ref = response.note.summary.ref
                fresh = (response.note.tasks?.items ?? []).map {
                    TaskRow(item: $0, noteID: noteID, fileKind: ref.fileKind, title: ref.title)
                }
                freshEtags[noteID] = response.note.etag
            } else {
                let res = showOpenOnly
                    ? try await client.listOpenTasks()
                    : try await client.listDatedTasks()
                fresh = res.tasks
            }
            guard token == loadGeneration, !Task.isCancelled else { return }
            rows = fresh
            etags = freshEtags
            drawn = Dictionary(uniqueKeysWithValues: fresh.map {
                (TaskKey(note: $0.noteID, line: $0.item.line), $0.item.state)
            })
        } catch {
            if token == loadGeneration { self.error = error.localizedDescription }
        }
    }

    public func setState(row: TaskRow, to state: String) async {
        guard !isWriting else { return }
        isWriting = true
        defer { isWriting = false }
        let key = TaskKey(note: row.noteID, line: row.item.line)
        do {
            _ = try await client.setTaskState(
                id: row.noteID, line: row.item.line, state: state,
                expect: drawn[key] ?? row.item.state,
                etag: etags[row.noteID] ?? ""
            )
            await reload()
            NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
        } catch {
            await handleWriteFailure(error)
        }
    }

    public func setDate(row: TaskRow, field: DateField, date: String) async {
        guard !isWriting else { return }
        isWriting = true
        defer { isWriting = false }
        do {
            _ = try await client.setTaskDate(
                id: row.noteID, line: row.item.line, field: field, date: date,
                expect: drawn[TaskKey(note: row.noteID, line: row.item.line)] ?? row.item.state,
                etag: etags[row.noteID] ?? ""
            )
            await reload()
            NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
        } catch {
            await handleWriteFailure(error)
        }
    }

    /// Dismiss the read-failure banner.
    public func dismissError() {
        error = nil
    }

    /// Dismiss the "note changed underneath" banner; the retried write then
    /// runs against the reloaded list the conflict notice describes.
    public func dismissConflict() {
        lastConflict = nil
    }

    // MARK: - Private

    /// A write refused with 409 means the row the view drew is stale: the server
    /// left the file untouched, so reload to show what the note says now and
    /// tell the user the change was not applied (web api.ts handleTaskWriteError).
    private func handleWriteFailure(_ failure: any Error) async {
        guard let api = failure as? APIError, api.status == 409 else {
            self.error = failure.localizedDescription
            return
        }
        lastConflict = "Note changed underneath — reloaded"
        await reload()
    }

}

private struct TaskKey: Hashable {
    let note: TrackID
    let line: Int
}
