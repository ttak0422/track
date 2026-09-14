import Foundation
import AppKit
import MarkdownUI
import SwiftUI
import TrackAPI
import WebKit

// GFM Markdown rendering for the native reader, backed by MarkdownUI
// (gonzalezreal/MarkdownUI), whose parser enables the cmark-gfm table,
// tasklist, strikethrough and autolink extensions. Tables, task lists,
// strikethrough and bare-URL autolinks therefore render as themselves — the
// old `AttributedString(markdown:)` path and hand-rolled swift-markdown
// renderer could not draw them.
//
// Visual language follows docs/spec/design.md via `Theme.trackReader` below:
// three type sizes, ink links with a stated-rule underline, muted mono inline
// code with no chip, sunk panel-soft code blocks, horizontal-only table rules,
// and plain blockquotes. Rich fences are drawn as FigureHost
// islands (mermaid/math/echarts/viewspec, plus dot/graphviz/d2/drawio/map via
// the same shell and a ```track-view JSON payload as native SwiftUI);
// ```track-query is expanded server-side by `/api/render` into ```track-view,
// and the raw fallback keeps the fence as a code block (web QueryView's
// "show the source" fallback).
// `$` math lines and `![[...]]` include lines are lifted out of the prose, and
// `[[wikilink]]` targets are rewritten to `trackwiki://` standard links. Inline
// trackwiki links navigate through the `\.openURL` environment installed by the
// reader (NoteReaderView); a "Links" rail (web reader's WikiLink) wired to
// `onWikilink` stays as the primary navigation surface.
//
// MarkdownUI does not expose footnote or source-position nodes. MarkdownAnchors
// preserves source lines while assigning SwiftUI scroll IDs and footnote links.

