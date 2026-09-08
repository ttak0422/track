import AppKit
import SwiftUI
import TrackAPI

// MVP reader: title, native-rendered Markdown body, backlink list.
// Design tokens follow docs/spec/design.md (ink + hairlines; the single
// salient `--mark` only for the active state). Rich blocks the MVP does not
// draw (math, diagrams, maps, decks) arrive as source text — a placeholder
// treatment is a follow-up, not a silent drop.

/// One entry of the sidebar's recently-opened list: a qualified note id plus
/// the title shown for it (persisted under a single AppStorage key).
private struct RecentNote: Codable {
    var id: String
    var title: String
}

// MARK: - Search + reader shell

public struct SearchReaderView: View {
    private static let recentVisibleLimit = 10
    private static let recentStorageLimit = 100

    @State private var search: SearchModel
    @State private var reader: NoteReaderModel
    @State private var query = ""
    @State private var activeSearchIndex = -1
    /// Title typed in the New-note sheet.
    @State private var newNoteTitle = ""
    @State private var showNewNote = false
    /// Duplicate-title notice from a refused create (web "A note with the same
    /// title already exists"), shown inline in the sheet instead of dismissing.
    @State private var newNoteError: String?
    /// The API base URL, kept so the reader can hand it to the GFM renderer
    /// (which builds `/api/asset?...` URLs from it).
    private let baseURL: URL
    /// Recently opened notes (most-recent first), persisted under one key.
    @AppStorage("track.recentNotes") private var recentJSON = "[]"
    /// Local read-state mirror, so NEW badges draw without a server round-trip
    /// (reader-backed, mirroring web/src/reading.ts).
    @State private var reading = ReadingStore()

    public init(client: TrackClient) {
        _search = State(initialValue: SearchModel(client: client))
        _reader = State(initialValue: NoteReaderModel(client: client))
        baseURL = client.baseURL
    }

    private var recentList: [RecentNote] {
        (try? JSONDecoder().decode([RecentNote].self, from: Data(recentJSON.utf8))) ?? []
    }

    /// Vault names only become useful when the MRU contains notes from more
    /// than one vault. The empty vault is kept as a distinct value so a
    /// qualified note is labelled when it sits beside an unqualified one.
    private var hasMultipleRecentVaults: Bool {
        Set(recentList.map { TrackID($0.id).split().vault }).count > 1
    }

    /// A minimal NoteRef for an MRU entry so a NEW badge can be decided against
    /// the local read-state mirror; nil when the entry cannot be represented
    /// (it then shows no NEW badge). Rebuilt through JSON (NoteRef exposes no
    /// memberwise init), with nil milestones so NEW is decided by the local
    /// seen/read sets alone.
    private static func mruRef(_ note: RecentNote) -> NoteRef? {
        let raw = TrackID(note.id).split().id
        let object: [String: Any] = ["note_id": raw, "file_kind": "", "title": note.title]
        guard let data = try? JSONSerialization.data(withJSONObject: object) else { return nil }
        return try? JSONDecoder().decode(NoteRef.self, from: data)
    }

    private func recordRecent(_ note: RecentNote) {
        var list = recentList.filter { $0.id != note.id }
        list.insert(note, at: 0)
        list = Array(list.prefix(Self.recentStorageLimit))
        if let data = try? JSONEncoder().encode(list) {
            recentJSON = String(decoding: data, as: UTF8.self)
        }
    }

