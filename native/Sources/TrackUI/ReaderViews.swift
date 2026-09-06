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

    public init(client: TrackClient) {
        _search = State(initialValue: SearchModel(client: client))
        _reader = State(initialValue: NoteReaderModel(client: client))
    }

    public var body: some View {
        NavigationSplitView {
            VStack(alignment: .leading, spacing: 0) {
                TextField("Search", text: $query)
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .onSubmit { Task { await search.search(query: query) } }
                Divider()
                List(search.results, id: \.ref.noteID) { result in
                    Button {
                        Task { await reader.open(result.qualifiedID) }
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(result.ref.title).font(.body)
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
    }
}

// MARK: - Note detail

public struct NoteReaderView: View {
    @Bindable var model: NoteReaderModel

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
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        Text(response.note.summary.ref.title)
                            .font(.title2).fontWeight(.medium)
                        MarkdownBody(markdown: response.note.body)
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
        }
    }
}

// MARK: - Markdown

/// Native Markdown rendering from the engine's GFM body. `[[wikilink]]`
/// source text does not resolve yet — tapping it is a follow-up
/// (resolve via `/api/resolve`, then `open`).
struct MarkdownBody: View {
    let markdown: String

    var body: some View {
        Group {
            if let attributed = try? AttributedString(markdown: markdown) {
                Text(attributed)
            } else {
                // The body is always plain text; never fail the whole note
                // because one construct did not parse.
                Text(markdown).font(.body).textSelection(.enabled)
            }
        }
        .textSelection(.enabled)
    }
}