public struct GFMBody: View {
    let markdown: String
    let baseURL: URL
    let vault: String
    let noteID: TrackID?
    let includes: [NoteInclude]?
    let client: TrackClient?
    var onWikilink: ((String) -> Void)?
    var onTaskToggle: ((Int, Bool) -> Void)?
    let taskLineMap: [Int?]?
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.trackFontScale) private var fontScale
    @AppStorage(TrackAppearance.contentWidthKey) private var contentWidthRaw: String?

    public init(
        markdown: String,
        baseURL: URL,
        vault: String,
        noteID: TrackID? = nil,
        includes: [NoteInclude]? = nil,
        client: TrackClient? = nil,
        onWikilink: ((String) -> Void)? = nil,
        onTaskToggle: ((Int, Bool) -> Void)? = nil,
        taskLineMap: [Int?]? = nil
    ) {
        self.markdown = markdown
        self.baseURL = baseURL
        self.vault = vault
        self.noteID = noteID
        self.includes = includes
        self.client = client
        self.onWikilink = onWikilink
        self.onTaskToggle = onTaskToggle
        self.taskLineMap = taskLineMap
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // The body is rendered as an alternating run of MarkdownUI spans
            // and FigureHost islands: rich fences the native app draws
            // (mermaid, math, echarts, viewspec) are lifted out of the
            // prose into figure segments, everything else stays MarkdownUI.
            segmentedBody

            // The wikilink rail (web reader's WikiLink): distinct targets are
            // collected from the *original* source and shown as tappable links
            // that resolve via `/api/resolve`. A target that does not resolve
            // shows greyed out and disabled (web WikiLink's "unresolved").
            // Inline `[[...]]` render as `trackwiki://` links that the reader's
            // `\.openURL` interception routes to the same `onWikilink`.
            let links = Self.wikilinks(in: markdown)
            if !links.isEmpty {
                Divider()
                Text("Links").trackSectionLabel()
                ForEach(links, id: \.self) { target in
                    WikilinkRailRow(target: target, client: client, sourceID: noteID) {
                        onWikilink?(target)
                    }
                }
            }
        }
    }

    /// The alternating MarkdownUI / FigureHost run, built once per render by
    /// `Self.segments` from the spliced + fenced source. The Markdown spans
    /// wear `Theme.trackReader` (design.md translation); prose width is capped
    /// by the caller (two measures), while figure/media islands bleed full.
    private var segmentedBody: some View {
        let segments = Self.segments(markdown: markdown, includes: includes)
        let palette = TrackTheme.palette(for: colorScheme)
        let proseWidth = ContentWidthMode(stored: contentWidthRaw).proseWidth(scale: fontScale)
        let theme = Theme.trackReader(palette: palette, scale: fontScale, proseWidth: proseWidth)
        return VStack(alignment: .leading, spacing: 0) {
          ForEach(Array(segments.enumerated()), id: \.offset) { _, segment in
            Group {
              switch segment {
            case .anchor(let id):
                Color.clear.frame(height: 0).id(id).accessibilityHidden(true)
            case .markdown(let text), .heading(let text, _):
                Markdown(text)
                    .markdownTheme(theme)
                    .markdownImageProvider(TrackAssetImageProvider(baseURL: baseURL, vault: vault))
                    .textSelection(.enabled)
                    // Cap prose blocks individually so ordinary code and
                    // tables share the full figure width within a Markdown run.
                    .frame(maxWidth: .infinity, alignment: .leading)
            case .task(let task):
                let line = taskSourceLine(task.line)
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Button {
                        if let line { onTaskToggle?(line, !task.completed) }
                    } label: {
                        Image(systemName: task.completed ? "checkmark.square.fill" : "square")
                    }
                    .buttonStyle(.plain)
                    .disabled(onTaskToggle == nil || line == nil)
                    Markdown(task.text)
                        .markdownTheme(theme)
                        .textSelection(.enabled)
                }
                .padding(.leading, 8)
                .frame(maxWidth: proseWidth, alignment: .leading)
            case .figure(let figure):
                FigureSegmentView(
                    figure: figure,
                    noteID: noteID,
                    vault: vault,
                    baseURL: baseURL,
                    client: client,
                    onWikilink: onWikilink
                )
                .frame(maxWidth: .infinity, alignment: .leading)
            case .media(let media):
                MediaSegmentView(media: media, baseURL: baseURL, vault: vault, client: client)
                    .frame(maxWidth: .infinity, alignment: .leading)
            case .include(let include):
                IncludeCardView(include: include, onWikilink: onWikilink)
                    .frame(maxWidth: proseWidth, alignment: .leading)
              }
            }
            // MarkdownUI discards margins at a span's boundary. Segment
            // spacing owns those boundaries; nested Markdown retains its margins.
            .padding(.top, max(0, segment.headingLead - 13) * fontScale)
            .padding(.bottom, segment.isAnchor ? 0 : 13 * fontScale)
          }
        }
    }

    private func taskSourceLine(_ renderedLine: Int) -> Int? {
        guard let taskLineMap else { return renderedLine }
        guard taskLineMap.indices.contains(renderedLine - 1) else { return nil }
        return taskLineMap[renderedLine - 1].map { $0 + 1 }
    }

    // MARK: - Segmenting (fence-aware)

    /// One run of the body: a Markdown span (already wikilink-rewritten, with
    /// unrenderable rich fences collapsed to placeholders), a figure the native
    /// app draws through FigureHost, or a media embed drawn through the
    /// MediaEmbeds views.
    enum Segment {
        case markdown(String)
        case heading(String, Int)
        case anchor(String)
        case figure(Figure)
        case media(Media)
        case include(NoteInclude)
        case task(TaskLine)

        var headingLead: Double {
            guard case .heading(_, let level) = self else { return 0 }
            switch level {
            case 1, 2: return 44
            case 3: return 26
            default: return 20
            }
        }

        var isAnchor: Bool {
            if case .anchor = self { return true }
            return false
        }
    }

    struct TaskLine {
        let line: Int
        let completed: Bool
        let text: String
    }

    /// A `![alt](src)` line lifted out of the prose and drawn as one of the
    /// MediaEmbeds views, carrying the src to route by type.
    struct Media {
        let src: String
        let alt: String
    }

    /// A rich fence (or math line) lifted out of the prose, carrying exactly
    /// the source the shell renderer needs.
    struct Figure {
        enum Kind: Equatable {
            case mermaid
            case math(display: Bool)
            case echarts
            case viewspec
            case dot
            case d2
            case drawio
            case mindmap
            case map
            case trackView
            case taskboard
            case dashboard
        }

        let kind: Kind
        let source: String
    }

    /// Fences rendered as real figures (FigureHost) — never collapsed.
    private static let figureFences: [String: Figure.Kind] = [
        "mermaid": .mermaid,
        "echarts": .echarts,
        "viewspec": .viewspec,
        "dot": .dot,
        "graphviz": .dot,
        "d2": .d2,
        "drawio": .drawio,
        "mindmap": .mindmap,
        "map": .map,
        "track-view": .trackView,
        "taskboard": .taskboard,
        "dashboard": .dashboard,
    ]

    /// Fences the native app still cannot draw — collapsed to a placeholder.
    /// `track-query` used to be here, but `/api/render` already expands it
    /// server-side into a ```track-view fence (drawn natively below), and the
    /// raw fallback now keeps the fence as a code block — the web QueryView's
    /// "show the source" fallback — instead of a placeholder line. `taskboard`
    /// is a real figure above, so nothing remains unrenderable here.
    private static let placeholderFences: Set<String> = []

    /// Split `markdown` into an ordered run of Markdown spans, media embeds,
    /// and figure segments. `includes` (resolved `![[...]]` directives,
    /// aligned to `markdown` lines) are expanded in place first. Standalone
    /// `![alt](src)` media lines are lifted into their own media segment
    /// (routed by src type at render time); fences that map to a figure are
    /// lifted into their own segment; unrenderable rich fences collapse to a
    /// placeholder line; everything else has footnotes/task chips processed
    /// and `[[wikilink]]` targets rewritten to `trackwiki://` links.
    static func segments(markdown: String, includes: [NoteInclude]?) -> [Segment] {
        let spliceIn = includes ?? []
        let source = spliceIn.isEmpty ? markdown : spliceIncludes(markdown, spliceIn)
        let document = MarkdownAnchors.prepare(source)
        let preprocessed = Self.preprocess(document.lines.joined(separator: "\n"))
        let lines = preprocessed.components(separatedBy: "\n")
        let headingsByLine = Dictionary(uniqueKeysWithValues: document.headings.map { ($0.line, $0) })

        // Each native span is parsed separately. Supply shared link definitions
        // to every span so splitting at a heading cannot break [label][ref].
        let references = MarkdownAnchors.proseLines(lines).compactMap { _, line in
            line.range(of: #"^ {0,3}\[(?!\^)[^\]]+\]:\s*\S"#, options: .regularExpression) == nil ? nil : line
        }.joined(separator: "\n")
        var segments: [Segment] = []
        var buf: [String] = []
        var i = 0
        let flush = {
            if !buf.isEmpty {
                segments.append(.markdown(buf.joined(separator: "\n") + (references.isEmpty ? "" : "\n\n" + references)))
                buf = []
            }
        }

        while i < lines.count {
            let line = lines[i]
            let trimmed = line.trimmingCharacters(in: .whitespaces)

            if let ids = document.anchors[i] {
                // A paragraph/list marker belongs to its containing block.
                // ponytail: nested list anchors land at the containing list;
                // source-position-aware MarkdownUI nodes would allow exact rows.
                // Preserve that block's Markdown (including list numbering).
                let tailStart = headingsByLine[i] != nil ? buf.count : (buf.lastIndex(of: "").map { $0 + 1 } ?? 0)
                let tail = Array(buf[tailStart...])
                buf = Array(buf[..<tailStart])
                flush()
                for id in ids { segments.append(.anchor(id)) }
                buf = tail
            }

            if line.hasPrefix("    ") || line.hasPrefix("\t") {
                buf.append(line)
                i += 1
                continue
            }

            if let marker = Self.includeMarkerIndex(trimmed),
               let include = spliceIn.first(where: { $0.line == marker }) {
                flush()
                segments.append(.include(include))
                i += 1
                continue
            }

            if let task = Self.taskLine(trimmed) {
                flush()
                segments.append(.task(TaskLine(line: i, completed: task.completed, text: task.text)))
                i += 1
                continue
            }

            // Consume a whole CommonMark fence before considering its contents
            // as headings, links, tasks, media, or math.
            if let opening = Self.fence(line) {
                let langName = opening.info.split(whereSeparator: \.isWhitespace).first.map(String.init) ?? ""
                let start = i
                i += 1
                var body: [String] = []
                while i < lines.count {
                    if let closing = Self.fence(lines[i]), closing.marker.first == opening.marker.first,
                       closing.marker.count >= opening.marker.count, closing.info.isEmpty { break }
                    body.append(lines[i])
                    i += 1
                }
                let closed = i < lines.count
                if closed { i += 1 }
                if closed, let kind = figureFences[langName] {
                    flush()
                    var figureSource = body.joined(separator: "\n")
                    if kind == .mindmap && figureSource.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        figureSource = Self.tocEntries(in: source).map {
                            String(repeating: "#", count: $0.level) + " " + $0.title
                        }.joined(separator: "\n")
                    }
                    segments.append(.figure(Figure(kind: kind, source: figureSource)))
                } else {
                    // Ordinary and unclosed fences remain exactly their source.
                    buf.append(contentsOf: lines[start..<i])
                }
                continue
            }

            // Standalone media embeds: a line that is exactly `![alt](src)`.
            if let media = Self.standaloneImage(trimmed) {
                flush()
                segments.append(.media(Media(src: media.src, alt: media.alt)))
                i += 1
                continue
            }

            // Math lines: `$$...$$` (display) and `$...$` (inline), each on a
            // line of its own.
            if !trimmed.hasPrefix("```"), trimmed.hasPrefix("$") && trimmed.hasSuffix("$") {
                flush()
                if trimmed.hasPrefix("$$") {
                    segments.append(.figure(Figure(kind: .math(display: true), source: stripMathDelims(trimmed, "$$"))))
                } else {
                    segments.append(.figure(Figure(kind: .math(display: false), source: stripMathDelims(trimmed, "$"))))
                }
                i += 1
                continue
            }

            // MarkdownUI does not expose inline math nodes. Split an ordinary
            // prose line around `$...$` so the same FigureHost math renderer is
            // used for inline expressions as for display math.
            if !trimmed.hasPrefix("```"), let inline = Self.inlineMath(in: line) {
                flush()
                if !inline.before.isEmpty { segments.append(.markdown(Self.styleAlert(Self.rewriteWikilinks(inline.before)))) }
                segments.append(.figure(Figure(kind: .math(display: false), source: inline.source)))
                if !inline.after.isEmpty { buf.append(Self.styleAlert(Self.rewriteWikilinks(inline.after))) }
                i += 1
                continue
            }

            let styled = MarkdownAnchors.transformProse(line) { Self.styleAlert(Self.rewriteWikilinks($0)) }
            if let heading = headingsByLine[i] {
                segments.append(.heading(styled + (references.isEmpty ? "" : "\n\n" + references), heading.level))
            } else {
                buf.append(styled)
            }
            i += 1
        }
        flush()
        return segments
    }

    // MARK: - Preprocessing (footnotes + task chips)

    /// Emphasize task chips outside fenced and inline code. Anchor preparation
    /// handles footnotes first while retaining every original source line.
    static func preprocess(_ source: String) -> String {
        var lines = source.components(separatedBy: "\n")
        for (index, line) in MarkdownAnchors.proseLines(lines) {
            lines[index] = MarkdownAnchors.transformProse(line) { text in
                Self.replaceMatches(text, regex: taskChipRegex) { _, value in "**\(value)**" }
            }
        }
        return lines.joined(separator: "\n")
    }

    /// The task-chip tokens: priority `[#A]`, `[sched:…]`, `[due:…]`,
    /// `[done:…]`, and cookie counters `[1/2]`/`[50%]` (web
    /// remarkTaskLine's taskTokenPattern).
    private static let taskChipRegex = try! NSRegularExpression(
        pattern: "\\[(?:#[A-Za-z]|(?:sched|due|done):\\d{4}-\\d{2}-\\d{2}|\\d+\\/\\d+|\\d+%)\\]"
    )

    private static func replaceMatches(_ text: String, regex: NSRegularExpression, _ transform: (String, String) -> String) -> String {
        let matches = regex.matches(in: text, range: NSRange(text.startIndex..., in: text)).reversed()
        var result = text
        for match in matches {
            guard let range = Range(match.range, in: result) else { continue }
            let value = String(result[range])
            result.replaceSubrange(range, with: transform(value, value))
        }
        return result
    }

    typealias TocEntry = MarkdownAnchors.Heading

    static func tocEntries(in source: String) -> [TocEntry] {
        MarkdownAnchors.headings(source)
    }

    private static func fence(_ line: String) -> (marker: String, info: String)? {
        guard line.prefix(while: { $0 == " " }).count <= 3 else { return nil }
        let text = line.drop(while: { $0 == " " })
        guard let first = text.first, first == "`" || first == "~" else { return nil }
        let marker = text.prefix(while: { $0 == first })
        guard marker.count >= 3 else { return nil }
        return (String(marker), text.dropFirst(marker.count).trimmingCharacters(in: .whitespaces))
    }

    private static func inlineMath(in line: String) -> (before: String, source: String, after: String)? {
        guard let start = line.firstIndex(of: "$"),
              let end = line[line.index(after: start)...].firstIndex(of: "$"), end > line.index(after: start) else { return nil }
        let before = String(line[..<start])
        let source = String(line[line.index(after: start)..<end])
        let after = String(line[line.index(after: end)...])
        guard !source.contains("$") else { return nil }
        return (before, source, after)
    }

    /// "Exactly an image line" — `![alt](src)` with nothing else on the line,
    /// so a media embed line can be lifted into its own segment. Inline images
    /// inside prose stay in the Markdown span.
    private static func standaloneImage(_ trimmed: String) -> (src: String, alt: String)? {
        guard trimmed.hasPrefix("![") else { return nil }
        guard trimmed.hasSuffix(")") else { return nil }
        // Locate the `](` that separates alt from src (the alt may itself hold
        // a `]`, e.g. markup); take the first `](` after the opening `![`.
        guard let delimit = trimmed.range(of: "](") else { return nil }
        let alt = String(trimmed[trimmed.index(trimmed.startIndex, offsetBy: 2)..<delimit.lowerBound])
        let src = String(trimmed[delimit.upperBound..<trimmed.index(before: trimmed.endIndex)])
        guard !src.isEmpty else { return nil }
        return (src, alt)
    }

    private static func taskLine(_ line: String) -> (completed: Bool, text: String)? {
        guard line.hasPrefix("- [") || line.hasPrefix("* [") || line.hasPrefix("+ [") else { return nil }
        let start = line.index(line.startIndex, offsetBy: 2)
        guard line[start] == "[", line.index(start, offsetBy: 2) < line.endIndex,
              " xX".contains(line[line.index(after: start)]), line[line.index(start, offsetBy: 2)] == "]" else { return nil }
        let completed = line[line.index(start, offsetBy: 1)] != " "
        let contentStart = line.index(start, offsetBy: 3)
        return (completed, String(line[contentStart...]).trimmingCharacters(in: .whitespaces))
    }

    // MARK: - Media routing

    /// Classify a media src by the same rules the web reader's `Embed.tsx`
    /// routes an embed (using MediaEmbedsURLs), so a native segment draws the
    /// same surface the web iframe would.
    enum MediaKind {
        case youtube
        case maps
        case pdf
        /// A vault-local text-file attachment (non-image asset).
        case assetText
        case htmlAsset
        /// An http(s) page url that is not a recognised embed → Open Graph card.
        case ogp
        /// Everything else: a plain image, drawn by the existing image provider
        /// (http image, `assets/…` image, data: URI, relative path).
        case image
    }

    /// Resolve the asset href for a vault-local `assets/…` src (the web's
    /// `assetHref`): only path forms that name a vault asset count; nil for
    /// anything with a scheme or an absolute path.
    static func assetHref(_ src: String, vault: String, baseURL: URL) -> URL? {
        let trimmed = src.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return nil }
        if let url = URL(string: trimmed), ["http", "https", "data"].contains(url.scheme?.lowercased() ?? "") {
            return nil
        }
        if trimmed.hasPrefix("/") || trimmed.hasPrefix("./") {
            // "/" prefixed names the vault root's web path, not an asset name;
            // "./" is a relative reference MarkdownUI resolves elsewhere. Treat
            // as non-asset unless it clearly is an assets/ name.
            var normalized = trimmed
            if normalized.hasPrefix("./") { normalized = String(normalized.dropFirst(2)) }
            guard normalized.hasPrefix("assets/") else { return nil }
            return Self.assetURL(name: String(normalized.dropFirst("assets/".count)), vault: vault, baseURL: baseURL)
        }
        var name = trimmed
        if name.hasPrefix("assets/") { name = String(name.dropFirst("assets/".count)) }
        return Self.assetURL(name: name, vault: vault, baseURL: baseURL)
    }

    private static func assetURL(name: String, vault: String, baseURL: URL) -> URL? {
        guard !name.isEmpty else { return nil }
        var components = URLComponents(url: baseURL.appendingPathComponent("api/asset"), resolvingAgainstBaseURL: false)
        var items = [URLQueryItem(name: "name", value: name)]
        if !vault.isEmpty { items.append(URLQueryItem(name: "vault", value: vault)) }
        components?.queryItems = items
        return components?.url
    }

    /// Whether a src (resolved to a target) names a PDF by extension (the
    /// web's `isPdfHref`, applied at this simplified surface).
    private static func isPdf(_ src: String) -> Bool {
        let path = src.split(separator: "?", maxSplits: 1).first.map(String.init) ?? src
        return path.lowercased().hasSuffix(".pdf")
    }

    /// Whether a src names an image by extension (the web's `isImageHref`).
    private static func isImageHref(_ src: String) -> Bool {
        let path = src.split(separator: "?", maxSplits: 1).first.map(String.init) ?? src
        let lower = path.lowercased()
        return [".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif"].contains { lower.hasSuffix($0) }
    }

    private static func isHTMLHref(_ src: String) -> Bool {
        let path = src.split(separator: "?", maxSplits: 1).first.map(String.init) ?? src
        return path.lowercased().hasSuffix(".html") || path.lowercased().hasSuffix(".htm")
    }

    /// The MediaEmbedsURLs `webHref` upgrade (bare domain → https), mirrored
    /// so the media segment can hand the same string the web does.
    static func webHref(_ src: String) -> String {
        MediaEmbedsURLs.webHref(src)
    }

    /// `webHref` as a URL, nil when the result is not parseable.
    static func webHrefURL(_ href: String) -> URL? {
        URL(string: MediaEmbedsURLs.webHref(href))
    }

    /// Classify a media src into its routing kind. `asset` is the resolved
    /// `/api/asset` URL when the src names a vault-local asset, else nil. The
    /// order mirrors the web's `Embed.tsx`: a vault-local asset is never a
    /// YouTube/Maps/OGP URL; a PDF name wins over everything else; a
    /// non-image asset is a text file; a non-image http(s) page is an OGP
    /// card; anything left is a plain image.
    static func classify(src: String, asset: URL?) -> MediaKind {
        // A vault-local asset is never a YouTube/Maps URL (web Embed.tsx).
        if asset == nil {
            if MediaEmbedsURLs.youtubeEmbedURL(from: src) != nil {
                return .youtube
            }
            if MediaEmbedsURLs.googleMapsEmbedURL(from: src) != nil {
                return .maps
            }
        }
        if isPdf(src) {
            return .pdf
        }
        if let _ = asset, isHTMLHref(src) {
            return .htmlAsset
        }
        if let _ = asset, !isImageHref(src) {
            return .assetText
        }
        if asset == nil, !isImageHref(src) {
            let target = MediaEmbedsURLs.webHref(src)
            if target.lowercased().hasPrefix("http://") || target.lowercased().hasPrefix("https://") {
                return .ogp
            }
        }
        return .image
    }

    /// A GitHub-style callout `> [!NOTE]` marker is left as plain prose: the
    /// web reader draws a plain quote, so the native reader does the same
    /// instead of inventing a labelled callout. Ordinary blockquotes pass
    /// through untouched.
    private static let alertRegex = try! NSRegularExpression(
        pattern: "^(\\s*)>\\s*(?:\\[\\!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\\])\\s*",
        options: [.caseInsensitive]
    )

    private static func styleAlert(_ line: String) -> String {
        return line
    }

    /// Strip a matched math delimiter pair from both ends of a line.
    private static func stripMathDelims(_ trimmed: String, _ delim: String) -> String {
        var s = trimmed
        if s.hasPrefix(delim) { s = String(s.dropFirst(delim.count)) }
        if s.hasSuffix(delim) { s = String(s.dropLast(delim.count)) }
        return s
    }

    /// Expand each resolved `![[...]]` directive at its 0-based `line` into the
    /// embed the native reader draws: a bold caption header plus the excerpt's
    /// lines as a blockquote, or the directive's error message. Only a line the
    /// splice still recognises as a directive is replaced (web
    /// spliceIncludeTokens' "trust but verify") so a stale line number never
    /// swallows unrelated prose.
    static func spliceIncludes(_ markdown: String, _ includes: [NoteInclude]) -> String {
        var lines = markdown.components(separatedBy: "\n")
        for inc in includes {
            guard inc.line >= 0, inc.line < lines.count else { continue }
            guard lines[inc.line].trimmingCharacters(in: .whitespaces).hasPrefix("![") else { continue }
            lines[inc.line] = Self.includeBlock(inc)
        }
        return lines.joined(separator: "\n")
    }

    /// Replace an include directive with a private marker so it can become a
    /// native card segment without flattening its excerpt into a blockquote.
    private static func includeBlock(_ inc: NoteInclude) -> String {
        return includeMarker(inc.line)
    }

    private static func includeMarker(_ line: Int) -> String {
        "<!-- track-native-include:\(line) -->"
    }

    private static func includeMarkerIndex(_ line: String) -> Int? {
        let prefix = "<!-- track-native-include:"
        guard line.hasPrefix(prefix), line.hasSuffix(" -->") else { return nil }
        return Int(line.dropFirst(prefix.count).dropLast(4))
    }

    /// `[[target|display]]` → `[display](trackwiki://pct-encoded-target)` and
    /// `[[target]]` → `[target](trackwiki://pct-encoded-target)`.
    ///
    /// `.urlPathAllowed` percent-encodes `#`, `:` and non-ASCII, so
    /// `[[title#anchor]]` and `[[vault:title]]` stay in the URL's host/path
    /// portion and `host + path` + `removingPercentEncoding` restores the
    /// exact target in the reader's `\.openURL` handler.
    private static func rewriteWikilinks(_ line: String) -> String {
        var result = ""
        var rest = Substring(line)
        while let open = rest.firstRange(of: "[["), let close = rest[open.upperBound...].firstRange(of: "]]") {
            result += rest[..<open.lowerBound]
            let inner = rest[open.upperBound..<close.lowerBound]
            let parts = inner.split(separator: "|", maxSplits: 1)
            let target = parts[0].trimmingCharacters(in: .whitespaces)
            let display = parts.count > 1
                ? String(parts[1]).trimmingCharacters(in: .whitespaces)
                : target
            if !target.isEmpty {
                result += "[\(display)](\(MarkdownAnchors.wikiURL(target).absoluteString))"
            }
            rest = rest[close.upperBound...]
        }
        result += rest
        return result
    }

    // MARK: - Wikilink rail

    /// Collect distinct `[[title]]`, `[[title#anchor]]`, `[[vault:title]]`
    /// targets in first-seen order, capped at 20 like the web reader's link
    /// rail. Anchors are kept verbatim so a tap can re-split them.
    static func wikilinks(in markdown: String, limit: Int = 20) -> [String] {
        var seen: [String] = []
        var set = Set<String>()
        let pattern = "\\[\\[([^\\]|]+)(?:\\|[^\\]]+)?\\]\\]"
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return [] }
        let ns = markdown as NSString
        let range = NSRange(location: 0, length: ns.length)
        regex.enumerateMatches(in: markdown, range: range) { match, _, _ in
            guard let match, let r = match.range(at: 1).optional else { return }
            let target = (ns.substring(with: r) as String).trimmingCharacters(in: .whitespaces)
            guard !target.isEmpty, !set.contains(target) else { return }
            set.insert(target)
            seen.append(target)
        }
        return Array(seen.prefix(limit))
    }
}

