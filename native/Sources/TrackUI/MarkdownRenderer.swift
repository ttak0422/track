import Foundation
import MarkdownUI
import SwiftUI
import TrackAPI

// GFM Markdown rendering for the native reader, backed by MarkdownUI
// (gonzalezreal/MarkdownUI), whose parser enables the cmark-gfm table,
// tasklist, strikethrough and autolink extensions. Tables, task lists,
// strikethrough and bare-URL autolinks therefore render as themselves — the
// old `AttributedString(markdown:)` path and hand-rolled swift-markdown
// renderer could not draw them.
//
// Visual fidelity to docs/spec/design.md is intentionally NOT the goal here —
// only that a construct renders as itself. Fenced rich blocks the native app
// cannot draw (mermaid/dot/graphviz/d2/drawio/mindmap/map/echarts/viewspec/
// taskboard/track-view/track-query/dashboard), `$` math lines and `![[...]]`
// include lines are collapsed to a placeholder line, and `[[wikilink]]`
// targets are rewritten to `trackwiki://` standard links. Inline trackwiki
// links navigate through the `\.openURL` environment installed by the reader
// (NoteReaderView); a "Links" rail (web reader's WikiLink) wired to
// `onWikilink` stays as the primary navigation surface.
//
// DEFICIT: GFM footnote references `[^1]` are a cmark-gfm extension that
// MarkdownUI does not enable, so they remain literal source text.

public struct GFMBody: View {
    let markdown: String
    let baseURL: URL
    let vault: String
    let includes: [NoteInclude]?
    let client: TrackClient?
    var onWikilink: ((String) -> Void)?

