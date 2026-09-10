import AppKit
import SwiftUI
import TrackAPI
import UniformTypeIdentifiers

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

private struct SearchSection: Identifiable {
    let title: String
    let results: [SearchResult]
    var id: String { title }
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
    @State private var browse: BrowseModel
    @State private var liveEvents: LiveEventPoller
    @State private var dismissedChangeAt: Date?
    @State private var readerChangeNotice: String?
    @State private var pendingSearchResult: SearchResult?
    /// Today's-journal shortcut (web Shell "Today's journal"): failure notice
    /// shown inline under the search field, like a search error.
    @State private var todayError: String?
    @State private var isOpeningJournal = false
    @FocusState private var searchFocused: Bool
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.trackFontScale) private var fontScale

    public init(client: TrackClient) {
        _search = State(initialValue: SearchModel(client: client))
        _reader = State(initialValue: NoteReaderModel(client: client))
        _browse = State(initialValue: BrowseModel(client: client))
        _liveEvents = State(initialValue: LiveEventPoller(baseURL: client.baseURL) {
            NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
        })
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
                        .focused($searchFocused)
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
                    Button {
                        openTodayJournal()
                    } label: {
                        Image(systemName: isOpeningJournal ? "hourglass" : "book.closed")
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel("Today's journal")
                    .help("Today's journal")
                    .disabled(isOpeningJournal)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                Divider()
                 Group {
                    if search.isLoading {
                        ProgressView().frame(maxWidth: .infinity)
                    } else if let error = search.error {
                        Text(error).font(.caption).foregroundStyle(.red).padding(8)
                     } else if let error = todayError {
                        Text(error).font(.caption).foregroundStyle(.red).padding(8)
                     }
                 }
                if query.isEmpty && !recentList.isEmpty {
                    Text("Recent").trackSectionLabel()
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
                                Text(note.title).font(.system(size: 16 * fontScale)).lineLimit(1)
                                if note.id == reader.currentID?.raw && reader.isDirty {
                                    Text("•")
                                        .font(.title3)
                                        .foregroundStyle(TrackTheme.palette(for: colorScheme).mark)
                                        .accessibilityLabel("Unsaved changes")
                                }
                                if let ref = Self.mruRef(note), reading.isNew(ref) {
                                    TrackStateBadge("NEW")
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
                if query.isEmpty && !browse.newNotes.isEmpty {
                    // Recently-created notes (web SidebarNew "New" panel): the
                    // server's `sort=created` listing, newest first.
                    Text("New").trackSectionLabel()
                        .padding(.horizontal, 12).padding(.top, 8)
                    ForEach(Array(browse.newNotes.prefix(10)), id: \.qualifiedID) { note in
                        Button {
                            recordRecent(RecentNote(id: note.qualifiedID.raw, title: note.ref.title))
                            reading.markSeen(note.ref.noteID.raw)
                            Task { await reader.open(note.qualifiedID) }
                        } label: {
                            HStack(spacing: 6) {
                                Image(systemName: "plus").font(.caption).foregroundStyle(.tertiary)
                                Text(note.ref.title).font(.system(size: 16 * fontScale)).lineLimit(1)
                                if reading.isNew(note.ref) {
                                    TrackStateBadge("NEW")
                                }
                            }
                        }
                        .buttonStyle(.plain)
                        .padding(.horizontal, 12).padding(.vertical, 2)
                    }
                    Divider().padding(.top, 6)
                }
                 List {
                     ForEach(searchSections, id: \.title) { section in
                         Section {
                             ForEach(section.results, id: \.qualifiedID) { result in
                                 VStack(alignment: .leading, spacing: 4) {
                                     Button { openSearchResult(result) } label: {
                                         VStack(alignment: .leading, spacing: 2) {
                                              HStack(spacing: 6) {
                                                  if let icon = result.icon, !icon.isEmpty {
                                                      Text(icon).font(.body)
                                                  } else {
                                                      Image(systemName: "doc.text")
                                                          .font(.caption).foregroundStyle(.tertiary)
                                                  }
                                                  highlighted(result.ref.title)
                                                      .font(.system(size: 16 * fontScale))
                                                 if reading.isNew(result.ref) { TrackStateBadge("NEW") }
                                                  if isStale(result) { TrackStateBadge("古い", kind: .stale) }
                                                  if let flags = result.ref.flags {
                                                      ForEach(flags.filter { $0 == "DEPRECATED" || $0 == "CONFIDENTIAL" }, id: \.self) {
                                                          TrackFlagBadge($0)
                                                      }
                                                  }
                                             }
                                             if let match = result.match {
                                                 highlighted(match).font(.system(size: 11 * fontScale)).foregroundStyle(.tertiary)
                                             }
                                             if let snippet = result.snippet {
                                                 highlighted(snippet).font(.system(size: 13 * fontScale)).foregroundStyle(.secondary)
                                                     .lineLimit(2)
                                             }
                                         }
                                         .frame(maxWidth: .infinity, alignment: .leading)
                                     }
                                     .buttonStyle(.plain)
                                     if let tags = result.tags, !tags.isEmpty {
                                         HStack(spacing: 5) {
                                             ForEach(tags, id: \.self) { tag in
                                                 Button("#\(tag)") { appendSearchTag(tag) }
                                                     .buttonStyle(.borderless)
                                                     .font(.system(size: 13 * fontScale)).foregroundStyle(.secondary)
                                             }
                                         }
                                     }
                                 }
                                 .padding(.vertical, 2)
                                 .listRowBackground(activeSearchRow(result) ? Color.clear : nil)
                                 .overlay(alignment: .leading) {
                                     if activeSearchRow(result) {
                                         // L-shaped reading-edge cursor (web
                                         // .result-row:has(.result.is-active)):
                                         // a mark edge on the left and bottom,
                                         // never a filled tile.
                                         TrackTheme.palette(for: colorScheme).mark
                                             .frame(width: 2)
                                             .padding(.vertical, 4)
                                     }
                                 }
                                 .overlay(alignment: .bottom) {
                                     if activeSearchRow(result) {
                                         TrackTheme.palette(for: colorScheme).mark
                                             .frame(height: 2)
                                             .padding(.leading, 2)
                                     }
                                 }
                             }
                         } header: {
                             Text(section.title).trackSectionLabel()
                         }
                     }
                     if search.unavailableCount > 0 {
                         Section {
                             Text("⚠ \(search.unavailableCount) vault\(search.unavailableCount == 1 ? "" : "s") could not be searched")
                                 .font(.caption).foregroundStyle(.secondary)
                         }
                     }
                 }
            }
            .navigationTitle("track")
         } detail: {
             if reader.currentID == nil {
                 searchHome
             } else {
                  NoteReaderView(model: reader, baseURL: baseURL) { tag in
                      appendSearchTag(tag)
                  }
             }
         }
         .overlay(alignment: .top) {
             if let changedAt = liveEvents.lastChangeAt, changedAt != dismissedChangeAt {
                 changeBanner(changedAt: changedAt)
                     .padding(.horizontal, 12)
                     .padding(.top, 8)
                     .transition(.move(edge: .top).combined(with: .opacity))
             }
         }
         .onKeyPress("/") {
             search.requestSearchFocus()
             return .handled
         }
         .onChange(of: search.searchFocusRequested) { _, _ in
             if search.consumeSearchFocusRequest() { searchFocused = true }
         }
          .task {
              liveEvents.start()
              await browse.loadNewNotes()
          }
         .onDisappear {
             liveEvents.stop()
         }
         .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard !query.isEmpty, !search.isLoading else { return }
            search.search(query: query)
      }
         .alert("Unsaved edits", isPresented: Binding(
             get: { readerChangeNotice != nil },
             set: { if !$0 { readerChangeNotice = nil; pendingSearchResult = nil } }
         )) {
             Button("Discard and open", role: .destructive) {
                 reader.discardDraft()
                 if let result = pendingSearchResult {
                     pendingSearchResult = nil
                     openSearchResult(result)
                 }
                 readerChangeNotice = nil
             }
             Button("Keep editing", role: .cancel) {
                 readerChangeNotice = nil
                 pendingSearchResult = nil
             }
         } message: {
             Text("Your unsaved changes will be lost.")
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

    private var searchHome: some View {
        VStack(alignment: .leading, spacing: 22) {
            Spacer()
            VStack(alignment: .leading, spacing: 8) {
                Text("Search your notes").font(.title2.weight(.medium))
                Text("Find a note by title, text, or tag.")
                    .font(.callout).foregroundStyle(.secondary)
                TextField("Search", text: $query)
                    .textFieldStyle(.plain).focused($searchFocused)
                    .onSubmit { searchFocused = false }
                    .padding(.vertical, 8)
                    .overlay(alignment: .bottom) {
                        Rectangle()
                            .fill(searchFocused ? TrackTheme.palette(for: colorScheme).mark : Color(nsColor: .separatorColor))
                            .frame(height: searchFocused ? 2 : 1)
                    }
            }
            .frame(maxWidth: 520, alignment: .leading)
            VStack(alignment: .leading, spacing: 8) {
                Text("ACTIVITY").trackSectionLabel()
                Text("Browse your recent note activity")
                    .font(.system(size: 14 * fontScale)).foregroundStyle(.secondary)
                ActivityHeatmapView(model: browse)
                    .background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 8))
                    .overlay(RoundedRectangle(cornerRadius: 8).stroke(Color(nsColor: .separatorColor), lineWidth: 0.5))
            }
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .center)
        .padding(40)
    }

    private func changeBanner(changedAt: Date) -> some View {
        let name = liveEvents.lastChangedNoteName
        return HStack(spacing: 10) {
            Image(systemName: "arrow.triangle.2.circlepath")
                .foregroundStyle(TrackTheme.palette(for: colorScheme).mark)
            Text(name.map { "Changed: \($0)" } ?? "Vault changed")
                .font(.caption).lineLimit(1)
            Spacer()
            Button("Reload") {
                dismissedChangeAt = changedAt
                if let id = reader.currentID { Task { await reader.open(id) } }
                if !query.isEmpty { search.search(query: query) }
            }.buttonStyle(.plain)
            Button { dismissedChangeAt = changedAt } label: {
                Image(systemName: "xmark")
            }
            .buttonStyle(.plain).accessibilityLabel("Dismiss change notification")
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(Color(nsColor: .separatorColor), lineWidth: 0.5))
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

    private var searchSections: [SearchSection] {
        let groups = ["title": "Titles", "full": "Full text", "file": "File name"]
        let grouped = Dictionary(grouping: filteredSearchResults) { result -> String in
            let match = (result.match ?? "").lowercased()
            if match.contains("file") || match.contains("name") { return "file" }
            if match.contains("title") { return "title" }
            return "full"
        }
        return ["title", "full", "file"].compactMap { key in
            guard let results = grouped[key], !results.isEmpty else { return nil }
            return SearchSection(title: groups[key]!, results: results)
        }
    }

    private func openSearchResult(_ result: SearchResult) {
        guard !reader.isDirty else {
            readerChangeNotice = "Discard unsaved edits before opening another note?"
            pendingSearchResult = result
            return
        }
        recordRecent(RecentNote(id: result.qualifiedID.raw, title: result.ref.title))
        reading.markSeen(result.ref.noteID.raw)
        Task { await reader.open(result.qualifiedID) }
    }

    private func appendSearchTag(_ tag: String) {
        let token = "#\(tag)"
        guard !query.split(whereSeparator: { $0 == " " || $0 == "\n" }).contains(Substring(token)) else { return }
        query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        query += query.isEmpty ? token : " \(token)"
        search.search(query: query)
    }

    /// Today's journal shortcut (web Shell "Today's journal"): opens (creating
    /// if needed) today's journal and hands it to the reader, mirroring how
    /// the activity heatmap opens a day.
    private func openTodayJournal() {
        guard !isOpeningJournal else { return }
        guard !reader.isDirty else {
            readerChangeNotice = "Discard unsaved edits before opening another note?"
            return
        }
        isOpeningJournal = true
        todayError = nil
        Task {
            do {
                let journal = try await reader.client.openJournal(date: Self.todayString())
                await reader.open(journal.noteID)
                if case .loaded(let response) = reader.state {
                    recordRecent(RecentNote(id: journal.noteID.raw, title: response.note.summary.ref.title))
                    reading.markSeen(response.note.summary.ref.noteID.raw)
                }
            } catch {
                todayError = error.localizedDescription
            }
            isOpeningJournal = false
        }
    }

    private static func todayString() -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.string(from: Date())
    }

    /// Search-match highlight (design.md Search matches): a semantic mark
    /// around the matching text only — surrounding ink kept, sunk
    /// `--panel-soft` ground with medium weight. Built as one Text from an
    /// AttributedString so the row keeps a single Text value.
    private func highlighted(_ text: String) -> Text {
        let needle = query.split(whereSeparator: { $0 == " " || $0 == "\n" })
            .filter { !$0.hasPrefix("#") }.joined(separator: " ")
        guard !needle.isEmpty else { return Text(text) }
        let palette = TrackTheme.palette(for: colorScheme)
        var attr = AttributedString(text)
        var searchFrom = attr.startIndex
        var found = false
        while searchFrom < attr.endIndex,
              let range = attr[searchFrom...].range(of: needle, options: [.caseInsensitive]) {
            found = true
            attr[range].backgroundColor = palette.panelSoft
            attr[range].inlinePresentationIntent = .stronglyEmphasized
            searchFrom = range.upperBound
        }
        guard found else { return Text(text) }
        return Text(attr)
    }

    private func activeSearchRow(_ result: SearchResult) -> Bool {
        activeSearchIndex == filteredSearchResults.firstIndex(where: { $0.qualifiedID == result.qualifiedID })
    }

    private func statusBadge(_ text: String) -> some View {
        TrackStateBadge(text)
    }

    private func isStale(_ result: SearchResult) -> Bool {
        guard let last = result.days?.last,
              let date = ISO8601DateFormatter().date(from: last + "T00:00:00Z") else { return false }
        return date < Calendar.current.date(byAdding: .year, value: -1, to: Date())!
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
private enum NoteEditorPane: String {
    case edit
    case preview
    case split
}

private struct WikilinkPreview: Equatable {
    let title: String
    let excerpt: String
}

public struct NoteReaderView: View {
    @Bindable var model: NoteReaderModel
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.trackFontScale) private var fontScale
    @AppStorage(TrackAppearance.contentWidthKey) private var contentWidthRaw: String?
    /// The API base URL, passed down to the GFM renderer for `assets/…` embeds.
    let baseURL: URL
    let onTagSearch: (String) -> Void
    /// Edit/Preview/Split is shared across note windows, like the web editor's
    /// persisted editorMode. Keep the string at the edge so an older value can
    /// never make the picker fail to render.
    @AppStorage("track.noteEditorPane") private var paneRaw = NoteEditorPane.edit.rawValue
    private var pane: NoteEditorPane {
        get { NoteEditorPane(rawValue: paneRaw) ?? .edit }
    }
    private var paneBinding: Binding<NoteEditorPane> {
        Binding(get: { NoteEditorPane(rawValue: paneRaw) ?? .edit }, set: { paneRaw = $0.rawValue })
    }
    /// Dirty guard: Done with unsaved edits asks before discarding the draft.
    @State private var confirmDiscard = false
    /// Delete confirmation: the user must retype the title before the note can
    /// be removed (web confirmDelete's GitHub-style retype). A sheet, because a
    /// SwiftUI `.alert` cannot host a text field.
    @State private var showDeleteConfirm = false
    /// Note metadata editor (web NoteMetaDialog), bound to the open note.
    @State private var showMeta = false
    @State private var titleCopied = false
    @State private var anchorHighlight = false
    @State private var wikilinkPreview: WikilinkPreview?
    @State private var saveConfirmation = false
    /// Local visible-time accumulator shared with NoteReaderModel's recordView
    /// bridge. A coarse ten-second tick is sufficient for the read milestone.
    @State private var reading = ReadingStore()
    @State private var onThisDay: [SearchResult] = []
    /// Date-cell editing target for the note task table (web TaskControls date
    /// cells): the task line plus which date field the picker writes.
    @State private var taskDateTarget: NoteTaskDateTarget?
    /// Link-graph model for the aside's embedded local graph (web note pages
    /// carry the one-hop graph beside the reading column).
    @State private var graphModel: GraphModel

    public init(model: NoteReaderModel, baseURL: URL, onTagSearch: @escaping (String) -> Void = { _ in }) {
        self.model = model
        self.baseURL = baseURL
        self.onTagSearch = onTagSearch
        _graphModel = State(initialValue: GraphModel(client: model.client))
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
            if model.saveConflict != nil || model.saveError != nil || saveConfirmation {
                VStack(spacing: 6) {
                    if let conflict = model.saveConflict {
                        readerBanner(conflict, isError: false) { model.dismissConflict() }
                    }
                    if let error = model.saveError {
                        readerBanner(error, isError: true) { model.dismissSaveError() }
                    }
                    if saveConfirmation {
                        readerBanner("Saved successfully", isError: false) { saveConfirmation = false }
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
            guard case .loaded(let response) = model.state else { return }
            if response.note.summary.ref.fileKind == "journal" {
                let formatter = DateFormatter()
                formatter.dateFormat = "yyyy-MM-dd"
                formatter.locale = Locale(identifier: "en_US_POSIX")
                let day = formatter.string(from: Date())
                if let notes = try? await model.client.listNotes(limit: 500) {
                    onThisDay = notes.notes.filter {
                        $0.ref.fileKind == "journal" &&
                        $0.ref.noteID != response.note.summary.ref.noteID &&
                        ($0.days ?? []).contains(day)
                    }
                }
            } else {
                onThisDay = []
            }
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(10))
                guard !Task.isCancelled else { return }
                _ = await model.recordView(using: reading, seconds: 10, text: response.note.body)
            }
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
        .popover(item: $taskDateTarget) { target in
            NoteTaskDateEditor(target: target) { field, date in
                Task { await model.setTaskDate(line: target.line, field: field, date: date) }
            }
            .padding()
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
                    Button("Copy portable Markdown") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(PortableMarkdown.portable(note.body), forType: .string)
                    }
                    .help("Copy with [[wikilinks]] flattened to plain text")
                    Button("Copy for Confluence") {
                        copyConfluence(note.body)
                    }
                    .help("Copy as rich HTML with a plain-text fallback")
                    Divider()
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
            Button("Save") { Task {
                await model.saveDraft()
                if model.saveError == nil && model.saveConflict == nil { saveConfirmation = true }
            } }
                .disabled(!model.isDirty)
                .keyboardShortcut("s", modifiers: [.command])
        }
    }

    private var loadedNote: NoteDetail? {
        if case .loaded(let response) = model.state { return response.note }
        return nil
    }

    /// Rich copy for Confluence (web NoteActionsMenu copyConfluence): the
    /// portable body rendered to HTML for rich editors, paired with the
    /// plain-text fallback (delimiter-free, <br> as line breaks) on the same
    /// pasteboard. The HTML comes from Foundation's Markdown parser rather
    /// than the web's react-markdown pipeline, so exotic GFM may render
    /// plainly — the text flavor always survives. A body that will not parse
    /// falls back to the plain text alone.
    private func copyConfluence(_ body: String) {
        let portable = PortableMarkdown.portable(body)
        let plain = PortableMarkdown.confluencePlainText(portable)
        let board = NSPasteboard.general
        board.clearContents()
        if let parsed = try? AttributedString(
            markdown: portable,
            options: AttributedString.MarkdownParsingOptions(interpretedSyntax: .full)
        ),
            let html = try? NSAttributedString(parsed).data(
                from: NSRange(
                    location: 0,
                    length: NSAttributedString(parsed).length
                ),
                documentAttributes: [
                    NSAttributedString.DocumentAttributeKey.documentType:
                        NSAttributedString.DocumentType.html,
                    NSAttributedString.DocumentAttributeKey.characterEncoding:
                        String.Encoding.utf8.rawValue,
                ]
            ) {
            board.setString(plain, forType: .string)
            board.setData(html, forType: .html)
        } else {
            board.setString(plain, forType: .string)
        }
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
        paneRaw = NoteEditorPane.edit.rawValue
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
        GeometryReader { geometry in
            ScrollView {
                let wide = contentWidthMode != .normal || geometry.size.width >= 1_080
                Group {
                    if wide {
                        HStack(alignment: .top, spacing: 28) {
                            readerMain(response)
                                .frame(maxWidth: contentWidthMode == .full ? 900 : 760, alignment: .leading)
                            readerAside(response)
                                .frame(width: 260, alignment: .leading)
                        }
                    } else {
                        VStack(alignment: .leading, spacing: 16) {
                            readerMain(response)
                            readerAside(response)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .topLeading)
                .padding(24)
            }
        }
    }

    @ViewBuilder
    private func readerMain(_ response: NoteResponse) -> some View {
        VStack(alignment: .leading, spacing: 16) {
                if let trail = response.trail, !trail.isEmpty {
                    HStack(spacing: 5) {
                        ForEach(Array(trail.enumerated()), id: \.element.noteID) { index, ref in
                            if index > 0 { Text("/").foregroundStyle(.tertiary) }
                            asideLink(ref.title) { Task { await model.openRef(ref) } }
                        }
                    }
                    .font(.caption)
                }
                noteHeader(response.note, showTags: false)

                if let excerpt = model.anchoredExcerpt {
                    anchoredExcerptCard(excerpt)
                }

                GFMBody(
                    markdown: model.didRender ? model.renderedBody : response.note.body,
                    baseURL: baseURL,
                    vault: model.currentID?.split().vault ?? "",
                    includes: model.didRender ? model.renderedIncludes : nil,
                    client: model.client,
                    onWikilink: { target in Task { await model.openWikilink(target: target) } },
                    onTaskToggle: { line, completed in
                        Task { await model.setTaskState(line: line, to: completed ? "DONE" : "TODO") }
                    }
                )
                .environment(\.openURL, wikilinkURLAction)
                .textSelection(.enabled)

                if let tasks = response.note.tasks, !tasks.items.isEmpty {
                    NoteTasksSection(
                        tasks: tasks.items,
                        onCycle: { line in
                            let current = tasks.items.first { $0.line == line }?.state ?? "TODO"
                            Task { await model.setTaskState(line: line, to: nextTaskState(after: current)) }
                        },
                        onPickDate: { line, field in
                            taskDateTarget = NoteTaskDateTarget(line: line, field: field)
                        }
                    )
                }

        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func readerAside(_ response: NoteResponse) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            if let tags = response.note.summary.tags, !tags.isEmpty {
                asideSection("Tags", count: tags.count) {
                    ForEach(tags, id: \.self) { tag in
                        Button("#\(tag)") { onTagSearch(tag) }
                            .buttonStyle(.plain)
                            .font(.system(size: 14 * fontScale))
                            .foregroundStyle(TrackTheme.palette(for: colorScheme).muted)
                    }
                }
            }
            if !onThisDay.isEmpty {
                asideSection("On this day", count: onThisDay.count) {
                    ForEach(Array(onThisDay.prefix(8)), id: \.qualifiedID) { result in
                        asideLink(result.ref.title) { Task { await model.open(result.qualifiedID) } }
                    }
                    if onThisDay.count > 8 {
                        Text("+\(onThisDay.count - 8) more")
                            .font(.system(size: 11 * fontScale)).foregroundStyle(.secondary)
                    }
                }
            }
            let headings = GFMBody.tocEntries(in: response.note.body)
            if !headings.isEmpty {
                asideSection("Contents", count: headings.count) {
                    ForEach(Array(headings.prefix(12))) { entry in
                        Button {
                            model.anchoredExcerpt = headingExcerpt(entry.title, in: response.note.body)
                        } label: {
                            HStack(spacing: 4) {
                                Image(systemName: "link").font(.system(size: 11 * fontScale))
                                Text(entry.title)
                                    .font(.system(size: 14 * fontScale))
                            }
                            .padding(.leading, CGFloat(max(0, entry.level - 1) * 12))
                        }
                        .buttonStyle(.plain).foregroundStyle(.secondary)
                    }
                    if headings.count > 12 {
                        Text("+\(headings.count - 12) more")
                            .font(.system(size: 11 * fontScale)).foregroundStyle(.secondary)
                    }
                }
            }
            let wikilinks = GFMBody.wikilinks(in: response.note.body)
            if !wikilinks.isEmpty {
                asideSection("Links", count: wikilinks.count) {
                    ForEach(Array(wikilinks.prefix(10)), id: \.self) { target in
                        asideLink(target) { Task { await model.openWikilink(target: target) } }
                    }
                    if wikilinks.count > 10 {
                        Text("+\(wikilinks.count - 10) more")
                            .font(.system(size: 11 * fontScale)).foregroundStyle(.secondary)
                    }
                }
            }
            if let children = response.children, !children.isEmpty {
                asideRefs("Children", children)
            }
            if let external = response.external, !external.isEmpty {
                asideSection("Linked from other vaults", count: external.count) {
                    ForEach(Array(external.prefix(10)), id: \.noteID) { ref in
                        asideLink("\(ref.vault)/\(ref.title)") {
                            Task { await model.open(TrackID.qualify(vault: ref.vault, id: ref.noteID.raw)) }
                        }
                    }
                    if external.count > 10 {
                        Text("+\(external.count - 10) more")
                            .font(.system(size: 11 * fontScale)).foregroundStyle(.secondary)
                    }
                }
            }
            asideSection("Backlinks", count: response.backlinks.count) {
                    Text(response.backlinks.isEmpty ? "No backlinks." : "\(response.backlinks.count) backlink\(response.backlinks.count == 1 ? "" : "s")")
                        .font(.system(size: 13 * fontScale)).foregroundStyle(.secondary)
                if !response.backlinks.isEmpty {
                    ForEach(Array(response.backlinks.prefix(10)), id: \.noteID) { ref in
                        HStack(spacing: 6) {
                            asideLink(ref.title) { Task { await model.openRef(ref) } }
                            if readingBadge(for: ref) { TrackStateBadge("NEW") }
                        }
                    }
                    if response.backlinks.count > 10 {
                        Text("+\(response.backlinks.count - 10) more")
                            .font(.system(size: 11 * fontScale)).foregroundStyle(.secondary)
                    }
                }
            }
            // The one-hop link graph around the open note (web note pages carry
            // their local graph in the aside; the full vault graph stays in the
            // Graph tab). Rendered as the degree-sized neighbor list until a
            // Canvas force-directed layout lands natively.
            if let centerID = model.currentID {
                asideSection("Graph", count: nil) {
                    LocalGraphView(model: graphModel, centerID: centerID) { raw in
                        Task { await model.open(TrackID(raw)) }
                    }
                    .id(centerID)
                }
            }
            if let unavailable = response.unavailable, !unavailable.isEmpty {
                asideSection("Warnings", count: unavailable.count) {
                    ForEach(unavailable, id: \.name) { vault in
                        Text("⚠ \(vault.name)\(vault.error.map { ": \($0)" } ?? "")")
                            .font(.system(size: 13 * fontScale)).foregroundStyle(.secondary)
                    }
                }
            }
        }
        .overlay(alignment: .topLeading) {
            if let preview = wikilinkPreview {
                wikilinkPreviewCard(preview)
                    .offset(y: -8)
                    .zIndex(2)
            }
        }
    }

    private func asideSection<Content: View>(_ title: String, count: Int? = nil, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(title).trackSectionLabel()
                Spacer()
                if let count {
                    Text("\(count)")
                        .font(.system(size: 11 * fontScale, design: .monospaced))
                        .foregroundStyle(TrackTheme.palette(for: colorScheme).faint)
                }
            }
            content()
        }
        .padding(.bottom, 4)
    }

    private func asideRefs(_ title: String, _ refs: [NoteRef]) -> some View {
        asideSection(title, count: refs.count) {
            ForEach(Array(refs.prefix(10)), id: \.noteID) { ref in
                asideLink(ref.title) { Task { await model.openRef(ref) } }
            }
            if refs.count > 10 {
                Text("+\(refs.count - 10) more")
                    .font(.system(size: 11 * fontScale)).foregroundStyle(.secondary)
            }
        }
    }

    private func asideLink(_ title: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.system(size: 14 * fontScale))
                .foregroundStyle(TrackTheme.palette(for: colorScheme).muted)
        }
        .buttonStyle(.plain)
        .onHover { hovering in
            guard hovering else { wikilinkPreview = nil; return }
            Task { await loadWikilinkPreview(target: title) }
        }
    }

    private func loadWikilinkPreview(target: String) async {
        let trimmed = target.trimmingCharacters(in: .whitespacesAndNewlines)
        let key = trimmed.split(separator: "#", maxSplits: 1).first.map(String.init) ?? trimmed
        let parts = key.split(separator: ":", maxSplits: 1).map(String.init)
        let vault = parts.count == 2 ? parts[0] : ""
        let term = parts.count == 2 ? parts[1] : key
        guard let resolved = try? await model.client.resolveTerm(term, vault: vault), resolved.found else { return }
        let id = TrackID.qualify(vault: vault, id: resolved.note.noteID.raw)
        guard let response = try? await model.client.getNote(id) else { return }
        let excerpt = response.note.body.components(separatedBy: .newlines)
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .first { !$0.isEmpty && !$0.hasPrefix("#") && !$0.hasPrefix("```") } ?? ""
        await MainActor.run {
            wikilinkPreview = WikilinkPreview(title: response.note.summary.ref.title, excerpt: excerpt)
        }
    }

    private func wikilinkPreviewCard(_ preview: WikilinkPreview) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(preview.title).font(.callout.weight(.semibold))
            if !preview.excerpt.isEmpty {
                Text(preview.excerpt).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
        }
        .padding(10)
        .frame(width: 240, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(Color(nsColor: .separatorColor), lineWidth: 0.5))
        .shadow(radius: 8, y: 3)
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
                Picker("Pane", selection: paneBinding) {
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
    private func noteHeader(_ note: NoteDetail, showTags: Bool = true) -> some View {
        HStack(spacing: 8) {
            Text(note.summary.ref.title)
                .font(.system(size: 26 * fontScale, weight: .medium))
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(note.summary.ref.title, forType: .string)
                titleCopied = true
                DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { titleCopied = false }
            } label: {
                Image(systemName: titleCopied ? "checkmark" : "doc.on.doc")
            }
            .buttonStyle(.borderless)
            .foregroundStyle(titleCopied ? .green : .secondary)
            .help("Copy title")
            .accessibilityLabel(titleCopied ? "Title copied" : "Copy title")
            Spacer()
            if let flags = note.summary.ref.flags {
                ForEach(flags.filter { $0 == "DEPRECATED" || $0 == "CONFIDENTIAL" }, id: \.self) { flag in
                    TrackFlagBadge(flag)
                }
            }
        }

        if showTags, let tags = note.summary.tags, !tags.isEmpty {
            HStack(spacing: 8) {
                ForEach(tags, id: \.self) { tag in
                    Text("#\(tag)").font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        if let props = note.props, !props.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(Array(groupedProperties(props).prefix(10).enumerated()), id: \.offset) { _, item in
                    HStack(alignment: .firstTextBaseline, spacing: 4) {
                        Text("\(item.key):").font(.caption).foregroundStyle(.secondary)
                        if item.type == "link" {
                            Button(item.value) { Task { await model.openWikilink(target: item.value) } }
                                .buttonStyle(.link).font(.caption)
                        } else {
                            Text(item.value).font(.caption).foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        }
                    }
                }
            }
        }
        if let created = note.created { Text("created \(created)").font(.caption).foregroundStyle(.secondary) }
        if let updated = note.updated { Text("updated \(Self.dayString(updated))").font(.caption).foregroundStyle(.secondary) }
        if let tasks = note.tasks, !tasks.items.isEmpty {
            Text("\(tasks.items.count) task\(tasks.items.count == 1 ? "" : "s")")
                .font(.caption).foregroundStyle(.secondary)
        }
    }

    private struct GroupedProperty {
        let key: String
        let value: String
        let type: String
    }

    private func groupedProperties(_ props: [NoteProp]) -> [GroupedProperty] {
        var order: [String] = []
        var values: [String: (values: [String], type: String)] = [:]
        for prop in props where !(prop.key == "up" && prop.type == "link") {
            if values[prop.key] == nil { order.append(prop.key) }
            values[prop.key, default: ([], prop.type)].values.append(prop.value)
        }
        return order.compactMap { key in
            guard let item = values[key] else { return nil }
            return GroupedProperty(key: key, value: item.values.joined(separator: ", "), type: item.type)
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

    private func headingExcerpt(_ title: String, in body: String) -> String {
        let lines = body.components(separatedBy: .newlines)
        guard let start = lines.firstIndex(where: { line in
            let text = line.trimmingCharacters(in: .whitespaces)
            return text.drop { $0 == "#" }.trimmingCharacters(in: .whitespaces)
                .replacingOccurrences(of: "#", with: "") == title
        }) else { return title }
        let level = lines[start].prefix { $0 == "#" }.count
        let end = lines[(start + 1)...].firstIndex { line in
            let text = line.trimmingCharacters(in: .whitespaces)
            return text.prefix { $0 == "#" }.count == level && text.drop { $0 == "#" }.first == " "
        } ?? lines.count
        return lines[start..<end].joined(separator: "\n")
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
                    .trackSectionLabel()
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
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(anchorHighlight ? TrackTheme.palette(for: colorScheme).mark : .clear, lineWidth: 2)
        )
        .onAppear {
            anchorHighlight = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.4) { anchorHighlight = false }
        }
    }

    private func statusBadge(_ text: String) -> some View {
        TrackStateBadge(text)
    }

    private func readingBadge(for ref: NoteRef) -> Bool {
        let store = ReadingStore()
        return store.isNew(ref)
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
    /// Cover-image import (web NoteMetaDialog pickImage): the chosen file is
    /// uploaded to /api/asset and the returned assets/… ref fills the field.
    /// Failures surface inline; the existing ref is left untouched.
    @State private var showImageImporter = false
    @State private var isUploadingImage = false
    @State private var uploadError: String?

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
            HStack(spacing: 8) {
                Button(isUploadingImage ? "Importing…" : "Choose a file…") {
                    uploadError = nil
                    showImageImporter = true
                }
                .disabled(isUploadingImage)
                if isUploadingImage { ProgressView().controlSize(.small) }
            }
            .font(.caption)
            if let uploadError {
                Text(uploadError).font(.caption).foregroundStyle(.red)
            }
            TextField("Icon — an emoji shown beside the title", text: $icon)
            Toggle("DEPRECATED", isOn: flagBinding("DEPRECATED"))
            Toggle("CONFIDENTIAL", isOn: flagBinding("CONFIDENTIAL"))
            TextEditor(text: $props)
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 120)
        }
        .formStyle(.grouped)
        .scrollDisabled(false)
        .fileImporter(
            isPresented: $showImageImporter,
            allowedContentTypes: [.image],
            allowsMultipleSelection: false
        ) { result in importImage(result) }

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

    /// Uploads the file chosen in the image importer to the note's own vault
    /// and fills the cover field with the returned assets/… ref (web
    /// pickImage). A failure leaves the existing ref untouched.
    private func importImage(_ result: Result<[URL], Error>) {
        switch result {
        case .failure(let error):
            uploadError = error.localizedDescription
        case .success(let urls):
            guard let url = urls.first else { return }
            let accessing = url.startAccessingSecurityScopedResource()
            defer { if accessing { url.stopAccessingSecurityScopedResource() } }
            guard let data = try? Data(contentsOf: url) else {
                uploadError = "Could not read \(url.lastPathComponent)"
                return
            }
            isUploadingImage = true
            uploadError = nil
            Task {
                defer { isUploadingImage = false }
                do {
                    let vault = model.currentID?.split().vault ?? ""
                    let response = try await model.client.uploadAsset(
                        fileName: url.lastPathComponent,
                        data: data,
                        mimeType: Self.mimeType(for: url.pathExtension),
                        vault: vault
                    )
                    image = response.ref
                } catch {
                    uploadError = error.localizedDescription
                }
            }
        }
    }

    private static func mimeType(for pathExtension: String) -> String {
        switch pathExtension.lowercased() {
        case "png": return "image/png"
        case "jpg", "jpeg": return "image/jpeg"
        case "gif": return "image/gif"
        case "webp": return "image/webp"
        case "svg": return "image/svg+xml"
        case "avif": return "image/avif"
        case "heic": return "image/heic"
        default: return "application/octet-stream"
        }
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

/// Date-cell editing target for the note task table: the task's file line plus
/// which date field the picker writes (web TaskControls date cells).
private struct NoteTaskDateTarget: Identifiable {
    let line: Int
    let field: DateField
    var id: String { "\(line)#\(field.rawValue)" }
}

// MARK: - Tasks in this note

/// The note's parsed tasks as a table (web `TaskTable`: STATE/TASK/SCHED/DUE).
/// Sorting is view-only — the note keeps its file order (third header click
/// returns to source order); STATE sorts by the state-set order, date columns
/// sink empties last. The state cell cycles on tap and the date cells open the
/// picker; both write through the note's own etag with `expect`, so a 409
/// reloads via the model.
private struct NoteTasksSection: View {
    let tasks: [TaskItem]
    let onCycle: (Int) -> Void
    let onPickDate: (Int, DateField) -> Void

    private enum SortKey { case state, sched, due }
    @State private var sortKey: SortKey?
    @State private var sortAsc = true
    @Environment(\.colorScheme) private var colorScheme

    private static let stateOrder = ["TODO", "DOING", "WAITING", "DONE", "CANCELLED"]

    var body: some View {
        Divider()
        Text("Tasks in this note").trackSectionLabel()
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                sortHeader("STATE", key: .state).frame(width: 72, alignment: .leading)
                Text("TASK")
                    .font(.caption).fontWeight(.semibold).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                sortHeader("SCHED", key: .sched).frame(width: 92, alignment: .leading)
                sortHeader("DUE", key: .due).frame(width: 92, alignment: .leading)
            }
            .padding(.vertical, 4)
            Divider()
            ForEach(orderedTasks, id: \.line) { item in
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Button(item.state) { onCycle(item.line) }
                        .buttonStyle(.plain)
                        .font(.caption).fontWeight(.medium)
                        .foregroundStyle(item.done ? .secondary : .primary)
                        .frame(width: 72, alignment: .leading)
                    VStack(alignment: .leading, spacing: 2) {
                        if let priority = item.priority {
                            noteTaskChip("[#\(priority)]", emphasis: true)
                        }
                        Text(item.text.isEmpty ? "(untitled task)" : item.text)
                            .strikethrough(item.done)
                        if let completed = item.completed {
                            Text("✓ \(completed)").font(.caption).foregroundStyle(.tertiary)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    dateCell(item.scheduled, prefix: "▷", line: item.line, field: .scheduled)
                        .frame(width: 92, alignment: .leading)
                    dateCell(item.due, prefix: "!", line: item.line, field: .due)
                        .frame(width: 92, alignment: .leading)
                }
                .padding(.vertical, 4)
                Divider()
            }
        }
    }

    private func sortHeader(_ label: String, key: SortKey) -> some View {
        Button {
            if sortKey != key {
                sortKey = key
                sortAsc = true
            } else if sortAsc {
                sortAsc = false
            } else {
                sortKey = nil
                sortAsc = true
            }
        } label: {
            Text(sortKey == key ? "\(label)\(sortAsc ? " ▲" : " ▼")" : label)
                .font(.caption).fontWeight(.semibold).foregroundStyle(.secondary)
        }
        .buttonStyle(.plain)
    }

    private var orderedTasks: [TaskItem] {
        guard let sortKey else { return tasks }
        return tasks.sorted { a, b in
            let va = value(a, for: sortKey)
            let vb = value(b, for: sortKey)
            // Rows without the value always sink, either direction (web TaskTable).
            if (va == "") != (vb == "") { return vb == "" }
            if sortKey == .state {
                let ia = Self.stateOrder.firstIndex(of: va) ?? Int.max
                let ib = Self.stateOrder.firstIndex(of: vb) ?? Int.max
                if ia != ib { return sortAsc ? ia < ib : ia > ib }
                return a.line < b.line
            }
            if va != vb { return sortAsc ? va < vb : va > vb }
            return a.line < b.line
        }
    }

    private func value(_ item: TaskItem, for key: SortKey) -> String {
        switch key {
        case .state: return item.state
        case .sched: return item.scheduled ?? ""
        case .due: return item.due ?? ""
        }
    }

    @ViewBuilder
    private func dateCell(_ date: String?, prefix: String, line: Int, field: DateField) -> some View {
        if let date {
            Button("\(prefix) \(date)") { onPickDate(line, field) }
                .buttonStyle(.plain).font(.caption).foregroundStyle(.secondary)
        } else {
            Button("—") { onPickDate(line, field) }
                .buttonStyle(.plain).font(.caption).foregroundStyle(.tertiary)
        }
    }

    /// Quiet chip for task metadata (design.md Task table / quiet chip — the
    /// same capsule TaskBoard cards use).
    private func noteTaskChip(_ label: String, emphasis: Bool = false) -> some View {
        Text(label)
            .font(.caption)
            .foregroundStyle(emphasis ? .secondary : .tertiary)
            .padding(.horizontal, 5)
            .padding(.vertical, 2)
            .background(Color.secondary.opacity(0.08), in: Capsule())
    }
}

/// Date picker behind the note task table's SCHED/DUE cells (web TaskControls
/// date cells → setTaskDate with "" clearing the token).
private struct NoteTaskDateEditor: View {
    let target: NoteTaskDateTarget
    let onSave: (DateField, String) -> Void
    @State private var date = Date()
    @State private var field: DateField
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) private var colorScheme

    init(target: NoteTaskDateTarget, onSave: @escaping (DateField, String) -> Void) {
        self.target = target
        self.onSave = onSave
        _field = State(initialValue: target.field)
    }

    var body: some View {
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