/// The design.md reading-surface translation for MarkdownUI: body 16px/1.85
/// ink, headings at body size told apart by space and rule (h1 stated rule
/// above, h2 hairline above, h3 faint section sign, h4 muted), links as ink
/// with a stated-rule underline, inline code as muted mono with no chip, and
/// Danger reserved for the call sites that own it (unresolved rail rows).
extension Theme {
    @MainActor
    static func trackReader(palette: TrackTheme, scale: Double, proseWidth: CGFloat = .infinity) -> Theme {
        let body = CGFloat(16 * scale)
        return Theme()
            .text {
                ForegroundColor(palette.text)
                FontFamily(.custom(TrackTypography.readingFamily))
                FontSize(body)
            }
            .code {
                FontFamily(.system(.monospaced))
                FontFamilyVariant(.monospaced)
                ForegroundColor(palette.muted)
                FontSize(body)
            }
            .strong {
                FontWeight(.bold)
            }
            .link {
                ForegroundColor(palette.text)
                UnderlineStyle(.single)
            }
            .paragraph { configuration in
                configuration.label
                    .relativeLineSpacing(.em(0.85))
                    .markdownMargin(bottom: .em(0.8125))
                    .frame(maxWidth: proseWidth, alignment: .leading)
            }
            .heading1 { configuration in
                VStack(alignment: .leading, spacing: 6) {
                    Divider().overlay(palette.lineStrong)
                    configuration.label
                        .markdownTextStyle { FontWeight(.bold) }
                }
                .markdownMargin(top: .em(2.75), bottom: .em(0.5))
                .frame(maxWidth: proseWidth, alignment: .leading)
            }
            .heading2 { configuration in
                VStack(alignment: .leading, spacing: 6) {
                    Divider().overlay(palette.line)
                    configuration.label
                        .markdownTextStyle {
                            FontWeight(.bold)
                        }
                }
                .markdownMargin(top: .em(2.75), bottom: .em(0.5))
                .frame(maxWidth: proseWidth, alignment: .leading)
            }
            .heading3 { configuration in
                HStack(alignment: .firstTextBaseline, spacing: 4) {
                    Text("§").foregroundStyle(palette.faint)
                    configuration.label
                        .markdownTextStyle { FontWeight(.bold) }
                }
                .markdownMargin(top: .em(1.625), bottom: .em(0.5))
                .frame(maxWidth: proseWidth, alignment: .leading)
            }
            .heading4 { configuration in
                configuration.label
                    .markdownMargin(top: .em(1.25), bottom: .em(0.5))
                    .frame(maxWidth: proseWidth, alignment: .leading)
                    .markdownTextStyle {
                        FontWeight(.bold)
                        ForegroundColor(palette.muted)
                    }
            }
            .list { configuration in
                configuration.label
                    .relativeLineSpacing(.em(0.85))
                    .frame(maxWidth: proseWidth, alignment: .leading)
            }
            .listItem { configuration in
                configuration.label.markdownMargin(top: .em(0.4375))
            }
            .blockquote { configuration in
                // Plain quote (web): no callout bar, no label, just the
                // words in secondary ink with a hairline at the left.
                HStack(spacing: 0) {
                    Rectangle().fill(palette.line).frame(width: 2)
                    configuration.label
                        .markdownTextStyle { ForegroundColor(palette.muted) }
                        .padding(.leading, 12)
                }
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: proseWidth, alignment: .leading)
            }
            .codeBlock { configuration in
                let language = configuration.language?.isEmpty == false ? configuration.language! : "Code"
                VStack(alignment: .leading, spacing: 0) {
                    HStack {
                        Text(language)
                            .trackSectionLabel()
                        Spacer()
                        Button("Copy") {
                            NSPasteboard.general.clearContents()
                            NSPasteboard.general.setString(configuration.content, forType: .string)
                        }
                        .buttonStyle(.borderless).font(.caption)
                    }
                    .padding(.horizontal, 10).padding(.vertical, 6)
                    Divider().overlay(palette.line)
                    ScrollView(.horizontal) {
                        configuration.label
                            .markdownTextStyle {
                                FontFamily(.system(.monospaced))
                                FontSize(14 * scale)
                            }
                            .fixedSize(horizontal: false, vertical: true).padding(10)
                    }
                }
                .background(palette.panelSoft)
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }
            .table { configuration in
                ScrollView(.horizontal, showsIndicators: true) {
                    configuration.label
                        .markdownTableBorderStyle(TableBorderStyle(color: .clear))
                        .fixedSize(horizontal: false, vertical: true)
                }
                .markdownMargin(top: .zero, bottom: .em(1))
            }
            .tableCell { configuration in
                configuration.label
                    .markdownTextStyle {
                        ForegroundColor(configuration.row == 0 ? palette.text : palette.muted)
                        FontSize(14 * scale)
                        FontWeight(configuration.row == 0 ? .medium : .regular)
                    }
                    .overlay(alignment: .bottom) {
                        if configuration.row == 0 { Divider().overlay(palette.lineStrong) }
                        else { Divider().overlay(palette.line) }
                    }
                    .padding(.horizontal, 8).padding(.vertical, 5)
            }
    }
}

