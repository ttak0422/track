import Foundation
import Observation
import TrackAPI

// View models for the MVP reader (note + backlinks) and search.
// Rendering stays native: `note.body` is plain GFM from the engine and
// SwiftUI parses it locally — `/api/render` is never used.

// MARK: - Note reader

@MainActor
@Observable
public final class NoteReaderModel {
    public enum State: Sendable {
        case empty
        case loading
        case loaded(NoteResponse)
        case failed(String)
    }

    public private(set) var state: State = .empty
    /// The id `open` last succeeded with — the qualified id every write and
    /// the read report address. Cleared while loading and once nothing is open.
    public private(set) var currentID: TrackID?

    // Render pipeline (web useRenderQuery): `open` posts the raw body to
    // `/api/render` so the engine stays the single source of truth for
    // track-specific Markdown rules (action-link flattening, wiki links) and
    // resolves every `![[...]]` against the vault. On success the resolved
    // markdown and its includes are kept; on any failure the raw body is
    // rendered instead — a render failure must not stop the note from opening.
    public private(set) var renderedBody: String = ""
    public private(set) var renderedIncludes: [NoteInclude]?
    /// True once `/api/render` produced the currently-held `renderedBody` —
    /// distinguishes "render succeeded on an empty body" from "render failed".
    public private(set) var didRender = false

    /// The excerpt a `[[Note#Heading]]` / `[[Note#^block]]` anchor arrived
    /// with: the anchored heading (or block) through the lines before the next
    /// same-level heading. Shown in a dismissible card above the reader
    /// (MarkdownUI offers no in-body scroll target), and cleared when another
    /// note opens or when dismissed by the view.
    public var anchoredExcerpt: String?

    // Editing buffer for the note body (web NoteEditor parity). `draftBody`
    // mirrors the loaded body while the note is not being edited and diverges
    // while it is; a save gates on `isDirty` and echoes the loaded response's
    // etag (NoteDetail.etag) so the server can refuse (409) a stale overwrite.
    public var draftBody: String = ""
    public private(set) var isEditing = false

    // Write-path state (web useSaveNoteMutation / useDeleteNoteMutation /
    // createNote / useSaveNoteMetaMutation + the API's 409 conflict reply).
    // Each busy flag drives a disable + ProgressView-equivalent in the view.
    public private(set) var isSaving = false
    public private(set) var isDeleting = false
    public private(set) var isCreating = false
    public private(set) var isSavingMeta = false
    /// The most recent write-path failure (save/delete/create/meta save/meta
    /// load), shown until dismissed or until the next write clears it.
    public private(set) var saveError: String?
    /// Set when a body save is refused with 409: the note changed on disk, so
    /// the view reloaded the latest etag and the edit was NOT applied.
    public private(set) var saveConflict: String?

    /// The API client the reader hands down to the GFM renderer (viewspec
    /// resolution) and every read/write path.
    public let client: TrackClient

    public init(client: TrackClient) {
        self.client = client
    }

    /// The body the open note has on disk (`""` with nothing loaded) — the
    /// reference `isDirty` is measured against (web `loadedRef.body`).
    public var loadedBody: String {
        if case .loaded(let response) = state { return response.note.body }
        return ""
    }

    /// `draftBody` has diverged from the loaded body (web `dirty`).
    public var isDirty: Bool { draftBody != loadedBody }

    /// A note is open, so Edit/Done and the note actions are available.
    public var isLoaded: Bool {
        if case .loaded = state { return true }
        return false
    }

