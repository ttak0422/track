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
            if let noteID {
                let response = try await client.getNote(noteID)
                let ref = response.note.summary.ref
                fresh = (response.note.tasks?.items ?? []).map {
                    TaskRow(item: $0, noteID: noteID, fileKind: ref.fileKind, title: ref.title)
                }
            } else {
                let res = showOpenOnly
                    ? try await client.listOpenTasks()
                    : try await client.listDatedTasks()
                fresh = res.tasks
            }
            guard token == loadGeneration, !Task.isCancelled else { return }
            rows = fresh
        } catch {
            if token == loadGeneration { self.error = error.localizedDescription }
        }
    }

    public func setState(row: TaskRow, to state: String) async {
        guard !isWriting else { return }
        isWriting = true
        defer { isWriting = false }
        do {
            guard let etag = try await writeEtag(for: row) else { return }
            _ = try await client.setTaskState(
                id: row.noteID, line: row.item.line, state: state,
                expect: row.item.state, etag: etag
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
            guard let etag = try await writeEtag(for: row) else { return }
            _ = try await client.setTaskDate(
                id: row.noteID, line: row.item.line, field: field, date: date,
                expect: row.item.state, etag: etag
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

    /// Global rows carry no etag. Fetch the note and confirm the task the user
    /// selected still occupies that line before pairing it with a write token.
    /// A retained date popover must never edit a replacement task after reload.
    private func writeEtag(for row: TaskRow) async throws -> String? {
        let note = try await client.getNote(row.noteID).note
        guard note.tasks?.items.first(where: { $0.line == row.item.line }) == row.item else {
            lastConflict = "Task changed underneath — change was not applied"
            await reload()
            return nil
        }
        return note.etag
    }

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