/// One row of the wikilink rail. Resolves its target through `/api/resolve`
/// on first appearance; a resolved one stays a tappable ink link with a
/// stated-rule underline (web variant 8), an unresolved one wears danger with
/// a dotted underline — a warning, not decoration.
private struct WikilinkRailRow: View {
    let target: String
    let client: TrackClient?
    let sourceID: TrackID?
    let onOpen: () -> Void
    @State private var resolved = false
    @State private var isPending = true
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        let palette = TrackTheme.palette(for: colorScheme)
        Group {
        if isPending {
            Text(target).font(.body).foregroundStyle(palette.muted)
                .task(id: target) { await resolve() }
        } else if resolved {
            Button { onOpen() } label: {
                Text(target)
                    .font(.body)
                    .foregroundStyle(palette.text)
                    .underline(color: palette.lineStrong)
            }
            .buttonStyle(.plain)
        } else {
            Text(target).font(.body).foregroundStyle(palette.danger)
                .underline(pattern: .dot, color: palette.danger)
        }
        }
        .modifier(RailPreview(client: client, target: target, sourceID: sourceID))
    }


    private func resolve() async {
        guard let client else { isPending = false; resolved = true; return }
        let parsed = MarkdownAnchors.target(target)
        if parsed.key.isEmpty, parsed.anchor != nil { isPending = false; resolved = true; return }
        let (explicitVault, term) = Self.split(parsed.key)
        let vault = explicitVault.isEmpty ? (sourceID?.split().vault ?? "") : explicitVault
        let found = (try? await client.resolveTerm(term, vault: vault))?.found ?? false
        resolved = found
        isPending = false
    }

    /// `vault:title#anchor` → (`vault`, `title`); a bare `title` keeps the
    /// empty vault (mirrors NoteReaderModel.splitWikilink).
    private static func split(_ target: String) -> (vault: String, term: String) {
        let trimmed = target.trimmingCharacters(in: .whitespaces)
        let noAnchor = MarkdownAnchors.target(trimmed).key
        if let colon = noAnchor.firstIndex(of: ":") {
            return (String(noAnchor[..<colon]), String(noAnchor[noAnchor.index(after: colon)...]))
        }
        return ("", noAnchor)
    }
}

