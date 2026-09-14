import AppKit
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
    /// the read report address. Kept until the next open succeeds.
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
    public private(set) var dayNotes: [NoteRef] = []
    public private(set) var dayNotesError: String?
    public private(set) var isLoadingDayNotes = false

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
    /// the draft and its original etag are retained; the edit was NOT applied.
    public private(set) var saveConflict: String?

    /// The API client the reader hands down to the GFM renderer (viewspec
    /// resolution) and every read/write path.
    public let client: TrackClient

    private var generation = 0
    private var navigationDraft = ""
    public private(set) var isOpening = false
    private let confirmDiscard: @MainActor () -> Bool

    public init(client: TrackClient, confirmDiscard: (@MainActor () -> Bool)? = nil) {
        self.client = client
        self.confirmDiscard = confirmDiscard ?? {
            let alert = NSAlert()
            alert.messageText = "Discard unsaved edits?"
            alert.informativeText = "Your unsaved changes will be lost."
            alert.addButton(withTitle: "Keep editing")
            alert.addButton(withTitle: "Discard")
            return alert.runModal() == .alertSecondButtonReturn
        }
    }

    /// All navigation and window-close paths use this boundary. Keep the
    /// buffer until navigation succeeds, including when the network fails.
    public private(set) var isWritingTask = false

    public func authorizeDiscard() -> Bool {
        guard !isSaving, !isDeleting, !isSavingMeta, !isCreating, !isWritingTask else { return false }
        return !isDirty || confirmDiscard()
    }

    private func beginNavigation() -> Int? {
        guard authorizeDiscard() else { return nil }
        generation += 1
        navigationDraft = draftBody
        isOpening = true
        return generation
    }

    private func acceptsNavigation(_ token: Int) -> Bool {
        token == generation && draftBody == navigationDraft && !Task.isCancelled
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

    @discardableResult
    public func open(_ id: TrackID) async -> Bool {
        guard let token = beginNavigation() else { return false }
        return await load(id, token: token)
    }

    private func load(_ id: TrackID, token: Int, excerpt: String? = nil) async -> Bool {
        defer { if token == generation { isOpening = false } }
        if !isLoaded { state = .loading }
        do {
            let response = try await client.getNote(id)
            guard acceptsNavigation(token) else { return false }
            let render = try? await client.renderMarkdown(body: response.note.body, vault: id.split().vault)
            guard acceptsNavigation(token) else { return false }
            currentID = id
            draftBody = response.note.body
            isEditing = false
            saveError = nil
            saveConflict = nil
            renderedBody = render?.markdown ?? ""
            renderedIncludes = render?.includes
            didRender = render != nil
            anchoredExcerpt = excerpt
            state = .loaded(response)
            ReadingStore.shared.adopt([response.note.summary.ref] + response.backlinks)
            return true
        } catch {
            guard acceptsNavigation(token) else { return false }
            if isLoaded { saveError = Self.message(for: error) }
            else { state = .failed(error.localizedDescription) }
            return false
        }
    }

    // MARK: - Editing

    /// Enter the editor seeded from the loaded body.
    public func beginEditing() {
        guard isLoaded, !isEditing, !isOpening, !isWritingTask else { return }
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

    /// Close the open note (web "close tab" → empty reader): clears the id,
    /// buffers and write state so the detail falls back to the start page.
    @discardableResult
    public func close() -> Bool {
        guard authorizeDiscard() else { return false }
        generation += 1
        isOpening = false
        state = .empty
        currentID = nil
        renderedBody = ""
        renderedIncludes = nil
        didRender = false
        anchoredExcerpt = nil
        draftBody = ""
        isEditing = false
        saveError = nil
        saveConflict = nil
        return true
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
    /// body. A 409 retains the draft and its baseline etag, so retrying cannot
    /// overwrite the external change without an explicit reload and merge.
    public func saveDraft() async {
        guard isDirty, !isSaving, !isOpening, let id = currentID else { return }
        let submittedBody = draftBody
        let token = generation
        isSaving = true
        saveError = nil
        saveConflict = nil
        defer { isSaving = false }
        do {
            _ = try await client.saveNote(id: id, body: submittedBody, etag: loadedEtag)
            let fresh = try await client.getNote(id)
            guard token == generation, currentID == id else { return }
            if draftBody == submittedBody { draftBody = fresh.note.body }
            state = .loaded(fresh)
            await render(fresh.note.body, id: id)
        } catch let api as APIError where api.status == 409 {
            // Keep the original etag: retrying must never silently overwrite
            // the external change that caused this conflict.
            saveConflict = "This note changed on disk. Your edits are kept. Copy them before reloading and merging the latest version."
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
        guard case .loaded(let response) = state, !isDeleting, !isSaving, !isOpening, let id = currentID else { return false }
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
            generation += 1
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
    public func createNote(title: String, vault: String = "") async -> Bool {
        let trimmed = title.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else {
            saveError = "Enter a title to create the note."
            return false
        }
        guard !isCreating, let token = beginNavigation() else { return false }
        isCreating = true
        saveError = nil
        defer {
            isCreating = false
            if token == generation { isOpening = false }
        }
        do {
            let created = try await client.createNote(title: trimmed, vault: vault)
            guard acceptsNavigation(token) else { return false }
            return await load(created.noteID, token: token)
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
        guard !isSavingMeta, !isOpening, let id = currentID else { return false }
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
        ReadingStore.shared.markSeen(id.raw)
        _ = try? await client.markRead(id: id, event: .seen)
    }

    /// A journal's activity belongs to its own date and vault, not today's date.
    public func loadDayNotes() async {
        dayNotes = []
        dayNotesError = nil
        isLoadingDayNotes = false
        guard let id = currentID, case .loaded(let response) = state,
              let day = Self.journalDate(response.note.summary.ref) else { return }
        let token = generation
        isLoadingDayNotes = true
        defer { if token == generation { isLoadingDayNotes = false } }
        do {
            let agenda = try await client.getAgenda(date: day, vault: id.split().vault)
            guard token == generation, currentID == id, !Task.isCancelled else { return }
            dayNotes = agenda.notes.filter { $0.noteID != response.note.summary.ref.noteID }
            ReadingStore.shared.adopt(dayNotes)
        } catch {
            guard token == generation, currentID == id, !Task.isCancelled else { return }
            dayNotesError = Self.message(for: error)
        }
    }

    public static func journalDate(_ note: NoteRef) -> String? {
        let raw = note.noteID.split().id
        guard note.fileKind == "journal", raw.utf8.count == 8,
              raw.utf8.allSatisfy({ (48...57).contains($0) }) else { return nil }
        return "\(raw.prefix(4))-\(raw.dropFirst(4).prefix(2))-\(raw.suffix(2))"
    }

    /// Accumulate visible reading time and report the read milestone when the
    /// shared threshold is crossed. This is the reader-facing canonical API:
    /// views only need to pass their `ReadingStore`, tick duration, and body.
    /// The local store remains monotonic, and the network report is
    /// best-effort just like `reportSeen()`.
    @discardableResult
    public func recordView(using reading: ReadingStore, seconds: TimeInterval, text: String) async -> Bool {
        guard let id = currentID else { return false }
        let crossed = reading.recordView(id.raw, seconds: seconds, text: text)
        if crossed { _ = try? await client.markRead(id: id, event: .read) }
        return crossed
    }

    // MARK: - Tasks

    public func setTaskState(line: Int, to newState: String) async {
        await writeTask(line: line) { id, expect, etag in
            try await self.client.setTaskState(id: id, line: line, state: newState, expect: expect, etag: etag)
        }
    }

    public func setTaskDate(line: Int, field: DateField, date: String) async {
        await writeTask(line: line) { id, expect, etag in
            try await self.client.setTaskDate(id: id, line: line, field: field, date: date, expect: expect, etag: etag)
        }
    }

    private func writeTask(line: Int, mutation: (TrackID, String, String) async throws -> TasksResponse) async {
        guard let id = currentID, !isDirty, !isSaving, !isOpening, !isWritingTask,
              case .loaded(let response) = state,
              let task = response.note.tasks?.items.first(where: { $0.line == line }) else { return }
        isWritingTask = true
        defer { isWritingTask = false }
        saveError = nil
        saveConflict = nil
        do {
            _ = try await mutation(id, task.state, response.note.etag)
            // A task response has no body. Adopting only its etag would let
            // later editing overwrite the task with the old body and a fresh token.
            await refreshOpenNote()
            NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
        } catch let api as APIError where api.status == 409 {
            saveConflict = "Note changed underneath — change was not applied"
            await refreshOpenNote()
        } catch {
            saveError = Self.message(for: error)
        }
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
        let token = generation
        if let render = try? await client.renderMarkdown(body: body, vault: vault) {
            guard token == generation, currentID == id, loadedBody == body else { return }
            renderedBody = render.markdown
            renderedIncludes = render.includes
            didRender = true
        }
    }

    /// Re-fetch the open note after a write, adopting the fresh body into the
    /// draft only when the buffer is clean. Dirty buffers keep their baseline
    /// etag too, so external changes still conflict on the next save. Best
    /// effort: the write itself succeeded, so a failed refresh keeps the
    /// last-known state rather than failing the note.
    public func refreshOpenNote() async {
        guard let id = currentID, !isDirty, !isSaving, !isOpening else { return }
        generation += 1
        let token = generation
        do {
            let fresh = try await client.getNote(id)
            guard token == generation, currentID == id, !isDirty, !isSaving, !isOpening else { return }
            draftBody = fresh.note.body
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
        await open(ref.noteID.raw.contains("~") ? ref.noteID : TrackID.qualify(vault: currentID?.split().vault ?? "", id: ref.noteID.raw))
    }

    /// Resolve and open a `[[wikilink]]` target. The target grammar
    /// (`web/src/components/markdown/plugins.ts: splitWikiTarget`) allows an
    /// optional leading `vault:` and a trailing `#anchor`/`#^block`; the
    /// anchor is stripped for resolution (it names a destination inside the
    /// note, not a different note), but kept to compute the anchored excerpt
    /// shown above the reader. Cross-vault targets resolve through the same
    /// `/api/resolve` the web reader uses.
    public func openWikilink(target: String) async {
        guard let token = beginNavigation() else { return }
        defer { if token == generation { isOpening = false } }
        let parsed = Self.splitWikilinkFull(target)
        let vault = parsed.vault.isEmpty ? (currentID?.split().vault ?? "") : parsed.vault
        do {
            let resolved = try await client.resolveTerm(parsed.term, vault: vault)
            guard acceptsNavigation(token), resolved.found else { return }
            let id = TrackID.qualify(vault: vault, id: resolved.note.noteID.raw)
            let excerpt = parsed.anchor.isEmpty ? nil : await Self.anchoredExcerpt(for: parsed.anchor, in: id, client: client)
            guard acceptsNavigation(token) else { return }
            _ = await load(id, token: token, excerpt: excerpt)
        } catch {
            guard acceptsNavigation(token) else { return }
            saveError = Self.message(for: error)
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
    /// per-vault "vault … could not be searched" note. The count stays as a
    /// convenience for the banner; the list carries names and errors.
    public private(set) var unavailable: [UnavailableVault] = []
    public var unavailableCount: Int { unavailable.count }
    /// Recently submitted/opened search terms, newest first. Views may use
    /// this for a history menu without owning another persistence cache.
    public private(set) var history: [String]
    /// Set by keyboard/menu commands and consumed by the search field view.
    public private(set) var searchFocusRequested = false
    private let client: TrackClient
    private var pendingSearch: Task<Void, Never>?
    private let defaults: UserDefaults
    private static let historyKey = "track.search.history"
    private static let historyLimit = 20

    public init(client: TrackClient, defaults: UserDefaults = .standard) {
        self.client = client
        self.defaults = defaults
        self.history = defaults.stringArray(forKey: Self.historyKey) ?? []
    }

    public func addToHistory(_ query: String) {
        let value = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else { return }
        history.removeAll { $0.caseInsensitiveCompare(value) == .orderedSame }
        history.insert(value, at: 0)
        if history.count > Self.historyLimit { history.removeLast(history.count - Self.historyLimit) }
        defaults.set(history, forKey: Self.historyKey)
    }

    public func clearHistory() {
        history.removeAll()
        defaults.removeObject(forKey: Self.historyKey)
    }

    /// Requests focus for the `/` shortcut; the view consumes the edge.
    public func requestSearchFocus() { searchFocusRequested = true }

    /// Returns whether a request was pending and clears it atomically.
    @discardableResult
    public func consumeSearchFocusRequest() -> Bool {
        guard searchFocusRequested else { return false }
        searchFocusRequested = false
        return true
    }

    /// Starts a live search. Keeping the debounce task here (rather than in the
    /// view) also makes every search entry point share the same cancellation
    /// and stale-response behaviour.
    public func search(query: String, vault: String = "") {
        pendingSearch?.cancel()
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        error = nil
        guard !query.isEmpty else {
            results = []
            unavailable = []
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
                let response = try await client.searchNotes(query: query, vault: vault)
                try Task.checkCancellation()
                guard let self else { return }
                self.results = response.results
                ReadingStore.shared.adopt(response.results.map(\.ref))
                self.unavailable = response.unavailable ?? []
                self.isLoading = false
            } catch is CancellationError {
                // A newer keystroke owns the next request.
            } catch {
                guard let self, !Task.isCancelled else { return }
                self.error = error.localizedDescription
                self.results = []
                self.unavailable = []
                self.isLoading = false
            }
        }
    }
}