    public var body: some View {
        NavigationSplitView {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 6) {
                    TextField("Search", text: $query)
                        .textFieldStyle(.plain)
                        .onChange(of: query) { _, value in
                            activeSearchIndex = -1
                            search.search(query: value)
                        }
                        .onKeyPress { press in
                            switch press.key {
                            case .upArrow:
                                moveSearchSelection(by: -1)
                                return .handled
                            case .downArrow:
                                moveSearchSelection(by: 1)
                                return .handled
                            case .escape:
                                query = ""
                                return .handled
                            case .return:
                                chooseActiveSearchResult()
                                return .handled
                            default:
                                return .ignored
                            }
                        }
                        .onSubmit { chooseActiveSearchResult() }
                    Button {
                        newNoteError = nil
                        newNoteTitle = ""
                        showNewNote = true
                    } label: {
                        Image(systemName: "plus")
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel("New note")
                    .help("New note")
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                Divider()
                Group {
                    if search.isLoading {
                        ProgressView().frame(maxWidth: .infinity)
                    } else if let error = search.error {
                        Text(error).font(.caption).foregroundStyle(.red).padding(8)
                    } else if search.unavailableCount > 0 {
                        Text("⚠ \(search.unavailableCount) vault\(search.unavailableCount == 1 ? "" : "s") could not be searched")
                            .font(.caption).foregroundStyle(.secondary).padding(8)
                    }
                }
                if query.isEmpty && !recentList.isEmpty {
                    Text("Recent").font(.caption).foregroundStyle(.secondary)
                        .padding(.horizontal, 12).padding(.top, 8)
                    let visibleRecent = Array(recentList.prefix(Self.recentVisibleLimit))
                    let overflowRecent = Array(recentList.dropFirst(Self.recentVisibleLimit))
                    ForEach(visibleRecent, id: \.id) { note in
                        Button {
                            recordRecent(note)
                            reading.markSeen(TrackID(note.id).split().id)
                            Task { await reader.open(TrackID(note.id)) }
                        } label: {
                            HStack(spacing: 6) {
                                Image(systemName: "clock").font(.caption).foregroundStyle(.tertiary)
                                if hasMultipleRecentVaults {
                                    let vault = TrackID(note.id).split().vault
                                    if !vault.isEmpty {
                                        Text(vault)
                                            .font(.caption2)
                                            .foregroundStyle(.tertiary)
                                            .lineLimit(1)
                                    }
                                }
                                Text(note.title).font(.body).lineLimit(1)
                                if note.id == reader.currentID?.raw && reader.isDirty {
                                    Text("•")
                                        .font(.title3)
                                        .foregroundStyle(Color.accentColor)
                                        .accessibilityLabel("Unsaved changes")
                                }
                                if let ref = Self.mruRef(note), reading.isNew(ref) {
                                    Text("NEW")
                                        .font(.caption2).fontWeight(.bold)
                                        .foregroundStyle(.secondary)
                                        .padding(.horizontal, 4).padding(.vertical, 1)
                                        .background(.quaternary, in: Capsule())
                                }
                            }
                        }
                        .buttonStyle(.plain)
                        .padding(.horizontal, 12).padding(.vertical, 2)
                    }
                    if !overflowRecent.isEmpty {
                        Menu {
                            ForEach(overflowRecent, id: \.id) { note in
                                Button {
                                    recordRecent(note)
                                    reading.markSeen(TrackID(note.id).split().id)
                                    Task { await reader.open(TrackID(note.id)) }
                                } label: {
                                    HStack {
                                        if hasMultipleRecentVaults {
                                            let vault = TrackID(note.id).split().vault
                                            if !vault.isEmpty { Text("\(vault) ·") }
                                        }
                                        Text(note.title)
                                        if note.id == reader.currentID?.raw && reader.isDirty {
                                            Text("•")
                                        }
                                    }
                                }
                            }
                        } label: {
                            Text("+\(overflowRecent.count) more")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .menuStyle(.borderlessButton)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 2)
                    }
                    Divider().padding(.top, 6)
                }
                List(filteredSearchResults, id: \.qualifiedID) { result in
                    Button {
                        recordRecent(RecentNote(id: result.qualifiedID.raw, title: result.ref.title))
                        reading.markSeen(result.ref.noteID.raw)
                        Task { await reader.open(result.qualifiedID) }
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            HStack(spacing: 6) {
                                Text(result.ref.title).font(.body)
                                if reading.isNew(result.ref) {
                                    Text("NEW")
                                        .font(.caption2).fontWeight(.bold)
                                        .foregroundStyle(.secondary)
                                        .padding(.horizontal, 4).padding(.vertical, 1)
                                        .background(.quaternary, in: Capsule())
                                }
                            }
                            if let match = result.match {
                                Text(match).font(.caption2).foregroundStyle(.tertiary)
                            }
                            if let snippet = result.snippet {
                                Text(snippet).font(.caption).foregroundStyle(.secondary)
                                    .lineLimit(2)
                            }
                            if let tags = result.tags, !tags.isEmpty {
                                Text(tags.map { "#\($0)" }.joined(separator: " "))
                                    .font(.caption2).foregroundStyle(.tertiary)
                            }
                        }
                    }
                    .buttonStyle(.plain)
                    .listRowBackground(activeSearchIndex == filteredSearchResults.firstIndex(where: { $0.qualifiedID == result.qualifiedID }) ? Color.primary.opacity(0.08) : nil)
                }
            }
            .navigationTitle("track")
        } detail: {
            NoteReaderView(model: reader, baseURL: baseURL)
        }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard !query.isEmpty, !search.isLoading else { return }
            search.search(query: query)
        }
        .sheet(isPresented: $showNewNote) {
            NewNoteSheet(
                title: $newNoteTitle,
                error: $newNoteError,
                isCreating: reader.isCreating,
                onCreate: { title in
                    Task {
                        if await reader.createNote(title: title) {
                            newNoteError = nil
                            newNoteTitle = ""
                            showNewNote = false
                        }
                    }
                }
            )
        }
    }

    private var filteredSearchResults: [SearchResult] {
        let tags = query.split(whereSeparator: { $0 == " " || $0 == "\n" })
            .compactMap { token -> String? in
                guard token.first == "#", token.count > 1 else { return nil }
                return String(token.dropFirst()).lowercased()
            }
        guard !tags.isEmpty else { return search.results }
        return search.results.filter { result in
            let resultTags = (result.tags ?? []).map { $0.lowercased() }
            return tags.allSatisfy { tag in
                resultTags.contains { $0 == tag || $0.hasPrefix(tag + "/") }
            }
        }
    }

    private func moveSearchSelection(by offset: Int) {
        guard !filteredSearchResults.isEmpty else {
            activeSearchIndex = -1
            return
        }
        let next = activeSearchIndex < 0 ? (offset > 0 ? 0 : filteredSearchResults.count - 1) : activeSearchIndex + offset
        activeSearchIndex = (next + filteredSearchResults.count) % filteredSearchResults.count
    }

    private func chooseActiveSearchResult() {
        guard !filteredSearchResults.isEmpty else { return }
        let index = activeSearchIndex >= 0 ? activeSearchIndex : 0
        guard index < filteredSearchResults.count else { return }
        let result = filteredSearchResults[index]
        recordRecent(RecentNote(id: result.qualifiedID.raw, title: result.ref.title))
        reading.markSeen(result.ref.noteID.raw)
        Task { await reader.open(result.qualifiedID) }
    }
}