private struct RailPreview: ViewModifier {
    @Environment(\.openURL) private var openURL
    let client: TrackClient?
    let target: String
    let sourceID: TrackID?
    func body(content: Content) -> some View {
        if let client {
            content.notePreview(client: client, target: target, sourceID: sourceID) { id in openURL(MarkdownAnchors.wikiURL(id.raw)) }
        } else { content }
    }
}

// MARK: - Figure segments

/// A single lifted figure segment: mermaid, KaTeX math, an echarts option (from
/// a ```echarts fence or a ```viewspec fence the server resolved via
/// `/api/viewspec`), Graphviz/D2/draw.io diagrams, a Leaflet map, an indented
/// mindmap outline (converted to mermaid), and a ```track-view JSON payload
/// drawn as native SwiftUI. `viewspec` is resolved lazily on first appearance;
/// until then a placeholder stands in, and a resolution failure keeps the
/// spec's source as the placeholder text.
private struct FigureSegmentView: View {
    let figure: GFMBody.Figure
    let noteID: TrackID?
    let vault: String
    let baseURL: URL
    let client: TrackClient?
    let onWikilink: ((String) -> Void)?
    @State private var height: CGFloat
    @State private var resolvedOption: String?
    @Environment(\.colorScheme) private var colorScheme

    init(
        figure: GFMBody.Figure,
        noteID: TrackID?,
        vault: String,
        baseURL: URL,
        client: TrackClient?,
        onWikilink: ((String) -> Void)?
    ) {
        self.noteID = noteID
        self.figure = figure
        self.vault = vault
        self.baseURL = baseURL
        self.client = client
        self.onWikilink = onWikilink
        switch figure.kind {
        case .echarts, .viewspec:
            _height = State(initialValue: FigureAssets.defaultEchartsHeight)
        case .map:
            _height = State(initialValue: FigureAssets.defaultMapHeight)
        default:
            _height = State(initialValue: 40)
        }
    }

    var body: some View {
        switch figure.kind {
        case .mermaid:
            host(.mermaid(figure.source))
        case .math(let display):
            host(.math(figure.source, display: display))
        case .echarts:
            host(.echarts(optionJSON: figure.source))
        case .viewspec:
            viewspecBody
        case .dot:
            host(.dot(figure.source))
        case .d2:
            host(.d2(figure.source))
        case .drawio:
            host(.drawio(figure.source))
        case .mindmap:
            mindmapBody
        case .map:
            mapBody
        case .trackView:
            trackViewBody
        case .taskboard:
            taskboardBody
        case .dashboard:
            dashboardBody
        }
    }

    @ViewBuilder
    private var viewspecBody: some View {
        if let option = resolvedOption {
            host(.echarts(optionJSON: option))
        } else {
            HStack(spacing: 8) {
                ProgressView().controlSize(.small)
                Text("Resolving chart…")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .task(id: figure.source) { await resolve() }
            .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
                Task { await resolve() }
            }
        }
    }

    /// The ```mindmap fence is an indented outline; convert it to mermaid
    /// mindmap syntax and feed the existing mermaid engine. An empty fence maps
    /// nothing and reads as the web's "nothing to map" notice.
    @ViewBuilder
    private var mindmapBody: some View {
        let source = MindmapOutline.mermaidSource(figure.source)
        if source.isEmpty {
            Text("Mindmap: nothing to map (no headings).")
                .font(.caption)
                .foregroundStyle(.secondary)
        } else {
            host(.mermaid(source))
        }
    }