    public func open(_ id: TrackID) async {
        state = .loading
        currentID = nil
        // Opening always leaves editing behind: a draft belongs to the note
        // that was open, and switching notes discards it (web adopt path).
        isEditing = false
        draftBody = ""
        saveError = nil
        saveConflict = nil
        renderedBody = ""
        renderedIncludes = nil
        didRender = false
        anchoredExcerpt = nil
        do {
            let response = try await client.getNote(id)
            currentID = id
            draftBody = response.note.body
            // Resolve the body through the engine; a failure falls back to
            // the raw body so the note still opens.
            let vault = id.split().vault
            if let render = try? await client.renderMarkdown(body: response.note.body, vault: vault) {
                renderedBody = render.markdown
                renderedIncludes = render.includes
                didRender = true
            }
            state = .loaded(response)
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    // MARK: - Editing

    /// Enter the editor seeded from the loaded body.
    public func beginEditing() {
        guard isLoaded else { return }
        draftBody = loadedBody
        isEditing = true
    }

    /// Drop the draft and return to reading the loaded body. Any failure or
    /// conflict banner belongs to the abandoned edit, so it goes too.
    public func discardDraft() {
        draftBody = loadedBody
        isEditing = false
        saveError = nil
        saveConflict = nil
    }

    // MARK: - Writes (web NoteEditor / NoteMetaDialog / NoteActionsMenu parity)

    /// Dismiss the write-failure banner.
    public func dismissSaveError() {
        saveError = nil
    }

    /// Dismiss the "note changed on disk" conflict notice.
    public func dismissConflict() {
        saveConflict = nil
    }

    /// Save the draft against the loaded etag (web submit → saveNote). On
    /// success the note is refetched and the draft reseeded from the fresh
    /// body. A 409 leaves the draft intact, reloads the latest etag so a retry
    /// has a fresh token, and sets `saveConflict` (the edit was not applied).
    public func saveDraft() async {
        guard isDirty, !isSaving, let id = currentID else { return }
        isSaving = true
        saveError = nil
        saveConflict = nil
        defer { isSaving = false }
        do {
            _ = try await client.saveNote(id: id, body: draftBody, etag: loadedEtag)
            let fresh = try await client.getNote(id)
            draftBody = fresh.note.body
            state = .loaded(fresh)
            await render(fresh.note.body, id: id)
        } catch let api as APIError where api.status == 409 {
            saveConflict = "This note changed since it was loaded. Reloading the latest version; your edit was not saved."
            if let fresh = try? await client.getNote(id) {
                state = .loaded(fresh)
                await render(fresh.note.body, id: id)
            }
        } catch {
            saveError = Self.message(for: error)
        }
    }

    /// Delete the open note (web deleteNote). The view keeps the destructive
    /// button disabled until the retyped title matches; the model re-checks
    /// `confirmedTitle` so a stale call can never delete. Returns true once
    /// the note is gone (`state == .empty`).
    @discardableResult
    public func deleteCurrent(confirmedTitle: String) async -> Bool {
        guard case .loaded(let response) = state, !isDeleting, let id = currentID else { return false }
        guard confirmedTitle.trimmingCharacters(in: .whitespaces)
            == response.note.summary.ref.title.trimmingCharacters(in: .whitespaces) else {
            saveError = "Type the note's title exactly to confirm deletion."
            return false
        }
        isDeleting = true
        saveError = nil
        saveConflict = nil
        defer { isDeleting = false }
        do {
            _ = try await client.deleteNote(id: id)
            currentID = nil
            isEditing = false
            draftBody = ""
            state = .empty
            return true
        } catch {
            saveError = Self.message(for: error)
            return false
        }
    }

    /// Mint a note titled `title` with the default template (web createNote)
    /// and open it. Returns true on success (the new note is open and any
    /// staging sheet can clear + dismiss). A title that already resolves is
    /// refused with 409 — that and any other failure set `saveError` and
    /// return false so the caller keeps its creation dialog open.
    @discardableResult
    public func createNote(title: String) async -> Bool {
        let trimmed = title.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else {
            saveError = "Enter a title to create the note."
            return false
        }
        guard !isCreating else { return false }
        isCreating = true
        saveError = nil
        defer { isCreating = false }
        do {
            let created = try await client.createNote(title: trimmed)
            await open(created.noteID)
            return true
        } catch let api as APIError where api.status == 409 {
            // VoiceView parity: a duplicate is a notice, not an error.
            saveError = "A note with the same title already exists"
            return false
        } catch {
            saveError = Self.message(for: error)
            return false
        }
    }

    /// Replace the open note's whole editable sidecar metadata (web
    /// saveNoteMeta). On success the note is refetched so the header's
    /// title/tags/flags/props track the saved metadata. Returns true on
    /// success; a rejected edit sets `saveError` and returns false so the
    /// meta sheet stays open (web dialog keeps open, changing nothing).
    @discardableResult
    public func saveMeta(_ request: SaveNoteMetaRequest) async -> Bool {
        guard !isSavingMeta, let id = currentID else { return false }
        isSavingMeta = true
        saveError = nil
        defer { isSavingMeta = false }
        do {
            _ = try await client.saveNoteMeta(id: id, request: request)
            await refreshOpenNote()
            return true
        } catch {
            saveError = Self.message(for: error)
            return false
        }
    }

    /// Fetch the open note's editable metadata so the meta sheet can seed its
    /// form (web useNoteMetaQuery). Returns nil — with `saveError` set — when
    /// the fetch fails or nothing is open.
    public func fetchMeta() async -> NoteMetaResponse? {
        guard let id = currentID else { return nil }
        saveError = nil
        do {
            return try await client.getNoteMeta(id)
        } catch {
            saveError = Self.message(for: error)
            return nil
        }
    }

    /// Report the shared "seen" reading milestone for the open note
    /// (reading.ts markSeen). Fire-and-forget from the reader's `.task`; a
    /// failure must not disturb the note (try? swallows it).
    public func reportSeen() async {
        guard let id = currentID else { return }
        _ = try? await client.markRead(id: id, event: .seen)
    }

    // MARK: - Tasks (web setTaskState on the note's own board)

    /// Move a task line into `newState` against the note's own etag, mirroring
    /// the web's setTaskState write path (`expect` is the line's currently
    /// drawn state, asserted so a stale write to a moved line is refused). On
    /// success the response's refreshed tasks + etag are adopted into the open
    /// note; a 409 means the note changed underneath — the view reloads, and
    /// `saveConflict` explains the write was not applied.
    public func setTaskState(line: Int, to newState: String) async {
        guard let id = currentID,
              case .loaded(let response) = self.state,
              response.note.tasks != nil else { return }
        let expect = response.note.tasks?.items.first { $0.line == line }?.state ?? newState
        do {
            let res = try await client.setTaskState(
                id: id, line: line, state: newState,
                expect: expect, etag: response.note.etag
            )
            adoptTasks(res)
        } catch let api as APIError where api.status == 409 {
            saveConflict = "Note changed underneath — reloaded"
            if let fresh = try? await client.getNote(id) {
                self.state = .loaded(fresh)
                await render(fresh.note.body, id: id)
            }
        } catch {
            saveError = Self.message(for: error)
        }
    }

    /// Adopt the write response's refreshed tasks + etag into the open note,
    /// replacing only the task list it carried (a task write response lacks
    /// note context, so the rest of the note stays as loaded).
    private func adoptTasks(_ response: TasksResponse) {
        guard case .loaded(let current) = self.state else { return }
        var updated = current
        if let tasks = Self.noteTasks(from: response.items) {
            updated.note.tasks = tasks
        }
        updated.note.etag = response.etag
        self.state = .loaded(updated)
    }

    /// Build a `NoteTasks` from item rows. The struct has no public memberwise
    /// initializer (its memberwise init is internal and lives in TrackAPI), so
    /// it is rebuilt by round-tripping the items through JSON and decoding the
    /// `{"items": […]}` shape its Codable conformance expects.
    private static func noteTasks(from items: [TaskItem]) -> NoteTasks? {
        guard let itemData = try? JSONEncoder().encode(items),
              let itemArray = try? JSONSerialization.jsonObject(with: itemData),
              let wrapped = try? JSONSerialization.data(withJSONObject: ["items": itemArray]),
              let tasks = try? JSONDecoder().decode(NoteTasks.self, from: wrapped)
        else { return nil }
        return tasks
    }

    /// The loaded response's etag — the write token a save echoes back.
    private var loadedEtag: String {
        if case .loaded(let response) = state { return response.note.etag }
        return ""
    }

    /// Resolve `body` through the engine's `/api/render`, keeping the resolved
    /// markdown and includes on success and leaving the previous (or raw) body
    /// on failure. Fire-and-forget when a note just needs refreshing —
    /// rendering is a pure derivation of the body.
    private func render(_ body: String, id: TrackID) async {
        let vault = id.split().vault
        if let render = try? await client.renderMarkdown(body: body, vault: vault) {
            renderedBody = render.markdown
            renderedIncludes = render.includes
            didRender = true
        }
    }

    /// Re-fetch the open note after a write, adopting the fresh body into the
    /// draft only when the buffer is clean — a background refresh must not
    /// clobber unsaved edits (web adopt guard body === loadedRef.body). Best
    /// effort: the write itself succeeded, so a failed refresh keeps the
    /// last-known state rather than failing the note.
    private func refreshOpenNote() async {
        guard let id = currentID else { return }
        do {
            let fresh = try await client.getNote(id)
            if !isDirty { draftBody = fresh.note.body }
            state = .loaded(fresh)
            await render(fresh.note.body, id: id)
        } catch {
            // Swallowed — see above.
        }
    }

    private static func message(for error: any Error) -> String {
        (error as? APIError)?.message ?? error.localizedDescription
    }

    /// Backlink taps resolve through the same id space the lists use.
    public func openRef(_ ref: NoteRef) async {
        await open(TrackID.qualify(vault: "", id: ref.noteID.raw))
    }

    /// Resolve and open a `[[wikilink]]` target. The target grammar
    /// (`web/src/components/markdown/plugins.ts: splitWikiTarget`) allows an
    /// optional leading `vault:` and a trailing `#anchor`/`#^block`; the
    /// anchor is stripped for resolution (it names a destination inside the
    /// note, not a different note), but kept to compute the anchored excerpt
    /// shown above the reader. Cross-vault targets resolve through the same
    /// `/api/resolve` the web reader uses.
    public func openWikilink(target: String) async {
        let parsed = Self.splitWikilinkFull(target)
        do {
            let resolved = try await client.resolveTerm(parsed.term, vault: parsed.vault)
            guard resolved.found else { return }
            let id = TrackID.qualify(vault: parsed.vault, id: resolved.note.noteID.raw)
            // Compute the excerpt from the *target* note before it opens, so
            // the anchor lands in the note actually named, not the one already
            // on screen.
            let excerpt = parsed.anchor.isEmpty ? nil : await Self.anchoredExcerpt(for: parsed.anchor, in: id, client: client)
            await open(id)
            anchoredExcerpt = excerpt
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    /// `vault:title#anchor` → (`vault`, `title`); a bare `title` keeps the
    /// empty vault. The anchor (block or heading) is dropped for lookup.
    private static func splitWikilink(_ target: String) -> (vault: String, term: String) {
        let trimmed = target.trimmingCharacters(in: .whitespaces)
        let noAnchor = trimmed.split(separator: "#", maxSplits: 1).first.map(String.init) ?? trimmed
        if let colon = noAnchor.firstIndex(of: ":") {
            let vault = String(noAnchor[..<colon])
            let term = String(noAnchor[noAnchor.index(after: colon)...])
            return (vault, term)
        }
        return ("", noAnchor)
    }

    /// A `[[target]]` split into its resolution key (vault + term) and the
    /// trailing anchor name (heading text or `^block` id), mirroring the web's
    /// `splitWikiTarget`.
    private static func splitWikilinkFull(_ target: String) -> (vault: String, term: String, anchor: String) {
        let trimmed = target.trimmingCharacters(in: .whitespaces)
        let (keyPart, anchor) = Self.splitAnchor(trimmed)
        let (vault, term) = splitWikilink(keyPart)
        return (vault, term, anchor)
    }

    /// `key#anchor` → (`key`, `anchor`); nil/empty anchor when there is no
    /// `#` or the fragment is empty (`C#` keeps the hash as part of the key).
    private static func splitAnchor(_ target: String) -> (String, String) {
        guard let i = target.firstIndex(of: "#") else { return (target, "") }
        let rest = target[target.index(after: i)...].trimmingCharacters(in: .whitespaces)
        if rest.isEmpty { return (target, "") }
        return (target[..<i].trimmingCharacters(in: .whitespaces), rest)
    }

    /// The excerpt the anchored excerpt card shows: fetch the target note and
    /// collect the anchored heading (or `^block`) through the lines before the
    /// next same-level heading. Returns nil when the anchor does not match or
    /// the fetch fails (the note still opens, just without a card).
    private static func anchoredExcerpt(for anchor: String, in id: TrackID, client: TrackClient) async -> String? {
        guard let response = try? await client.getNote(id) else { return nil }
        let body = response.note.body
        let lines = body.components(separatedBy: "\n")
        let isBlock = anchor.hasPrefix("^")
        let blockID = isBlock ? String(anchor.dropFirst()) : nil
        let heading = isBlock ? nil : anchor

        // Scan ATX headings (skip fenced code) so heading level and anchors
        // match the note's own structure.
        var start = -1
        var startLevel = 0
        var fence: String? = nil
        for (idx, raw) in lines.enumerated() {
            let line = raw
            if fence != nil {
                let t = line.trimmingCharacters(in: .whitespaces)
                if t.hasPrefix("```") || t.hasPrefix("~~~") { fence = nil }
                continue
            }
            let t = line.trimmingCharacters(in: .whitespaces)
            if t.hasPrefix("```") || t.hasPrefix("~~~") {
                fence = String(t.prefix(while: { $0 == "`" || $0 == "~" }))
                continue
            }
            if isBlock {
                if t == "^" + blockID! || t.hasSuffix(" ^" + blockID!) {
                    start = idx
                    break
                }
            } else if let heading {
                if let level = Self.headingLevel(line), Self.headingSlug(Self.headingText(line)) == heading {
                    start = idx
                    startLevel = level
                    break
                }
            }
        }

        guard start >= 0 else { return nil }

        // Collect from the anchor through the line before the next heading at
        // or above the anchor's level (a block anchor runs to the next heading
        // of any level).
        var excerpt: [String] = []
        var j = start
        while j < lines.count {
            let line = lines[j]
            if isBlock {
                if Self.headingLevel(line) != nil { break }
            } else if let level = Self.headingLevel(line), level <= startLevel {
                if j > start { break }
            }
            excerpt.append(line)
            j += 1
        }
        let text = excerpt.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
        return text.isEmpty ? nil : text
    }

    /// The ATX heading level of a line (1–6), or nil when it is not a heading.
    private static func headingLevel(_ line: String) -> Int? {
        var count = 0
        for ch in line {
            if ch == "#" { count += 1 } else { break }
        }
        guard count >= 1, count <= 6 else { return nil }
        let after = line.dropFirst(count)
        guard after.first == " " || after.first == "\t" else { return nil }
        return count
    }

    /// The heading text (`TODAY` after `## TODAY`), trailing `#`s trimmed.
    private static func headingText(_ line: String) -> String {
        let level = headingLevel(line) ?? 0
        var text = String(line.dropFirst(max(level, 0)))
        text = text.trimmingCharacters(in: .whitespaces)
        while text.hasSuffix("#") { text.removeLast(); text = text.trimmingCharacters(in: .whitespaces) }
        return text
    }

    /// The heading slug an anchor names, matching the web's `headingSlug`
    /// (markdown syntax stripped, lowercased, non letter/number/space/hyphen
    /// dropped, spaces → hyphens).
    private static func headingSlug(_ text: String) -> String {
        var label = text
        label = Self.wikiRoleRegex.stringByReplacingMatches(
            in: label,
            range: NSRange(label.startIndex..., in: label),
            withTemplate: "$1"
        )
        label = Self.mdLinkRegex.stringByReplacingMatches(
            in: label,
            range: NSRange(label.startIndex..., in: label),
            withTemplate: "$1"
        )
        label = label.replacingOccurrences(of: "*", with: "")
            .replacingOccurrences(of: "_", with: "")
            .replacingOccurrences(of: "~", with: "")
            .replacingOccurrences(of: "`", with: "")
        var slug = label.lowercased()
        var cleaned = ""
        for ch in slug {
            if ch.isLetter || ch.isNumber || ch == " " || ch == "-" {
                cleaned.append(ch)
            }
        }
        slug = cleaned.trimmingCharacters(in: .whitespaces)
            .replacingOccurrences(of: " ", with: "-")
        return slug.isEmpty ? "section" : slug
    }

    private static let wikiRoleRegex = try! NSRegularExpression(
        pattern: "\\[\\[[^|\\]]+\\|([^\\]]+)\\]\\]"
    )
    private static let mdLinkRegex = try! NSRegularExpression(
        pattern: "\\[([^\\]]+)\\]\\([^\\s)]*\\)"
    )
}

// MARK: - Search

@MainActor
@Observable
public final class SearchModel {
    public private(set) var results: [SearchResult] = []
    public private(set) var isLoading = false
    public private(set) var error: String?
    /// Vaults the server could not search, mirroring `SearchPanel`'s
    /// "vault … could not be searched" note (the count drives a banner).
    public private(set) var unavailableCount = 0
    private let client: TrackClient
    private var pendingSearch: Task<Void, Never>?

    public init(client: TrackClient) {
        self.client = client
    }

    /// Starts a live search. Keeping the debounce task here (rather than in the
    /// view) also makes every search entry point share the same cancellation
    /// and stale-response behaviour.
    public func search(query: String) {
        pendingSearch?.cancel()
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        error = nil
        guard !query.isEmpty else {
            results = []
            unavailableCount = 0
            isLoading = false
            return
        }

        isLoading = true
        let client = client
        pendingSearch = Task { [weak self] in
            do {
                try await Task.sleep(nanoseconds: 180_000_000)
                try Task.checkCancellation()
                // api.ts: searchNotes(query, limit) — title hits first, server-side.
                let response = try await client.searchNotes(query: query)
                try Task.checkCancellation()
                guard let self else { return }
                self.results = response.results
                self.unavailableCount = response.unavailable?.count ?? 0
                self.isLoading = false
            } catch is CancellationError {
                // A newer keystroke owns the next request.
            } catch {
                guard let self, !Task.isCancelled else { return }
                self.error = error.localizedDescription
                self.results = []
                self.unavailableCount = 0
                self.isLoading = false
            }
        }
    }
}