// MARK: - New-note sheet

/// Title-entry sheet behind the sidebar "+", wired to `createNote` (web
/// VoiceView createCandidate → createNote): Create mints and opens the note,
/// then dismisses and clears the field; a refused title (409 duplicate) keeps
/// the sheet open and shows the duplicate notice inline.
private struct NewNoteSheet: View {
    @Binding var title: String
    @Binding var error: String?
    let isCreating: Bool
    let onCreate: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("New note").font(.headline)
            TextField("Note title", text: $title)
                .textFieldStyle(.roundedBorder)
                .onSubmit {
                    if !title.trimmingCharacters(in: .whitespaces).isEmpty, !isCreating {
                        onCreate(title)
                    }
                }
            if let error {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button {
                    onCreate(title)
                } label: {
                    if isCreating {
                        ProgressView().controlSize(.small)
                    } else {
                        Text("Create")
                    }
                }
                .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty || isCreating)
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .frame(width: 340)
    }
}

// MARK: - Note detail

/// The two panes of the in-note editor (web `editorMode` edit/preview; split
/// is not drawn natively). Read-only mode is the default when not editing.
private enum NoteEditorPane {
    case edit
    case preview
    case split
}

public struct NoteReaderView: View {
    @Bindable var model: NoteReaderModel
    @AppStorage(TrackAppearance.contentWidthKey) private var contentWidthRaw: String?
    /// The API base URL, passed down to the GFM renderer for `assets/…` embeds.
    let baseURL: URL
    /// Edit vs Preview inside the editor pane; reset to Edit each time an
    /// editing session starts.
    @State private var pane = NoteEditorPane.edit
    /// Dirty guard: Done with unsaved edits asks before discarding the draft.
    @State private var confirmDiscard = false
    /// Delete confirmation: the user must retype the title before the note can
    /// be removed (web confirmDelete's GitHub-style retype). A sheet, because a
    /// SwiftUI `.alert` cannot host a text field.
    @State private var showDeleteConfirm = false
    /// Note metadata editor (web NoteMetaDialog), bound to the open note.
    @State private var showMeta = false

    public init(model: NoteReaderModel, baseURL: URL) {
        self.model = model
        self.baseURL = baseURL
    }