    /// A ```map fence parses to a MapFigure; malformed source keeps the fence
    /// as a code block (web MapFence's CodeBlock fallback).
    @ViewBuilder
    private var mapBody: some View {
        if let figure = MapFigure.parse(self.figure.source) {
            VStack(alignment: .leading, spacing: 6) {
                host(.map(figure))
                if let url = URL(string: "https://www.openstreetmap.org/?mlat=\(figure.lat)&mlon=\(figure.long)#map=\(figure.zoom)/\(figure.lat)/\(figure.long)") {
                    Link(destination: url) {
                        Label("Open map", systemImage: "map")
                            .font(.caption)
                    }
                }
            }
        } else {
            VStack(alignment: .leading, spacing: 6) {
                codeBlock(self.figure.source)
                if let url = URL(string: "https://www.openstreetmap.org/search?query=" + (self.figure.source.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? "map")) {
                    Link("Open map", destination: url).font(.caption)
                }
            }
        }
    }

    /// A ```track-view fence holds a server-resolved View JSON payload (web
    /// QueryView). A payload this build understands draws natively; anything
    /// else shows the source (web's CodeBlock fallback).
    @ViewBuilder
    private var trackViewBody: some View {
        if let payload = TrackViewPayload.parse(figure.source) {
            TrackViewFigure(
                payload: payload,
                source: figure.source,
                vault: vault,
                baseURL: baseURL,
                onWikilink: onWikilink
            )
        } else {
            codeBlock(figure.source)
        }
    }

