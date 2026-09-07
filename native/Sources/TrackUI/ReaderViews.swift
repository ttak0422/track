import AppKit
import SwiftUI
import TrackAPI

// MVP reader: title, native-rendered Markdown body, backlink list.
// Design tokens follow docs/spec/design.md (ink + hairlines; the single
// salient `--mark` only for the active state). Rich blocks the MVP does not
// draw (math, diagrams, maps, decks) arrive as source text — a placeholder
// treatment is a follow-up, not a silent drop.

// MARK: - Search + reader shell

public struct SearchReaderView: View {
    @State private var search: SearchModel
    @State private var reader: NoteReaderModel
    @State private var query = ""
    /// Title typed in the New-note sheet.
    @State private var newNoteTitle = ""
    @State private var showNewNote = false
    /// Duplicate-title notice from a refused create (web "A note with the same
    /// title already exists"), shown inline in the sheet instead of dismissing.
    @State private var newNoteError: String?

    public init(client: TrackClient) {
        _search = State(initialValue: SearchModel(client: client))
        _reader = State(initialValue: NoteReaderModel(client: client))
    }

    public var body: some View {
        NavigationSplitView {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 6) {
                    TextField("Search", text: $query)
                        .textFieldStyle(.plain)
                        .onSubmit { Task { await search.search(query: query) } }
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
                List(search.results, id: \.ref.noteID) { result in
                    Button {
                        Task { await reader.open(result.qualifiedID) }
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(result.ref.title).font(.body)
                            if let match = result.match {
                                Text(match).font(.caption2).foregroundStyle(.tertiary)
                            }
                            if let snippet = result.snippet {
                                Text(snippet).font(.caption).foregroundStyle(.secondary)
                                    .lineLimit(2)
                            }
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
            .navigationTitle("track")
        } detail: {
            NoteReaderView(model: reader)
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
}

public struct NoteReaderView: View {
    @Bindable var model: NoteReaderModel
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

    public init(model: NoteReaderModel) {
        self.model = model
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

                MarkdownBody(
                    markdown: response.note.body,
                    onWikilink: { target in Task { await model.openWikilink(target: target) } }
                )

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
            .frame(maxWidth: 640, alignment: .leading)
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
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(maxWidth: 200)
                if model.isDirty {
                    Text("Edited — unsaved changes")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            if pane == .edit {
                TextEditor(text: $model.draftBody)
                    .font(.system(.body, design: .monospaced))
                    .frame(minHeight: 300)
            } else {
                ScrollView {
                    MarkdownBody(
                        markdown: model.draftBody,
                        onWikilink: { target in Task { await model.openWikilink(target: target) } }
                    )
                    .frame(maxWidth: 640, alignment: .leading)
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

// MARK: - Markdown

/// Native Markdown rendering from the engine's GFM body. Rich blocks the
/// native renderer cannot draw (diagrams, math, includes) are collapsed to a
/// placeholder line with the source kept behind a DisclosureGroup, and
/// `[[wikilink]]` targets are collected into a tappable "Links" section
/// resolved via `/api/resolve` (mirroring the web reader's WikiLink).
struct MarkdownBody: View {
    let markdown: String
    var onWikilink: ((String) -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Group {
                if let attributed = try? AttributedString(markdown: Self.placeholderSubstituting(markdown)) {
                    Text(attributed)
                } else {
                    // The body is always plain text; never fail the whole note
                    // because one construct did not parse.
                    Text(markdown).font(.body).textSelection(.enabled)
                }
            }
            .textSelection(.enabled)

            let links = Self.wikilinks(in: markdown)
            if !links.isEmpty {
                Text("Links").font(.caption).foregroundStyle(.secondary)
                ForEach(links, id: \.self) { target in
                    Button(target) { onWikilink?(target) }
                        .buttonStyle(.link)
                }
            }
        }
    }

    /// Languages whose fenced blocks the native renderer does not draw.
    private static let richFences: Set<String> = [
        "mermaid", "dot", "graphviz", "d2", "drawio", "mindmap", "map",
        "echarts", "viewspec", "taskboard", "track-view", "track-query", "dashboard",
    ]

    /// Rewrite `markdown` so rich blocks — diagram/math fenced blocks, `![[...]]`
    /// include lines, and `$`/`$$` math lines — are replaced by a single caption
    /// line instead of dumping their raw source into the note. Pure line-splitting;
    /// no full parse (the MVP scope).
    static func placeholderSubstituting(_ markdown: String) -> String {
        let lines = markdown.components(separatedBy: "\n")
        var out: [String] = []
        var i = 0
        while i < lines.count {
            let line = lines[i]
            let trimmed = line.trimmingCharacters(in: .whitespaces)

            // Fenced rich block: ```lang ... ```
            if trimmed.hasPrefix("```") {
                let lang = String(trimmed.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                let langName = lang.split(separator: " ", maxSplits: 1).first.map(String.init) ?? lang
                if richFences.contains(langName) {
                    i += 1
                    while i < lines.count && !lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                        i += 1
                    }
                    i += 1 // closing fence
                    out.append("[diagram: \(langName) — preview not supported in native yet]")
                    continue
                }
            }

            // Math lines: `$$...$$` and inline `$...$` on a line.
            if trimmed.hasPrefix("$$") || trimmed.hasPrefix("$") {
                out.append("[math — preview not supported in native yet]")
                i += 1
                continue
            }

            // Include directive: ![[...]]
            if trimmed.hasPrefix("![["), trimmed.hasSuffix("]]") {
                out.append("[include: \(trimmed) — not rendered in native yet]")
                i += 1
                continue
            }

            out.append(line)
            i += 1
        }
        return out.joined(separator: "\n")
    }

    /// Collect distinct `[[title]]`, `[[title#anchor]]`, `[[vault:title]]`
    /// targets in first-seen order, capped at 20 like the web reader's
    /// link rail. Anchors are kept verbatim so a tap can re-split them.
    static func wikilinks(in markdown: String, limit: Int = 20) -> [String] {
        var seen: [String] = []
        var set = Set<String>()
        let pattern = "\\[\\[([^\\]|]+)(?:\\|[^\\]]+)?\\]\\]"
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return [] }
        let ns = markdown as NSString
        let range = NSRange(location: 0, length: ns.length)
        regex.enumerateMatches(in: markdown, range: range) { match, _, _ in
            guard let match, let range = match.range(at: 1).toOptional() else { return }
            let target = (ns.substring(with: range) as String).trimmingCharacters(in: .whitespaces)
            guard !target.isEmpty, !set.contains(target) else { return }
            set.insert(target)
            seen.append(target)
        }
        return Array(seen.prefix(limit))
    }
}

private extension NSRange {
    func toOptional() -> NSRange? { location == NSNotFound ? nil : self }
}
