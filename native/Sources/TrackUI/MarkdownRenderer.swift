import Foundation
import MarkdownUI
import SwiftUI

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
    var onWikilink: ((String) -> Void)?

    public init(markdown: String, baseURL: URL, vault: String, onWikilink: ((String) -> Void)? = nil) {
        self.markdown = markdown
        self.baseURL = baseURL
        self.vault = vault
        self.onWikilink = onWikilink
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Markdown(Self.preprocess(markdown))
                .markdownTheme(.gitHub)
                .markdownImageProvider(TrackAssetImageProvider(baseURL: baseURL, vault: vault))
                .textSelection(.enabled)

            // The wikilink rail (web reader's WikiLink): distinct targets are
            // collected from the *original* source and shown as tappable links
            // resolved via `/api/resolve` through `onWikilink`. Inline
            // `[[...]]` render as `trackwiki://` links that the reader's
            // `\.openURL` interception routes to the same `onWikilink`.
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

    // MARK: - Preprocessing (fence-aware)

    /// Languages whose fenced blocks the native renderer does not draw.
    private static let richFences: Set<String> = [
        "mermaid", "dot", "graphviz", "d2", "drawio", "mindmap", "map",
        "echarts", "viewspec", "taskboard", "track-view", "track-query", "dashboard",
    ]

    /// Rewrite `markdown` before parsing so that:
    /// - rich fenced blocks (diagram/math), `$`/`$$` math lines and `![[...]]`
    ///   include lines collapse to a single placeholder line, and
    /// - `[[target|display]]` / `[[target]]` wikilinks become standard links
    ///   `[display](trackwiki://…)` / `[target](trackwiki://…)`.
    ///
    /// Line-splitting with explicit fence tracking so nothing inside any ```…
    /// ``` block is rewritten.
    static func preprocess(_ markdown: String) -> String {
        let lines = markdown.components(separatedBy: "\n")
        var out: [String] = []
        var i = 0
        var inFence = false
        while i < lines.count {
            let line = lines[i]
            let trimmed = line.trimmingCharacters(in: .whitespaces)

            // Fence transitions.
            if trimmed.hasPrefix("```") {
                let lang = String(trimmed.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                if inFence {
                    inFence = false
                    out.append(line)
                    i += 1
                    continue
                } else {
                    let langName = lang.split(separator: " ", maxSplits: 1).first.map(String.init) ?? lang
                    if richFences.contains(langName) {
                        // Collapse the whole rich block to a placeholder.
                        i += 1
                        while i < lines.count && !lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                            i += 1
                        }
                        i += 1 // closing fence
                        out.append("[diagram: \(langName) — preview not supported in native yet]")
                        continue
                    }
                    inFence = true
                    out.append(line)
                    i += 1
                    continue
                }
            }

            // Inside a (non-rich) fence: leave untouched.
            if inFence {
                out.append(line)
                i += 1
                continue
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

            // Wikilinks → standard links (outside fences).
            out.append(rewriteWikilinks(line))
            i += 1
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