    /// The standalone taskboard uses the same observable model and write path
    /// as the native Tasks screen. It intentionally reuses the existing board
    /// (including its per-card state picker); the fence does not add drag and
    /// drop-specific behavior of its own.
    @ViewBuilder
    private var taskboardBody: some View {
        if let client, let noteID {
            InlineTaskBoard(client: client, noteID: noteID)
        } else {
            Label("Taskboard unavailable", systemImage: "rectangle.3.group")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private var dashboardBody: some View {
        if let payload = DashboardPayload.parse(figure.source) {
            DashboardFigure(payload: payload, onWikilink: onWikilink)
        } else {
            codeBlock(figure.source)
        }
    }

    private func resolve() async {
        guard let client, !figure.source.isEmpty else { return }
        guard let option = try? await client.renderViewSpec(spec: figure.source, vault: vault) else { return }
        resolvedOption = option
    }

    private func host(_ kind: FigureKind) -> some View {
        FigureHost(
            kind: kind,
            height: $height,
            theme: colorScheme == .dark ? .dark : .light,
            onLink: figureOnLink
        )
        .frame(height: height)
        .modifier(DarkGraphvizModifier(enabled: colorScheme == .dark && isGraphviz))
    }

    private var isGraphviz: Bool {
        if case .dot = figure.kind { return true }
        return false
    }

    /// Interprets a link tapped inside a figure island: a `trackwiki://` URL
    /// (map-marker popups) routes through `onWikilink`, everything else is
    /// ignored (the reader has no other link surface for figures).
    private func figureOnLink(_ url: URL) {
        guard url.scheme?.lowercased() == "trackwiki" else { return }
        let combined = (url.host ?? "") + url.path
        let target = combined.removingPercentEncoding ?? combined
        if !target.isEmpty { onWikilink?(target) }
    }

    /// A fallback for a figure whose source did not parse: the source shown as
    /// a monospaced block (web's CodeBlock), never an empty island.
    private func codeBlock(_ source: String) -> some View {
        Text(source)
            .font(.system(.caption, design: .monospaced))
            .foregroundStyle(.secondary)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct DarkGraphvizModifier: ViewModifier {
    let enabled: Bool

    func body(content: Content) -> some View {
        if enabled {
            content.colorInvert()
        } else {
            content
        }
    }
}

private struct InlineTaskBoard: View {
    let client: TrackClient
    let noteID: TrackID
    @State private var model: TasksModel?

    var body: some View {
        Group {
            if let model {
                VStack(alignment: .leading) {
                    if let message = model.error ?? model.lastConflict { Text(message).foregroundStyle(.red) }
                    if model.rows.isEmpty { Text("No tasks in this note.").foregroundStyle(.secondary) }
                    else { TaskBoard(model: model).frame(minHeight: 240) }
                }
            } else {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Loading tasks…").font(.caption).foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task(id: noteID) {
            let loaded = TasksModel(client: client, noteID: noteID)
            model = loaded
            await loaded.reload()
        }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            Task { await model?.reload() }
        }
    }
}

// MARK: - track-view payload

/// The JSON shape of a ```track-view fence (web QueryView's ViewPayload): a
/// layout plus already-grouped rows the server resolved. Only the `list`,
/// `board`, `gallery` and `calendar` layouts are drawn; anything else falls
/// back to the source.
private struct TrackViewPayload {
    let layout: String
    let showTitle: Bool
    let key: String?
    let columns: [String]
    let groups: [TrackViewGroup]

    static func parse(_ text: String) -> TrackViewPayload? {
        guard let data = text.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let layout = obj["layout"] as? String,
              let columns = obj["columns"] as? [String],
              let groupsRaw = obj["groups"] as? [[String: Any]] else { return nil }
        var groups: [TrackViewGroup] = []
        for group in groupsRaw {
            guard let rowsRaw = group["rows"] as? [[String: Any]] else { return nil }
            var rows: [TrackViewRow] = []
            for row in rowsRaw {
                guard let title = row["title"] as? String else { return nil }
                rows.append(TrackViewRow(
                    title: title,
                    cells: row["cells"] as? [String] ?? [],
                    cover: row["cover"] as? String,
                    icon: row["icon"] as? String
                ))
            }
            groups.append(TrackViewGroup(name: group["name"] as? String, rows: rows))
        }
        return TrackViewPayload(
            layout: layout,
            showTitle: obj["showTitle"] as? Bool ?? true,
            key: obj["key"] as? String,
            columns: columns,
            groups: groups
        )
    }
}

private struct DashboardPayload {
    let title: String?
    let widgets: [(title: String, items: [String])]

    static func parse(_ text: String) -> DashboardPayload? {
        guard let data = text.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        let title = object["title"] as? String
        guard let raw = object["widgets"] as? [[String: Any]] else { return nil }
        var widgets: [(String, [String])] = []
        for widget in raw {
            guard let name = widget["title"] as? String,
                  let items = widget["items"] as? [String] else { return nil }
            widgets.append((name, items))
        }
        return DashboardPayload(title: title, widgets: widgets)
    }
}

private struct DashboardFigure: View {
    let payload: DashboardPayload
    let onWikilink: ((String) -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let title = payload.title, !title.isEmpty {
                Text(title).font(.title3.weight(.semibold))
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 180), alignment: .top)], alignment: .leading, spacing: 12) {
                ForEach(Array(payload.widgets.enumerated()), id: \.offset) { _, widget in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(widget.title).font(.headline)
                        if widget.items.isEmpty {
                            Text("No items.").font(.caption).foregroundStyle(.secondary)
                        } else {
                            ForEach(widget.items, id: \.self) { item in
                                Button(item) { onWikilink?(item) }
                                    .buttonStyle(.link)
                            }
                        }
                    }
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color.primary.opacity(0.045))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct TrackViewGroup {
    let name: String?
    let rows: [TrackViewRow]
}

private struct TrackViewRow {
    let title: String
    let cells: [String]
    let cover: String?
    let icon: String?
}

/// Draws a parsed track-view payload as native SwiftUI, mirroring web
/// QueryView's list/board/gallery/calendar layouts without the table chrome.
/// An unknown layout shows the fence source (web QueryView's CodeBlock
/// fallback), never a hole in the page.
private struct TrackViewFigure: View {
    let payload: TrackViewPayload
    let source: String
    let vault: String
    let baseURL: URL
    let onWikilink: ((String) -> Void)?

    var body: some View {
        switch payload.layout {
        case "list":
            TrackViewList(payload: payload, onWikilink: onWikilink)
        case "board":
            TrackViewBoard(payload: payload, onWikilink: onWikilink)
        case "gallery":
            TrackViewGallery(payload: payload, vault: vault, baseURL: baseURL, onWikilink: onWikilink)
        case "calendar":
            TrackViewCalendar(payload: payload, onWikilink: onWikilink)
        default:
            Text(source)
                .font(.system(.body, design: .monospaced))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// A single "key value" meta line on a card or list row.
private struct TrackViewMeta: Identifiable {
    let column: String
    let value: String
    var id: String { column }
}

/// The "key value" meta lines for a row, minus the title column and the lane's
/// own grouping column (web QueryView.rowMeta).
private func trackViewRowMeta(_ row: TrackViewRow, _ payload: TrackViewPayload, skip: String? = nil) -> [TrackViewMeta] {
    payload.columns.enumerated().compactMap { index, column in
        guard column != "title", column != skip else { return nil }
        let value = index < row.cells.count ? row.cells[index] : ""
        guard !value.isEmpty else { return nil }
        return TrackViewMeta(column: column, value: value)
    }
}

private struct TrackViewList: View {
    let payload: TrackViewPayload
    let onWikilink: ((String) -> Void)?

    private var rows: [TrackViewRow] {
        payload.groups.flatMap(\.rows)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                VStack(alignment: .leading, spacing: 3) {
                    Button(row.title) { onWikilink?(row.title) }
                        .buttonStyle(.link)
                    ForEach(trackViewRowMeta(row, payload)) { meta in
                        Text("\(meta.column)  \(meta.value)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

private struct TrackViewBoard: View {
    let payload: TrackViewPayload
    let onWikilink: ((String) -> Void)?

    var body: some View {
        ScrollView(.horizontal) {
          HStack(alignment: .top, spacing: 12) {
            ForEach(Array(payload.groups.enumerated()), id: \.offset) { _, group in
                VStack(alignment: .leading, spacing: 6) {
                    HStack(spacing: 8) {
                        Text(group.name ?? "")
                            .font(.subheadline.weight(.semibold))
                        Text("\(group.rows.count)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    ForEach(group.rows, id: \.title) { row in
                        TrackViewCard(row: row, payload: payload, skip: payload.key, onWikilink: onWikilink)
                    }
                }
                .frame(width: 220, alignment: .topLeading)
            }
          }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct TrackViewGallery: View {
    let payload: TrackViewPayload
    let vault: String
    let baseURL: URL
    let onWikilink: ((String) -> Void)?

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 180), alignment: .top)], alignment: .leading, spacing: 12) {
            ForEach(Array(payload.groups.flatMap(\.rows).enumerated()), id: \.offset) { _, row in
                VStack(alignment: .leading, spacing: 6) {
                    Button { onWikilink?(row.title) } label: {
                        Color.clear.aspectRatio(16.0 / 9.0, contentMode: .fit)
                            .overlay { cover(row) }
                            .clipped()
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Open \(row.title)")
                    TrackViewCardBody(row: row, payload: payload, skip: nil, onWikilink: onWikilink)
                }
            }
        }
    }

    @ViewBuilder
    private func cover(_ row: TrackViewRow) -> some View {
        if let cover = row.cover, !cover.isEmpty {
            let url = GFMBody.assetHref(cover, vault: vault, baseURL: baseURL) ?? URL(string: cover)
            AsyncImage(url: url) { phase in
                if let image = phase.image {
                    image.resizable().scaledToFill()
                } else {
                    emptyCover(row)
                }
            }
        } else {
            emptyCover(row)
        }
    }

    @ViewBuilder
    private func emptyCover(_ row: TrackViewRow) -> some View {
        ZStack {
            Rectangle().fill(Color.secondary.opacity(0.15))
            if let icon = row.icon, !icon.isEmpty {
                Text(icon)
            } else {
                Image(systemName: "photo.on.rectangle.angled")
                    .font(.title2)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

private struct TrackViewCalendar: View {
    let payload: TrackViewPayload
    let onWikilink: ((String) -> Void)?

    private static let weekdays = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
    private static let cellNotes = 3

    private var byDay: [String: [TrackViewRow]] {
        var map: [String: [TrackViewRow]] = [:]
        for group in payload.groups {
            if let name = group.name { map[name] = group.rows }
        }
        return map
    }

    private var months: [String] {
        Array(Set(byDay.keys.map { String($0.prefix(7)) })).sorted()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(months, id: \.self) { month in
                monthGrid(month)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func monthGrid(_ month: String) -> some View {
        if let (year, monthNo, days, leading) = monthInfo(month) {
            let columns = Array(repeating: GridItem(.flexible(), spacing: 4), count: 7)
            VStack(alignment: .leading, spacing: 6) {
                Text("\(year) / \(String(format: "%02d", monthNo))")
                    .font(.subheadline.weight(.semibold))
                LazyVGrid(columns: columns, spacing: 4) {
                    ForEach(Self.weekdays, id: \.self) { day in
                        Text(day).font(.caption2).foregroundStyle(.secondary)
                    }
                    ForEach(0..<leading, id: \.self) { _ in
                        Color.clear.frame(height: 20)
                    }
                    ForEach(1...days, id: \.self) { day in
                        let date = "\(year)-\(String(format: "%02d", monthNo))-\(String(format: "%02d", day))"
                        dayCell(date: date, day: day)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func dayCell(date: String, day: Int) -> some View {
        let rows = byDay[date] ?? []
        VStack(alignment: .leading, spacing: 2) {
            Text("\(day)")
                .font(.caption2)
                .foregroundStyle(rows.isEmpty ? .secondary : .primary)
            ForEach(rows.prefix(Self.cellNotes), id: \.title) { row in
                Button(row.title) { onWikilink?(row.title) }
                    .buttonStyle(.link)
                    .font(.caption2)
                    .lineLimit(1)
            }
            if rows.count > Self.cellNotes {
                Text("+\(rows.count - Self.cellNotes)")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .padding(2)
    }

    private func monthInfo(_ month: String) -> (year: Int, monthNo: Int, days: Int, leading: Int)? {
        guard month.count == 7,
              let year = Int(month.prefix(4)),
              let monthNo = Int(month.suffix(2)) else { return nil }
        var comps = DateComponents()
        comps.year = year
        comps.month = monthNo
        comps.day = 1
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        guard let first = calendar.date(from: comps),
              let days = calendar.range(of: .day, in: .month, for: first)?.count else { return nil }
        let leading = (calendar.component(.weekday, from: first) + 6) % 7
        return (year, monthNo, days, leading)
    }
}

private struct TrackViewCard: View {
    let row: TrackViewRow
    let payload: TrackViewPayload
    let skip: String?
    let onWikilink: ((String) -> Void)?

    var body: some View {
        TrackViewCardBody(row: row, payload: payload, skip: skip, onWikilink: onWikilink)
            .padding(8)
            .background(RoundedRectangle(cornerRadius: 8).fill(Color.primary.opacity(0.05)))
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct TrackViewCardBody: View {
    let row: TrackViewRow
    let payload: TrackViewPayload
    let skip: String?
    let onWikilink: ((String) -> Void)?

    var body: some View {
        Button { onWikilink?(row.title) } label: {
            VStack(alignment: .leading, spacing: 3) {
                if payload.showTitle { Text(row.title) }
                ForEach(trackViewRowMeta(row, payload, skip: skip)) { meta in
                    Text("\(meta.column)  \(meta.value)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, minHeight: 24, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Open \(row.title)")
    }
}

// MARK: - Media segments

private struct IncludeCardView: View {
    let include: NoteInclude
    let onWikilink: ((String) -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let error = include.error, !error.isEmpty {
                Label(error, systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.secondary)
            } else {
                HStack {
                    if let title = include.title, !title.isEmpty, include.noteID != nil {
                        Button(include.caption.isEmpty ? title : include.caption) {
                            onWikilink?(include.noteID?.description ?? title)
                        }
                        .buttonStyle(.link).font(.headline)
                    } else {
                        Text(include.caption.isEmpty ? "Include" : include.caption)
                            .font(.headline)
                    }
                    Spacer()
                    Image(systemName: "arrow.up.right")
                        .font(.caption).foregroundStyle(.secondary)
                }
                if !include.lines.isEmpty {
                    Text(include.lines.joined(separator: "\n"))
                        .font(.body).foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
                ForEach(include.badOptions ?? [], id: \.self) { option in
                    Text("⚠ unknown option: \(option)")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.primary.opacity(0.035))
        .overlay(RoundedRectangle(cornerRadius: 6).stroke(Color.secondary.opacity(0.28)))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }
}

/// One lifted media embed, routed by src type the same way the web reader's
/// `Embed.tsx` routes a standalone `![alt](src)`: YouTube/Maps (FigureHost
/// islands via MediaEmbeds), PDF and vault-local text-file assets (MediaEmbeds
/// views), an http(s) page the card (`OgpCardView`), and otherwise a plain
/// image through the existing asset/image provider.
private struct MediaSegmentView: View {
    let media: GFMBody.Media
    let baseURL: URL
    let vault: String
    let client: TrackClient?
    @State private var previewURL: URL?
    @State private var showingPreview = false

    var body: some View {
        switch GFMBody.classify(
            src: media.src,
            asset: GFMBody.assetHref(media.src, vault: vault, baseURL: baseURL)
        ) {
        case .youtube:
            YouTubeView(src: media.src)
        case .maps:
            MapsView(src: media.src)
        case .pdf:
            pdfBody
        case .assetText:
            assetTextBody
        case .htmlAsset:
            htmlAssetBody
        case .ogp:
            ogpBody
        case .image:
            imageBody
        }
    }

    @ViewBuilder
    private var pdfBody: some View {
        if let target = targetURL(media.src) {
            PdfNoteView(assetURL: target)
        } else {
            fallbackLink
        }
    }

    @ViewBuilder
    private var assetTextBody: some View {
        if let asset = GFMBody.assetHref(media.src, vault: vault, baseURL: baseURL) {
            TextAssetView(url: asset)
        } else {
            fallbackLink
        }
    }

    @ViewBuilder
    private var htmlAssetBody: some View {
        if let asset = GFMBody.assetHref(media.src, vault: vault, baseURL: baseURL),
           let scheme = asset.scheme?.lowercased(), scheme == "http" || scheme == "https" {
            HTMLAssetView(url: asset)
                .frame(minHeight: 320, maxHeight: 720)
        } else {
            fallbackLink
        }
    }

    @ViewBuilder
    private var ogpBody: some View {
        if let client, let url = GFMBody.webHrefURL(GFMBody.webHref(media.src)) {
            OgpCardView(client: client, url: url)
        } else {
            fallbackLink
        }
    }

    @ViewBuilder
    private var imageBody: some View {
        if let asset = GFMBody.assetHref(media.src, vault: vault, baseURL: baseURL) {
            imageFrame(asset)
        } else if let url = GFMBody.webHrefURL(GFMBody.webHref(media.src)) {
            imageFrame(url)
        } else {
            fallbackLink
        }
    }

    @ViewBuilder
    private func imageFrame(_ url: URL) -> some View {
        Button { previewURL = url; showingPreview = true } label: {
            AsyncImage(url: url) { phase in
                if let image = phase.image {
                    image.resizable().scaledToFit().frame(maxWidth: .infinity)
                } else if phase.error != nil {
                    fallbackLink
                } else {
                    ProgressView().frame(maxWidth: .infinity, minHeight: 80)
                }
            }
            .frame(maxWidth: .infinity, minHeight: 80)
            .background(Color.primary.opacity(0.035))
            .clipShape(RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain)
        .sheet(isPresented: $showingPreview) {
            VStack {
                AsyncImage(url: previewURL) { phase in
                    if let image = phase.image { image.resizable().scaledToFit() }
                    else { ProgressView() }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .padding()
            }
            .background(Color.black)
        }
    }

    @ViewBuilder
    private var fallbackLink: some View {
        if let url = GFMBody.webHrefURL(GFMBody.webHref(media.src)) {
            Link(destination: url) {
                Text(media.alt.isEmpty ? media.src : media.alt)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } else {
            Text(media.alt.isEmpty ? media.src : media.alt)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    /// The URL a src resolves to for a local asset, or the webbed href.
    private func targetURL(_ src: String) -> URL? {
        GFMBody.assetHref(src, vault: vault, baseURL: baseURL)
            ?? GFMBody.webHrefURL(GFMBody.webHref(src))
    }
}

// MARK: - Image provider

/// Custom image provider for the GFM reader, mirroring the web reader's
/// `assetHref`: `assets/…` sources are rewritten to the local
/// `/api/asset?name=…&vault=…` endpoint, http(s) sources load directly, and
/// anything else (relative non-asset paths, `data:` URIs, unknown schemes)
/// falls back to a placeholder caption. MarkdownUI hands this provider a URL
/// already resolved against its (unset) image base URL, so a note-relative
/// `assets/foo.png` arrives with its path verbatim.
struct TrackAssetImageProvider: ImageProvider {
    let baseURL: URL
    let vault: String

    @ViewBuilder
    func makeImage(url: URL?) -> some View {
        if let url {
            if let resolved = Self.resolve(url: url, baseURL: baseURL, vault: vault) {
                AsyncImage(url: resolved) { phase in
                    if let image = phase.image {
                        image.resizable().scaledToFit()
                    } else if phase.error != nil {
                        placeholder(url.absoluteString)
                    } else {
                        ProgressView().frame(maxWidth: .infinity, minHeight: 60)
                    }
                }
                .frame(maxWidth: .infinity)
            } else {
                placeholder(url.absoluteString)
            }
        } else {
            placeholder("")
        }
    }

    @ViewBuilder
    private func placeholder(_ source: String) -> some View {
        Text("🖼 \(source)")
            .font(.caption)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// Route a resolved image URL:
    /// - http(s) URLs load as-is;
    /// - `assets/…` paths (optionally `./`- or `/`-prefixed) become the local
    ///   `/api/asset?name=…&vault=…` URL (`vault` omitted when empty);
    /// - anything else has no drawable source and yields nil.
    ///
    /// `url.path` is already percent-decoded by Foundation, so the `name`
    /// query value is encoded exactly once by `URLComponents`.
    private static func resolve(url: URL, baseURL: URL, vault: String) -> URL? {
        let scheme = url.scheme?.lowercased()
        if scheme == "http" || scheme == "https" {
            return url
        }
        let path = url.path
        var normalized = path
        if normalized.hasPrefix("/") {
            normalized = String(normalized.dropFirst())
        } else if normalized.hasPrefix("./") {
            normalized = String(normalized.dropFirst(2))
        }
        guard normalized.hasPrefix("assets/") else { return nil }
        let name = String(normalized.dropFirst("assets/".count))
        guard !name.isEmpty else { return nil }
        var components = URLComponents(url: baseURL.appendingPathComponent("api/asset"), resolvingAgainstBaseURL: false)
        var items = [URLQueryItem(name: "name", value: name)]
        if !vault.isEmpty { items.append(URLQueryItem(name: "vault", value: vault)) }
        components?.queryItems = items
        return components?.url
    }
}

private extension NSRange {
    var optional: NSRange? { location == NSNotFound ? nil : self }
}

/// Isolated HTML asset surface. It deliberately has no script-message bridge,
/// no persistent storage, and does not allow custom schemes or file URLs.
private struct HTMLAssetView: NSViewRepresentable {
    let url: URL

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        let view = WKWebView(frame: .zero, configuration: configuration)
        view.navigationDelegate = context.coordinator
        view.load(URLRequest(url: url))
        return view
    }

    func updateNSView(_ view: WKWebView, context: Context) {
        guard view.url != url else { return }
        view.load(URLRequest(url: url))
    }

    static func dismantleNSView(_ view: WKWebView, coordinator: Coordinator) {
        view.stopLoading()
        view.navigationDelegate = nil
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        @MainActor
        func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                     decisionHandler: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void) {
            guard let url = navigationAction.request.url,
                  let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else {
                decisionHandler(.cancel)
                return
            }
            decisionHandler(.allow)
        }
    }
}
