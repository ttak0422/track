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

                        // Author-assigned metadata above the body: tags and
                        // flags (ADR 0074) from the summary, mirroring
                        // NoteReaderStatic's stamp + tag strip. created/updated
                        // are not on the native NoteDetail yet, so they are
                        // intentionally absent rather than invented.
                        if let tags = response.note.summary.tags, !tags.isEmpty {
                            HStack(spacing: 8) {
                                ForEach(tags, id: \.self) { tag in
                                    Text("#\(tag)").font(.caption).foregroundStyle(.secondary)
                                }
                            }
                        }
                        if let flags = response.note.summary.ref.flags, !flags.isEmpty {
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