    public var body: some View {
        Group {
            switch model.state {
            case .empty:
                ContentUnavailableView("No note open", systemImage: "doc.text")
            case .loading:
                ProgressView()
            case .failed(let message):
                ContentUnavailableView("Could not open note", systemImage: "exclamationmark.triangle", description: Text(message))
            case .loaded(let response):
                if model.isEditing {
                    editorPane(response)
                } else {
                    readerPane(response)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .toolbar { loadedToolbar }
        .overlay(alignment: .top) {
            if model.saveConflict != nil || model.saveError != nil {
                VStack(spacing: 6) {
                    if let conflict = model.saveConflict {
                        readerBanner(conflict, isError: false) { model.dismissConflict() }
                    }
                    if let error = model.saveError {
                        readerBanner(error, isError: true) { model.dismissSaveError() }
                    }
                }
                .padding(.horizontal, 12)
                .padding(.top, 8)
                .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .task(id: model.currentID) {
            // Read reporting: each time a note finishes loading, report the
            // "seen" milestone (reading.ts markSeen). Fire-and-forget.
            await model.reportSeen()
        }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard let id = model.currentID, !model.isEditing else { return }
            Task { await model.open(id) }
        }
        .alert("Discard unsaved edits?", isPresented: $confirmDiscard) {
            Button("Discard", role: .destructive) { model.discardDraft() }
            Button("Keep editing", role: .cancel) {}
        } message: {
            Text("Your unsaved edits will be lost.")
        }
        .sheet(isPresented: $showMeta) {
            if model.isLoaded {
                NoteMetaEditor(model: model) {
                    showMeta = false
                }
            }
        }
        .sheet(isPresented: $showDeleteConfirm) {
            if let response = loadedResponse {
                DeleteNoteSheet(model: model, note: response.note) {
                    showDeleteConfirm = false
                }
            }
        }
    }

    /// One-line notice drawn at the top of the reader: the read/conflict notice
    /// (non-error) or a write failure (red), each dismissible (TasksView Banner
    /// parity, extended for the reader's darker background).
    private func readerBanner(_ message: String, isError: Bool, onDismiss: @escaping () -> Void) -> some View {
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
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    /// Toolbar for a loaded note: Copy path (when the note has one), the
    /// actions menu (Meta… / Delete…), and the Save or Edit/Done toggle.
    /// Nothing is offered in the other states.
    @ToolbarContentBuilder
    private var loadedToolbar: some ToolbarContent {
        if let note = loadedNote {
            if let path = note.copyPath {
                ToolbarItem {
                    Button {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(path, forType: .string)
                    } label: {
                        Label("Copy path", systemImage: "doc.on.doc")
                    }
                    .help("Copy note path")
                }
            }
            ToolbarItem {
                ShareButton(items: [note.summary.ref.title, note.body])
                    .help("Share")
            }
            ToolbarItem {
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(note.body, forType: .string)
                } label: {
                    Label("Copy body", systemImage: "doc.on.clipboard")
                }
                .help("Copy note body")
            }
            ToolbarItem {
                if model.isEditing {
                    Button("Done") { finishEditing() }
                } else {
                    Button("Edit") { startEditing() }
                }
            }
            ToolbarItem {
                Menu {
                    Button("Meta…") { showMeta = true }
                    Divider()
                    Button("Delete…", role: .destructive) { showDeleteConfirm = true }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .help("Note actions")
            }
            ToolbarItem {
                if model.isEditing {
                    saveButton
                }
            }
        }
    }

    /// The Save control shown while editing (web editor-actions primary
    /// button): enabled only while the draft is dirty and not already saving,
    /// and replaced by a progress spinner while a save is in flight.
    @ViewBuilder
    private var saveButton: some View {
        if model.isSaving {
            ProgressView()
                .controlSize(.small)
                .accessibilityLabel("Saving")
        } else {
            Button("Save") { Task { await model.saveDraft() } }
                .disabled(!model.isDirty)
                .keyboardShortcut("s", modifiers: [.command])
        }
    }

    private var loadedNote: NoteDetail? {
        if case .loaded(let response) = model.state { return response.note }
        return nil
    }

    private var loadedResponse: NoteResponse? {
        if case .loaded(let response) = model.state { return response }
        return nil
    }

    private var contentWidthMode: ContentWidthMode {
        ContentWidthMode(stored: contentWidthRaw)
    }

    /// Intercepts link taps inside the GFM body: `trackwiki://` links (produced
    /// by GFMBody's `[[wikilink]]` rewrite) navigate to the target note via
    /// `openWikilink`, everything else falls through to the system handler.
    /// The target is the URL's host + path percent-decoded — `rewriteWikilinks`
    /// percent-encodes `#`, `:` and non-ASCII with `.urlPathAllowed`, so
    /// `[[title#anchor]]` and `[[vault:title]]` survive the round trip.
    private var wikilinkURLAction: OpenURLAction {
        OpenURLAction { url in
            if url.scheme?.lowercased() == "trackwiki" {
                let combined = (url.host ?? "") + url.path
                let target = combined.removingPercentEncoding ?? combined
                if !target.isEmpty {
                    Task { await model.openWikilink(target: target) }
                    return .handled
                }
            }
            return .systemAction
        }
    }

    private func startEditing() {
        pane = .edit
        model.beginEditing()
    }

    private func finishEditing() {
        if model.isDirty {
            confirmDiscard = true
        } else {
            model.discardDraft()
        }
    }

    // MARK: - Read mode

    @ViewBuilder
    private func readerPane(_ response: NoteResponse) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                noteHeader(response.note)

                if let excerpt = model.anchoredExcerpt {
                    anchoredExcerptCard(excerpt)
                }

                GFMBody(
                    markdown: model.didRender ? model.renderedBody : response.note.body,
                    baseURL: baseURL,
                    vault: model.currentID?.split().vault ?? "",
                    includes: model.didRender ? model.renderedIncludes : nil,
                    client: model.client,
                    onWikilink: { target in Task { await model.openWikilink(target: target) } }
                )
                .environment(\.openURL, wikilinkURLAction)

                if let tasks = response.note.tasks, !tasks.items.isEmpty {
                    NoteTasksSection(
                        tasks: tasks.items,
                        onCycle: { line in
                            let current = tasks.items.first { $0.line == line }?.state ?? "TODO"
                            Task { await model.setTaskState(line: line, to: nextTaskState(after: current)) }
                        }
                    )
                }

                // Hierarchy and cross-vault sections, mirroring
                // NoteReaderStatic's trail/children/external/unavailable.
                if let trail = response.trail, !trail.isEmpty {
                    Divider()
                    Text("Trail").font(.caption).foregroundStyle(.secondary)
                    ForEach(trail, id: \.noteID) { ref in
                        Button(ref.title) { Task { await model.openRef(ref) } }
                            .buttonStyle(.link)
                    }
                }
                if let children = response.children, !children.isEmpty {
                    Divider()
                    Text("Children").font(.caption).foregroundStyle(.secondary)
                    ForEach(children, id: \.noteID) { ref in
                        Button(ref.title) { Task { await model.openRef(ref) } }
                            .buttonStyle(.link)
                    }
                }
                if let external = response.external, !external.isEmpty {
                    Divider()
                    Text("Linked from other vaults").font(.caption).foregroundStyle(.secondary)
                    ForEach(external, id: \.noteID) { ref in
                        Button("\(ref.vault)/\(ref.title)") {
                            Task { await model.open(TrackID.qualify(vault: ref.vault, id: ref.noteID.raw)) }
                        }
                        .buttonStyle(.link)
                    }
                }
                if let unavailable = response.unavailable, !unavailable.isEmpty {
                    ForEach(unavailable, id: \.name) { vault in
                        Text("⚠ vault “\(vault.name)” could not be checked\(vault.error.map { ": \($0)" } ?? "")")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                if !response.backlinks.isEmpty {
                    Divider()
                    Text("Backlinks")
                        .font(.caption).foregroundStyle(.secondary)
                    ForEach(response.backlinks, id: \.noteID) { ref in
                        Button(ref.title) {
                            Task { await model.openRef(ref) }
                        }
                        .buttonStyle(.link)
                    }
                }
            }
            .frame(maxWidth: contentWidthMode.maxWidth, alignment: .leading)
            .padding(24)
        }
    }

    // MARK: - Edit mode

    /// Editor pane: shared note header, an Edit/Preview switch, and the draft
    /// body as a monospaced TextEditor or a rendered preview. The Save control
    /// lives in the toolbar; Done after an unsaved edit asks before discarding
    /// the draft, and opening another note discards it.
    @ViewBuilder
    private func editorPane(_ response: NoteResponse) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            noteHeader(response.note)
            Divider()
            HStack(spacing: 12) {
                Picker("Pane", selection: $pane) {
                    Text("Edit").tag(NoteEditorPane.edit)
                    Text("Preview").tag(NoteEditorPane.preview)
                    Text("Split").tag(NoteEditorPane.split)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(maxWidth: 280)
                if model.isDirty {
                    Text("Edited — unsaved changes")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            switch pane {
            case .edit:
                TextEditor(text: $model.draftBody)
                    .font(.system(.body, design: .monospaced))
                    .frame(minHeight: 300)
            case .preview:
                ScrollView {
                    GFMBody(
                        markdown: model.draftBody,
                        baseURL: baseURL,
                        vault: model.currentID?.split().vault ?? "",
                        onWikilink: { target in Task { await model.openWikilink(target: target) } }
                    )
                    .environment(\.openURL, wikilinkURLAction)
                    .frame(maxWidth: contentWidthMode.maxWidth, alignment: .leading)
                }
            case .split:
                HSplitView {
                    TextEditor(text: $model.draftBody)
                        .font(.system(.body, design: .monospaced))
                        .frame(minWidth: 240, minHeight: 300)
                    ScrollView {
                        GFMBody(
                            markdown: model.draftBody,
                            baseURL: baseURL,
                            vault: model.currentID?.split().vault ?? "",
                            onWikilink: { target in Task { await model.openWikilink(target: target) } }
                        )
                        .environment(\.openURL, wikilinkURLAction)
                        .frame(maxWidth: contentWidthMode.maxWidth, alignment: .leading)
                        .padding(.horizontal, 12)
                    }
                    .frame(minWidth: 240, minHeight: 300)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .padding(24)
    }

    // MARK: - Shared header

    /// Title, tags and flags (ADR 0074) above the body — kept from the MVP —
    /// followed by read-only captions mirroring the web NoteProperties strip:
    /// typed props (`key: value`, capped at 10), created/updated, and the task
    /// count when the engine parsed tasks out of the body.
    @ViewBuilder
    private func noteHeader(_ note: NoteDetail) -> some View {
        Text(note.summary.ref.title)
            .font(.title2).fontWeight(.medium)

        if let tags = note.summary.tags, !tags.isEmpty {
            HStack(spacing: 8) {
                ForEach(tags, id: \.self) { tag in
                    Text("#\(tag)").font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        if let flags = note.summary.ref.flags, !flags.isEmpty {
            HStack(spacing: 8) {
                ForEach(flags, id: \.self) { flag in
                    Text(flag)
                        .font(.caption).fontWeight(.medium)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 6).padding(.vertical, 2)
                        .background(.quaternary, in: Capsule())
                }
            }
        }

        if let meta = metadataCaptionLines(note), !meta.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(Array(meta.enumerated()), id: \.offset) { _, line in
                    Text(line)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
            }
        }
    }

    /// Caption lines for a note's typed props (max 10), then created/updated,
    /// then the task count. The `up` link prop is skipped — the trail section
    /// already draws the hierarchy, so it is not repeated here (web
    /// NoteProperties filters the same pair). `created` is shown verbatim (the
    /// vault's own date format); `updated` is the file mtime rendered at day
    /// precision like the web reader (noteShared NoteProperties → dateKey).
    private func metadataCaptionLines(_ note: NoteDetail) -> [String]? {
        var lines: [String] = []
        if let props = note.props {
            let shown = props.filter { !($0.key == "up" && $0.type == "link") }
            for prop in shown.prefix(10) {
                lines.append("\(prop.key): \(prop.value)")
            }
        }
        if let created = note.created {
            lines.append("created \(created)")
        }
        if let updated = note.updated {
            lines.append("updated \(Self.dayString(updated))")
        }
        if let tasks = note.tasks, !tasks.items.isEmpty {
            lines.append("\(tasks.items.count) task\(tasks.items.count == 1 ? "" : "s")")
        }
        return lines.isEmpty ? nil : lines
    }

    private static func dayString(_ unixSeconds: Int) -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.locale = Locale(identifier: "en_US_POSIX")
        return formatter.string(from: Date(timeIntervalSince1970: TimeInterval(unixSeconds)))
    }

    /// A dismissible card showing the anchored excerpt a `[[Note#heading]]` /
    /// `[[Note#^block]]` tap landed on (the MarkdownUI body offers no in-body
    /// scroll target, so the excerpt is surfaced here instead). Dismissing
    /// clears the model's `anchoredExcerpt` so it does not reappear.
    private func anchoredExcerptCard(_ excerpt: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: "scope")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text("Excerpt")
                    .font(.caption).fontWeight(.medium)
                    .foregroundStyle(.secondary)
                Spacer()
                Button {
                    model.anchoredExcerpt = nil
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Dismiss excerpt")
            }
            Text(excerpt)
                .font(.body)
                .textSelection(.enabled)
                .lineLimit(nil)
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color(nsColor: .textBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color(nsColor: .separatorColor), lineWidth: 0.5)
        )
    }
}

// MARK: - Delete confirmation sheet

/// GitHub-style destructive confirm (web NoteEditor confirmDelete): the note's
/// title must be retyped exactly before Delete enables. The model re-checks the
/// match, so a stale call can never delete. On success the note is gone
/// (`state == .empty`), which drops the reader back to the empty pane.
private struct DeleteNoteSheet: View {
    @Bindable var model: NoteReaderModel
    let note: NoteDetail
    let onClose: () -> Void
    @State private var typedTitle = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Delete note").font(.headline)
            Text("This permanently deletes \(note.summary.ref.title) and cannot be undone.")
                .font(.body)
                .fixedSize(horizontal: false, vertical: true)
            Text("Type the note title to confirm:")
                .font(.caption)
                .foregroundStyle(.secondary)
            TextField("Note title", text: $typedTitle)
                .textFieldStyle(.roundedBorder)
                .onSubmit {
                    if isConfirmed { delete() }
                }
            if let error = model.saveError {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
            HStack {
                Spacer()
                Button("Cancel") { onClose() }
                    .keyboardShortcut(.cancelAction)
                Button(role: .destructive) { delete() } label: {
                    if model.isDeleting {
                        ProgressView().controlSize(.small)
                    } else {
                        Text("Delete note")
                    }
                }
                .disabled(!isConfirmed || model.isDeleting)
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .frame(width: 360)
    }

    private var isConfirmed: Bool {
        typedTitle.trimmingCharacters(in: .whitespaces)
            == note.summary.ref.title.trimmingCharacters(in: .whitespaces)
    }

    private func delete() {
        Task {
            if await model.deleteCurrent(confirmedTitle: typedTitle) {
                onClose()
            }
        }
    }
}

// MARK: - Note metadata editor

/// Edits a note's editable sidecar metadata (web NoteMetaDialog): the built-in
/// fields get typed controls — title, comma-separated tags, description, cover
/// image ref, icon — while props stays the one free-form "key: value" YAML
/// block, and the author-assigned flags (ADR 0074) are the two toggles
/// DEPRECATED / CONFIDENTIAL. The engine composes and validates the whole edit,
/// so a rejected save keeps the sheet open (web dialog stays open, unchanged).
private struct NoteMetaEditor: View {
    @Bindable var model: NoteReaderModel
    let onClose: () -> Void

    @State private var title = ""
    @State private var tags = ""
    @State private var description = ""
    @State private var image = ""
    @State private var icon = ""
    @State private var flags: [String] = []
    @State private var props = ""
    @State private var didLoad = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Note metadata").font(.headline)
            if didLoad {
                form
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, minHeight: 200)
            }
        }
        .padding(20)
        .frame(width: 400)
        .task {
            guard !didLoad else { return }
            if let meta = await model.fetchMeta() {
                title = meta.title
                tags = meta.tags.joined(separator: ", ")
                description = meta.description
                image = meta.image
                icon = meta.icon
                flags = meta.flags
                props = meta.props
                didLoad = true
            }
        }
    }

    @ViewBuilder
    private var form: some View {
        Form {
            TextField("Title — changing it renames the note and rewrites backlinks", text: $title, axis: .vertical)
            TextField("Tags — comma-separated", text: $tags)
            TextField("Description (og:description)", text: $description, axis: .vertical)
            TextField("Cover image — assets/… path", text: $image)
            TextField("Icon — an emoji shown beside the title", text: $icon)
            Toggle("DEPRECATED", isOn: flagBinding("DEPRECATED"))
            Toggle("CONFIDENTIAL", isOn: flagBinding("CONFIDENTIAL"))
            TextEditor(text: $props)
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 120)
        }
        .formStyle(.grouped)
        .scrollDisabled(false)

        if let error = model.saveError {
            Text(error)
                .font(.caption)
                .foregroundStyle(.red)
        }

        HStack {
            Spacer()
            Button("Cancel") { onClose() }
                .keyboardShortcut(.cancelAction)
            Button {
                save()
            } label: {
                if model.isSavingMeta {
                    ProgressView().controlSize(.small)
                } else {
                    Text("Save")
                }
            }
            .disabled(model.isSavingMeta)
            .keyboardShortcut(.defaultAction)
        }
    }

    /// One flag toggle, flipping the named member of the closed set in or out.
    private func flagBinding(_ flag: String) -> Binding<Bool> {
        Binding(
            get: { flags.contains(flag) },
            set: { checked in
                if checked {
                    flags = flags.contains(flag) ? flags : flags + [flag]
                } else {
                    flags = flags.filter { $0 != flag }
                }
            }
        )
    }

    private func save() {
        let request = SaveNoteMetaRequest(
            title: title.trimmingCharacters(in: .whitespaces),
            tags: tags.split(separator: ",").map {
                $0.trimmingCharacters(in: .whitespaces)
            }.filter { !$0.isEmpty },
            description: description,
            image: image.trimmingCharacters(in: .whitespaces),
            icon: icon.trimmingCharacters(in: .whitespaces),
            flags: flags,
            props: props
        )
        Task {
            if await model.saveMeta(request) {
                onClose()
            }
        }
    }
}

// Markdown rendering lives in MarkdownRenderer.swift (GFMBody, a MarkdownUI GFM
// renderer). The wikilink "Links" rail, the onWikilink wiring it needs, and the
// `trackwiki://` link interception (`wikilinkURLAction`) live with it or here;
// this file owns only the reader/editor chrome.

// MARK: - Tasks in this note

/// The note's parsed tasks (web tasktable/task controls, surfaced as a separate
/// "Tasks in this note" section rather than inside the body): one row per task
/// line, its state shown as a tappable badge that cycles TODO → DOING →
/// WAITING → DONE → CANCELLED. The write goes through the note's own etag; a
/// conflict (409) reloads via the model.
private struct NoteTasksSection: View {
    let tasks: [TaskItem]
    let onCycle: (Int) -> Void

    var body: some View {
        Divider()
        Text("Tasks in this note").font(.caption).foregroundStyle(.secondary)
        VStack(alignment: .leading, spacing: 4) {
            ForEach(tasks, id: \.line) { item in
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Button(item.state) { onCycle(item.line) }
                        .buttonStyle(.plain)
                        .font(.caption).fontWeight(.medium)
                        .foregroundStyle(item.done ? .secondary : .primary)
                    VStack(alignment: .leading, spacing: 2) {
                        if let priority = item.priority {
                            Text("[#\(priority)]")
                                .font(.caption).fontWeight(.bold)
                        }
                        Text(item.text.isEmpty ? "(untitled task)" : item.text)
                            .strikethrough(item.done)
                        if let completed = item.completed {
                            Text("✓ \(completed)").font(.caption).foregroundStyle(.tertiary)
                        }
                    }
                }
            }
        }
    }
}

/// Next state in the fixed table (`web/src/taskStates.ts`, mirroring the
/// engine's `task.States`): TODO → DOING → WAITING → DONE → CANCELLED → TODO.
private func nextTaskState(after state: String) -> String {
    let order = ["TODO", "DOING", "WAITING", "DONE", "CANCELLED"]
    guard let i = order.firstIndex(of: state) else { return "TODO" }
    return order[(i + 1) % order.count]
}

// MARK: - Share

/// A minimal NSSharingServicePicker wrapper (web ShareActions' share surface):
/// highlights the body for the system share sheet. Anchored to the toolbar
/// button's own frame; the picker is the macOS standard sheet.
private struct ShareButton: View {
    let items: [Any]

    var body: some View {
        Button {
            share()
        } label: {
            Label("Share", systemImage: "square.and.arrow.up")
        }
    }

    @MainActor
    private func share() {
        let picker = NSSharingServicePicker(items: items)
        if let window = NSApp.keyWindow,
           let contentView = window.contentView {
            // Anchor at the top-right of the window; the exact button frame is
            // not tracked here, which is fine for a share sheet.
            picker.show(relativeTo: .zero, of: contentView, preferredEdge: .maxY)
        }
    }
}