    public init(
        markdown: String,
        baseURL: URL,
        vault: String,
        includes: [NoteInclude]? = nil,
        client: TrackClient? = nil,
        onWikilink: ((String) -> Void)? = nil
    ) {
        self.markdown = markdown
        self.baseURL = baseURL
        self.vault = vault
        self.includes = includes
        self.client = client
        self.onWikilink = onWikilink
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
                Text("Links").font(.caption).foregroundStyle(.secondary)
                ForEach(links, id: \.self) { target in
                    WikilinkRailRow(target: target, client: client) {
                        onWikilink?(target)
                    }
                }
            }
        }
    }

    /// The alternating MarkdownUI / FigureHost run, built once per render by
    /// `Self.segments` from the spliced + fenced source.
    private var segmentedBody: some View {
        let segments = Self.segments(markdown: markdown, includes: includes)
        return ForEach(Array(segments.enumerated()), id: \.offset) { _, segment in
            switch segment {
            case .markdown(let text):
                Markdown(text)
                    .markdownTheme(.gitHub)
                    .markdownImageProvider(TrackAssetImageProvider(baseURL: baseURL, vault: vault))
                    .textSelection(.enabled)
            case .figure(let figure):
                FigureSegmentView(figure: figure, vault: vault, client: client)
            }
        }
    }

    // MARK: - Segmenting (fence-aware)

    /// One run of the body: either a Markdown span (already wikilink-rewritten,
    /// with unrenderable rich fences collapsed to placeholders) or a figure the
    /// native app draws through FigureHost.
    enum Segment {
        case markdown(String)
        case figure(Figure)
    }

    /// A rich fence (or math line) lifted out of the prose, carrying exactly
    /// the source the shell renderer needs.
    struct Figure {
        enum Kind: Equatable {
            case mermaid
            case math(display: Bool)
            case echarts
            case viewspec
        }

        let kind: Kind
        let source: String
    }

    /// Fences rendered as real figures (FigureHost) — never collapsed.
    private static let figureFences: [String: Figure.Kind] = [
        "mermaid": .mermaid,
        "echarts": .echarts,
        "viewspec": .viewspec,
    ]

    /// Fences the native app cannot draw — still collapsed to a placeholder.
    private static let placeholderFences: Set<String> = [
        "dot", "graphviz", "d2", "drawio", "mindmap", "map",
        "taskboard", "track-view", "track-query", "dashboard",
    ]

    /// Split `markdown` into an ordered run of Markdown spans and figure
    /// segments. `includes` (resolved `![[...]]` directives, aligned to
    /// `markdown` lines) are expanded in place first. Fences that map to a
    /// figure are lifted into their own segment; unrenderable rich fences
    /// collapse to a placeholder line; everything else has its `[[wikilink]]`
    /// targets rewritten to `trackwiki://` links.
    static func segments(markdown: String, includes: [NoteInclude]?) -> [Segment] {
        let spliceIn = includes ?? []
        let source = spliceIn.isEmpty ? markdown : spliceIncludes(markdown, spliceIn)
        let lines = source.components(separatedBy: "\n")

        var segments: [Segment] = []
        var buf: [String] = []
        var i = 0
        let flush = {
            if !buf.isEmpty {
                segments.append(.markdown(buf.joined(separator: "\n")))
                buf = []
            }
        }

        while i < lines.count {
            let line = lines[i]
            let trimmed = line.trimmingCharacters(in: .whitespaces)

            // Math lines: `$$...$$` (display) and `$...$` (inline), each on a
            // line of its own.
            if !trimmed.hasPrefix("```"), trimmed.hasPrefix("$") {
                flush()
                if trimmed.hasPrefix("$$") {
                    segments.append(.figure(Figure(kind: .math(display: true), source: stripMathDelims(trimmed, "$$"))))
                } else {
                    segments.append(.figure(Figure(kind: .math(display: false), source: stripMathDelims(trimmed, "$"))))
                }
                i += 1
                continue
            }

            // Fence transitions.
            if trimmed.hasPrefix("```") {
                let lang = String(trimmed.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                let langName = lang.split(separator: " ", maxSplits: 1).first.map(String.init) ?? lang
                if langName.isEmpty {
                    // Unlabelled fence — no figure interpretation. Skip past it
                    // into the markdown span, leaving it untouched.
                    buf.append(line)
                    i += 1
                    var closed = false
                    while i < lines.count {
                        buf.append(lines[i])
                        if lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") { closed = true; i += 1; break }
                        i += 1
                    }
                    _ = closed
                    continue
                }
                if let kind = figureFences[langName] {
                    flush()
                    i += 1
                    var body: [String] = []
                    while i < lines.count && !lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                        body.append(lines[i])
                        i += 1
                    }
                    i += 1 // closing fence
                    segments.append(.figure(Figure(kind: kind, source: body.joined(separator: "\n"))))
                    continue
                }
                if placeholderFences.contains(langName) {
                    flush()
                    i += 1
                    while i < lines.count && !lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                        i += 1
                    }
                    i += 1 // closing fence
                    segments.append(.markdown("[diagram: \(langName) — preview not supported in native yet]"))
                    continue
                }
                // A labelled fence of another language (code): leave untouched.
                buf.append(line)
                i += 1
                while i < lines.count {
                    buf.append(lines[i])
                    if lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") { i += 1; break }
                    i += 1
                }
                continue
            }

            buf.append(self.styleAlert(self.rewriteWikilinks(line)))
            i += 1
        }
        flush()
        return segments
    }

    /// A GitHub-style callout `> [!NOTE]` (or TIP/IMPORTANT/WARNING/CAUTION)
    /// becomes a titled admonition: the marker is rewritten to a bold title so
    /// the blockquote reads as a labelled callout in the native renderer (web
    /// remarkAlert's visual intent, without a custom component). Ordinary
    /// blockquotes pass through untouched.
    private static let alertRegex = try! NSRegularExpression(
        pattern: "^(\\s*)>\\s*(?:\\[\\!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\\])\\s*",
        options: [.caseInsensitive]
    )

    private static func styleAlert(_ line: String) -> String {
        let ns = line as NSString
        let range = NSRange(location: 0, length: ns.length)
        guard let match = alertRegex.firstMatch(in: line, range: range),
              let typeRange = Range(match.range(at: 2), in: line),
              let leadRange = Range(match.range(at: 1), in: line) else {
            return line
        }
        let type = String(line[typeRange]).capitalized
        let lead = String(line[leadRange])
        let rest = ns.substring(from: match.range(at: 0).location + match.range(at: 0).length)
        return "\(lead)> **\(type):** \(rest)"
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

    /// The markdown an include directive becomes: an error notice, or a bold
    /// caption + the excerpt blockquoted.
    private static func includeBlock(_ inc: NoteInclude) -> String {
        if let error = inc.error, !error.isEmpty {
            return "> ⚠ \(error)"
        }
        var out: [String] = []
        let caption = inc.caption.isEmpty ? "Include" : inc.caption
        out.append("**\(caption)**")
        for line in inc.lines {
            out.append("> \(line)")
        }
        for bad in inc.badOptions ?? [] {
            out.append("> ⚠ unknown option: \(bad)")
        }
        return out.joined(separator: "\n")
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
                let encoded = target.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? target
                result += "[\(display)](trackwiki://\(encoded))"
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

/// One row of the wikilink rail. Resolves its target through `/api/resolve`
/// on first appearance; an unresolved target renders greyed out and disabled
/// (web WikiLink's "unresolved"), a resolved one stays a tappable link.
private struct WikilinkRailRow: View {
    let target: String
    let client: TrackClient?
    let onOpen: () -> Void
    @State private var resolved = false
    @State private var isPending = true

    var body: some View {
        if isPending {
            Text(target).font(.body).foregroundStyle(.tertiary)
                .task(id: target) { await resolve() }
        } else if resolved {
            Button(target) { onOpen() }
                .buttonStyle(.link)
        } else {
            Text(target).font(.body).foregroundStyle(.tertiary)
        }
    }

    private func resolve() async {
        guard let client else { isPending = false; resolved = true; return }
        let (vault, term) = Self.split(target)
        let found = (try? await client.resolveTerm(term, vault: vault))?.found ?? false
        resolved = found
        isPending = false
    }

    /// `vault:title#anchor` → (`vault`, `title`); a bare `title` keeps the
    /// empty vault (mirrors NoteReaderModel.splitWikilink).
    private static func split(_ target: String) -> (vault: String, term: String) {
        let trimmed = target.trimmingCharacters(in: .whitespaces)
        let noAnchor = trimmed.split(separator: "#", maxSplits: 1).first.map(String.init) ?? trimmed
        if let colon = noAnchor.firstIndex(of: ":") {
            return (String(noAnchor[..<colon]), String(noAnchor[noAnchor.index(after: colon)...]))
        }
        return ("", noAnchor)
    }
}

// MARK: - Figure segments

/// A single lifted figure segment rendered through FigureHost: mermaid, KaTeX
/// math, and an echarts option (from a ```echarts fence or a ```viewspec fence
/// the server resolved via `/api/viewspec`). `viewspec` is resolved lazily on
/// first appearance; until then a placeholder stands in, and a resolution
/// failure keeps the spec's source as the placeholder text.
private struct FigureSegmentView: View {
    let figure: GFMBody.Figure
    let vault: String
    let client: TrackClient?
    @State private var height: CGFloat
    @State private var resolvedOption: String?
    @Environment(\.colorScheme) private var colorScheme

    init(figure: GFMBody.Figure, vault: String, client: TrackClient?) {
        self.figure = figure
        self.vault = vault
        self.client = client
        switch figure.kind {
        case .echarts, .viewspec:
            _height = State(initialValue: FigureAssets.defaultEchartsHeight)
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
            theme: colorScheme == .dark ? .dark : .light
        )
        .frame(height: height)
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