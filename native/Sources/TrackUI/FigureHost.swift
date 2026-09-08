import Foundation
import SwiftUI
import WebKit

// FigureHost — the native reader's figure island.
//
// SwiftUI cannot draw the rich fences of a note (mermaid diagrams, KaTeX
// math, echarts options, or raw SVG/HTML), so each such figure is rendered by
// a small dedicated WKWebView instead of a whole-page WebView — the "island"
// approach the web reader uses for the same content (docs/spec/web.md).
//
// - One WKWebView per FigureHost view, created lazily on first appearance
//   (makeNSView) and reused for every later kind/theme change: content is
//   re-dispatched into the same shell, never a page reload.
// - Non-persistent website data store: the island is a throwaway surface and
//   must not leak cookies/localStorage between islands or app launches.
// - The only JS→native bridge is the "fig" script message handler, which
//   reports the rendered height (SwiftUI sizes the island through the
//   `height` binding) and outbound links (`onLink`). No native capability is
//   exposed to the figure — the island cannot reach the app.
// - Link taps are intercepted in the coordinator's `decidePolicyFor` (the JS
//   shell also cancels anchor clicks and posts them to the same callback).
// - mermaid / KaTeX / echarts scripts load from CDN by default, pinned to the
//   versions web/package.json resolves today (mermaid 11.16.1, katex 0.16.47,
//   echarts 6.1.0 — see FigureAssets). A `localScriptURL` override swaps the
//   CDN root for a local directory so the same shell works fully offline.
//   The scripts are NOT vendored into the package: mermaid.min.js alone is
//   ~3.5 MB, and making them Bundle resources would require a Package.swift
//   change, so the shell stays a CDN default plus a local override hook.
//
// The island draws content only through `render(...)`; the shell HTML and
// dispatch JS are embedded below so the package needs no resource bundle.

// MARK: - Figure kind

/// The rich content a figure island can draw. Each case carries exactly the
/// source the web reader feeds the same renderer, so a fence block from a
/// note maps 1:1 onto a case.
public enum FigureKind: Sendable, Equatable {
    /// ` ```mermaid …``` ` diagram source.
    case mermaid(String)
    /// TeX source; `display` selects `$$…$$` display mode vs inline `$…$`.
    case math(String, display: Bool)
    /// An echarts option object as JSON text (set via `setOption`).
    case echarts(optionJSON: String)
    /// Raw SVG markup, injected as-is.
    case svg(String)
    /// Raw HTML fragment, injected as-is.
    case html(String)
    /// Graphviz DOT source (` ```dot ``` ` or ` ```graphviz ``` `).
    case dot(String)
    /// D2 source (` ```d2 ``` `).
    case d2(String)
    /// draw.io XML (`<mxfile>` or `<mxGraphModel>`).
    case drawio(String)
    /// A Leaflet map figure (` ```map ``` `).
    case map(MapFigure)

    /// The name the shell JS dispatches on.
    var kindName: String {
        switch self {
        case .mermaid: return "mermaid"
        case .math: return "math"
        case .echarts: return "echarts"
        case .svg: return "svg"
        case .html: return "html"
        case .dot: return "dot"
        case .d2: return "d2"
        case .drawio: return "drawio"
        case .map: return "map"
        }
    }

    /// The payload passed to the renderer.
    var source: String {
        switch self {
        case .mermaid(let source), .echarts(let source), .svg(let source), .html(let source),
             .dot(let source), .d2(let source), .drawio(let source):
            return source
        case .math(let source, _):
            return source
        case .map(let figure):
            return figure.json
        }
    }

    /// Whether math renders in display mode; nil for non-math kinds.
    var displayMode: Bool? {
        if case .math(_, let display) = self { return display }
        return nil
    }
}

// MARK: - Map figure

/// A tile-layer flavor for a ```map figure, mirroring web MapFence's MapType.
public enum MapTileType: String, Sendable, Equatable {
    case roadmap
    case satellite
    case hybrid
    case terrain
}

/// One pin on a ```map figure. `target` is the `[[wikilink]]` target (the part
/// before `|`), `display` the resolved label, and `description` the free text
/// after the wikilink.
public struct MapMarker: Sendable, Equatable {
    public let lat: Double
    public let long: Double
    public let target: String
    public let display: String
    public let description: String

    public init(lat: Double, long: Double, target: String, display: String, description: String) {
        self.lat = lat
        self.long = long
        self.target = target
        self.display = display
        self.description = description
    }
}

/// The parsed content of a ```map fence: a center, a zoom, a tile flavor, and
/// any number of markers (web MapFence's MapFenceProps).
public struct MapFigure: Sendable, Equatable {
    public let lat: Double
    public let long: Double
    public let zoom: Int
    public let type: MapTileType
    public let markers: [MapMarker]

    public init(lat: Double, long: Double, zoom: Int, type: MapTileType, markers: [MapMarker]) {
        self.lat = lat
        self.long = long
        self.zoom = zoom
        self.type = type
        self.markers = markers
    }

    /// The shell's JSON payload (parsed by `JSON.parse(cfg.source)`).
    var json: String {
        let markers = markers.map { m -> [String: Any] in
            ["lat": m.lat, "long": m.long, "target": m.target, "display": m.display, "description": m.description]
        }
        let payload: [String: Any] = [
            "lat": lat, "long": long, "zoom": zoom, "type": type.rawValue, "markers": markers,
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: payload, options: []),
              let text = String(data: data, encoding: .utf8) else {
            return "{}"
        }
        return text
    }

    /// A `lat|long|zoom|type` field line, or a `marker` line.
    private static let lineRegex = try! NSRegularExpression(pattern: "^(lat|long|zoom|type|marker):\\s*(.*?)\\s*$")

    /// `kind,lat,long,[[target|display]], description` — the marker line's tail.
    private static let markerRegex = try! NSRegularExpression(pattern: "^([^,]+),\\s*([^,]+),\\s*([^,]+),\\s*\\[\\[([^\\]]+)\\]\\]\\s*,\\s*(.*)$")

    /// Parse a ```map fence body. Mirrors web MapFence.parseMapFence: `lat` and
    /// `long` are required, `zoom` (integer 0...19) and `type` are optional, and
    /// any number of `marker:` lines follow. Nil for anything malformed — the
    /// renderer then keeps the fence as a code block.
    public static func parse(_ body: String) -> MapFigure? {
        var values: [String: String] = [:]
        var markers: [MapMarker] = []
        for raw in body.components(separatedBy: "\n") {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.isEmpty { continue }
            let ns = line as NSString
            let range = NSRange(location: 0, length: ns.length)
            guard let match = lineRegex.firstMatch(in: line, range: range) else { return nil }
            let key = ns.substring(with: match.range(at: 1))
            let value = ns.substring(with: match.range(at: 2))
            if key == "marker" {
                guard let marker = parseMarker(value) else { return nil }
                markers.append(marker)
            } else {
                guard !values.keys.contains(key), !value.isEmpty else { return nil }
                values[key] = value
            }
        }
        guard let lat = coordinate(values["lat"], limit: 90),
              let long = coordinate(values["long"], limit: 180) else { return nil }
        let zoom = Int(values["zoom"] ?? "10") ?? -1
        guard zoom >= 0, zoom <= 19 else { return nil }
        guard let type = MapTileType(rawValue: values["type"] ?? "roadmap") else { return nil }
        return MapFigure(lat: lat, long: long, zoom: zoom, type: type, markers: markers)
    }

    private static func parseMarker(_ value: String) -> MapMarker? {
        let ns = value as NSString
        let range = NSRange(location: 0, length: ns.length)
        guard let match = markerRegex.firstMatch(in: value, range: range) else { return nil }
        let kind = ns.substring(with: match.range(at: 1)).trimmingCharacters(in: .whitespaces)
        let latText = ns.substring(with: match.range(at: 2))
        let longText = ns.substring(with: match.range(at: 3))
        let link = ns.substring(with: match.range(at: 4))
        let description = ns.substring(with: match.range(at: 5))
        guard !kind.isEmpty,
              let lat = coordinate(latText, limit: 90),
              let long = coordinate(longText, limit: 180) else { return nil }
        let parts = link.split(separator: "|", maxSplits: 1)
        let target = parts[0].trimmingCharacters(in: .whitespaces)
        guard !target.isEmpty else { return nil }
        let display = parts.count > 1
            ? parts[1].trimmingCharacters(in: .whitespaces)
            : target
        return MapMarker(lat: lat, long: long, target: target, display: display.isEmpty ? target : display, description: description)
    }

    private static func coordinate(_ value: String?, limit: Double) -> Double? {
        guard let value, !value.trimmingCharacters(in: .whitespaces).isEmpty else { return nil }
        guard let number = Double(value), number.isFinite, abs(number) <= limit else { return nil }
        return number
    }
}

// MARK: - Figure theme

/// The only appearance the island takes from SwiftUI: a background and a
/// foreground color, as CSS strings. Light/dark palettes for the JS engines
/// (mermaid's and echarts' built-in dark themes) are derived from the
/// background's luminance, so no third knob is needed.
public struct FigureTheme: Sendable, Equatable {
    public let background: String
    public let foreground: String

    public init(background: String, foreground: String) {
        self.background = background
        self.foreground = foreground
    }

    /// design.md Light column (bg / text tokens).
    public static let light = FigureTheme(background: "#fbfaf8", foreground: "#1a1a18")

    /// design.md Dark column (bg / text tokens).
    public static let dark = FigureTheme(background: "#141618", foreground: "#e9e9e4")

    /// True when `background` reads as a dark color (WCAG relative luminance
    /// below 0.5). Drives which built-in mermaid/echarts theme the island
    /// selects; both engines ship a fixed dark palette that no bg/fg CSS can
    /// reproduce. Non-hex or unparseable strings count as light so the island
    /// never flips to a dark engine theme by accident.
    public var isDark: Bool {
        Self.luminance(of: background) < 0.5
    }

    /// WCAG relative luminance of a `#rgb` / `#rrggbb` hex color string.
    static func luminance(of cssColor: String) -> Double {
        let hex = cssColor.trimmingCharacters(in: CharacterSet(charactersIn: "# \t\n"))
        let chars = Array(hex)
        let wide = chars.count == 6
        guard (wide || chars.count == 3), chars.allSatisfy({ $0.isHexDigit }) else { return 1 }

        func channel(_ index: Int) -> Double {
            let start = index * (wide ? 2 : 1)
            let slice = wide
                ? String(chars[start]) + String(chars[start + 1])
                : String(chars[start]) + String(chars[start])
            guard let value = Int(slice, radix: 16) else { return 1 }
            return Double(value) / 255.0
        }

        func linear(_ component: Double) -> Double {
            component <= 0.03928 ? component / 12.92 : pow((component + 0.055) / 1.055, 2.4)
        }

        return 0.2126 * linear(channel(0)) + 0.7152 * linear(channel(1)) + 0.0722 * linear(channel(2))
    }
}

// MARK: - Figure assets

/// The script/CSS URLs the island loads, pinned to the versions
/// web/package.json resolves (mermaid is an exact pin; katex `^0.16.47` and
/// echarts `^6.1.0` both resolve to these versions in this repo's lockfile).
public enum FigureAssets {
    public static let mermaidVersion = "11.16.1"
    public static let katexVersion = "0.16.47"
    public static let echartsVersion = "6.1.0"
    public static let graphvizVersion = "1.24.1"
    public static let d2Version = "0.1.33"
    public static let leafletVersion = "1.9.4"

    /// Default CDN root for the pinned packages.
    public static let cdnRoot = URL(string: "https://cdn.jsdelivr.net/npm")!

    /// The draw.io static viewer (diagrams.net's own renderer). Defaults to the
    /// live CDN build; a local override swaps it for a vendored copy
    /// (drawio-viewer-static.min.js, web/public).
    public static let drawioViewerURL = URL(string: "https://viewer.diagrams.net/js/viewer-static.min.js")!

    /// Default height (points) reserved for an echarts figure, which needs an
    /// explicit size before `init`; callers can reserve the same space while
    /// the island measures itself.
    public static let defaultEchartsHeight: CGFloat = 400

    /// Default height (points) reserved for a map figure (Leaflet needs an
    /// explicit box before `L.map`).
    public static let defaultMapHeight: CGFloat = 360

    /// Asset URLs by the name the shell JS knows ("mermaid", "katex",
    /// "katexCSS", "echarts", "graphviz", "d2", "leaflet", "leafletCSS",
    /// "drawio").
    ///
    /// With no override these are the pinned jsdelivr URLs (plus the diagrams.net
    /// viewer). With a local root they become `<root>/<basename>` so the same
    /// shell works fully offline from a directory of the pinned files; the two
    /// wasm engines expect a pre-bundled ESM file each (`graphviz.esm.js`,
    /// `d2.esm.js`).
    static func resolved(localScriptURL: URL?) -> [String: String] {
        let remotePaths = [
            "mermaid": "mermaid@\(mermaidVersion)/dist/mermaid.min.js",
            "katex": "katex@\(katexVersion)/dist/katex.min.js",
            "katexCSS": "katex@\(katexVersion)/dist/katex.min.css",
            "echarts": "echarts@\(echartsVersion)/dist/echarts.min.js",
            "graphviz": "@hpcc-js/wasm-graphviz@\(graphvizVersion)/+esm",
            "d2": "@terrastruct/d2@\(d2Version)/+esm",
            "leaflet": "leaflet@\(leafletVersion)/dist/leaflet.js",
            "leafletCSS": "leaflet@\(leafletVersion)/dist/leaflet.css",
        ]
        let remote: [String: String] = remotePaths.mapValues { cdnRoot.appending(path: $0).absoluteString }
        var withDrawio = remote
        withDrawio["drawio"] = drawioViewerURL.absoluteString
        guard let local = localScriptURL else { return withDrawio }
        let localBasenames = [
            "mermaid": "mermaid.min.js",
            "katex": "katex.min.js",
            "katexCSS": "katex.min.css",
            "echarts": "echarts.min.js",
            "graphviz": "graphviz.esm.js",
            "d2": "d2.esm.js",
            "leaflet": "leaflet.js",
            "leafletCSS": "leaflet.css",
            "drawio": "drawio-viewer-static.min.js",
        ]
        return localBasenames.mapValues { local.appending(path: $0).absoluteString }
    }
}

// MARK: - Mindmap outline

/// Converts a ```mindmap fence body into mermaid mindmap source. The web reader
/// draws mindmaps with a tiny built-in SVG renderer (web/src/components/markdown/
/// mindmap.ts); the native reader reuses the existing mermaid engine instead, so
/// the outline is folded into a tree and re-emitted as indented mermaid
/// `mindmap` syntax.
public enum MindmapOutline {
    /// The mermaid source for an indented outline: `#`..`######` headings set
    /// the hierarchy (depth = level) and `-`/`*`/`+` list items under a heading
    /// become leaves (depth = heading + 1 + extra indent). Labels are stripped of
    /// `[[wiki]]` and `[text](url)` link syntax. Empty for an empty fence.
    public static func mermaidSource(_ body: String) -> String {
        let items = parse(body)
        guard !items.isEmpty else { return "" }
        let root = tree(items)

        var lines = ["mindmap"]
        func emit(_ node: Node, depth: Int) {
            let indent = String(repeating: " ", count: 2 + depth * 2)
            lines.append(indent + node.label)
            for child in node.children {
                emit(child, depth: depth + 1)
            }
        }
        emit(root, depth: 0)
        return lines.joined(separator: "\n")
    }

    private struct Item {
        let depth: Int
        let label: String
    }

    private final class Node {
        let label: String
        var children: [Node] = []
        init(label: String) { self.label = label }
    }

    private static func parse(_ body: String) -> [Item] {
        var items: [Item] = []
        var headingDepth = 0
        for raw in body.components(separatedBy: "\n") {
            if let heading = headingRegex.firstMatch(in: raw, options: [], range: NSRange(raw.startIndex..., in: raw)),
               let textRange = Range(heading.range(at: 2), in: raw) {
                headingDepth = heading.range(at: 1).length * 10
                items.append(Item(depth: headingDepth, label: labelText(String(raw[textRange]))))
                continue
            }
            if let list = listRegex.firstMatch(in: raw, options: [], range: NSRange(raw.startIndex..., in: raw)),
               let indentRange = Range(list.range(at: 1), in: raw),
               let textRange = Range(list.range(at: 2), in: raw) {
                guard headingDepth != 0 else { continue }
                let indent = String(raw[indentRange]).reduce(into: 0) { $0 += $1 == "\t" ? 2 : 1 }
                items.append(Item(depth: headingDepth + 1 + indent, label: labelText(String(raw[textRange]))))
            }
        }
        return items
    }

    /// `[[target|display]]` → `display` (or `target`), `[text](url)` → `text`,
    /// anything else unchanged (web mindmap.ts parseLabel).
    private static func labelText(_ source: String) -> String {
        if source.hasPrefix("[[") && source.hasSuffix("]]") {
            let inner = String(source.dropFirst(2).dropLast(2))
            let parts = inner.split(separator: "|", maxSplits: 1)
            return (parts.count > 1 ? String(parts[1]) : String(parts[0])).trimmingCharacters(in: .whitespaces)
        }
        if source.hasPrefix("["), let close = source.firstIndex(of: "]"),
           close < source.index(before: source.endIndex),
           source[source.index(after: close)] == "(",
           source.hasSuffix(")") {
            return String(source[source.index(after: source.startIndex)..<close])
        }
        return source
    }

    /// Folds a depth-annotated item sequence into a tree. When several items
    /// share the minimum depth there is no single root, so an implicit root is
    /// added (web mindmap.ts treeFromItems); mermaid needs a labeled root, so it
    /// renders as a single dot.
    private static func tree(_ items: [Item]) -> Node {
        let minDepth = items.map(\.depth).min() ?? 0
        let single = items[0].depth == minDepth && items.filter { $0.depth == minDepth }.count == 1
        let root = single ? Node(label: items[0].label) : Node(label: "•")
        let rest = single ? Array(items.dropFirst()) : items

        var stack: [(depth: Int, node: Node)] = [(minDepth - 1, root)]
        for item in rest {
            while stack.count > 1 && stack[stack.count - 1].depth >= item.depth {
                stack.removeLast()
            }
            let node = Node(label: item.label)
            stack[stack.count - 1].node.children.append(node)
            stack.append((item.depth, node))
        }
        return root
    }

    private static let headingRegex = try! NSRegularExpression(pattern: "^(#{1,6})\\s+(.+?)\\s*#*\\s*$")
    private static let listRegex = try! NSRegularExpression(pattern: "^(\\s*)[-*+]\\s+(.+?)\\s*$")
}

// MARK: - Figure host

/// A single lazy WKWebView island that draws one `FigureKind` and reports its
/// height back through the `height` binding. Reused across content changes:
/// every update re-dispatches into the same loaded shell rather than
/// recreating the webview or the page.
public struct FigureHost: NSViewRepresentable {
    public let kind: FigureKind
    @Binding public var height: CGFloat
    public var theme: FigureTheme
    /// Called with the absolute URL of a tapped link inside the figure.
    public var onLink: ((URL) -> Void)?
    /// When set, scripts/CSS load from this directory instead of the CDN
    /// (see FigureAssets.resolved for the expected layout).
    public var localScriptURL: URL?
    /// Height reserved for echarts figures that carry no intrinsic size.
    public var echartsHeight: CGFloat
    /// Height reserved for map figures (Leaflet needs an explicit box).
    public var mapHeight: CGFloat

    public init(
        kind: FigureKind,
        height: Binding<CGFloat>,
        theme: FigureTheme,
        onLink: ((URL) -> Void)? = nil,
        localScriptURL: URL? = nil,
        echartsHeight: CGFloat = FigureAssets.defaultEchartsHeight,
        mapHeight: CGFloat = FigureAssets.defaultMapHeight
    ) {
        self.kind = kind
        self._height = height
        self.theme = theme
        self.onLink = onLink
        self.localScriptURL = localScriptURL
        self.echartsHeight = echartsHeight
        self.mapHeight = mapHeight
    }

    // MARK: NSViewRepresentable

    @MainActor
    public func makeCoordinator() -> Coordinator {
        Coordinator(parent: self)
    }

    @MainActor
    public func makeNSView(context: Context) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        // Islands are throwaway surfaces: no cookies, no localStorage.
        configuration.websiteDataStore = .nonPersistent()
        configuration.userContentController.add(context.coordinator, name: "fig")

        let webView = WKWebView(frame: .zero, configuration: configuration)
        webView.navigationDelegate = context.coordinator
        context.coordinator.webView = webView
        webView.loadHTMLString(FigureAssets.shellHTML, baseURL: nil)
        context.coordinator.requestRender()
        return webView
    }

    @MainActor
    public func updateNSView(_ webView: WKWebView, context: Context) {
        context.coordinator.parent = self
        context.coordinator.requestRender()
    }

    @MainActor
    public static func dismantleNSView(_ nsView: WKWebView, coordinator: Coordinator) {
        coordinator.webView = nil
        nsView.stopLoading()
        nsView.navigationDelegate = nil
        nsView.configuration.userContentController.removeScriptMessageHandler(forName: "fig")
    }

    // MARK: Coordinator

    /// Owns the webview's native side: the "fig" message channel, navigation
    /// policy (link interception), and the render dispatch. Lives on the main
    /// actor like the webview it drives.
    @MainActor
    public final class Coordinator: NSObject, WKNavigationDelegate, WKScriptMessageHandler {
        var parent: FigureHost
        var webView: WKWebView?
        /// True once the shell finished its first load and JS is reachable.
        private var isReady = false
        /// The last render config sent (or queued); guards against redundant
        /// re-renders when the SwiftUI view re-updates (e.g. the height
        /// binding itself changing).
        private var lastJSON: String?

        init(parent: FigureHost) {
            self.parent = parent
        }

        /// Encode the host's current figure into the shell's render config
        /// and dispatch it, unless it is exactly what was already dispatched.
        func requestRender() {
            let json = Self.renderConfig(for: parent)
            guard json != lastJSON else { return }
            lastJSON = json
            guard let webView, isReady else { return }
            webView.evaluateJavaScript("window.trackFigure.render(\(json))", completionHandler: nil)
        }

        // MARK: WKNavigationDelegate

        /// Intercept taps on links inside the figure and hand the resolved
        /// URL to `onLink` instead of navigating the island. Everything else
        /// (the shell's own initial load) is allowed.
        public func webView(
            _ webView: WKWebView,
            decidePolicyFor navigationAction: WKNavigationAction,
            decisionHandler: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void
        ) {
            if navigationAction.navigationType == .linkActivated, let url = navigationAction.request.url {
                parent.onLink?(url)
                decisionHandler(.cancel)
            } else {
                decisionHandler(.allow)
            }
        }

        /// The shell is reachable from here on; flush any render queued while
        /// the page was still loading.
        public func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            isReady = true
            requestRender()
        }

        // MARK: WKScriptMessageHandler

        /// The one bridge into native. The shell posts exactly two shapes:
        /// `{type:"height", height:N}` to resize the island and
        /// `{type:"link", url:"…"}` (also covered by `decidePolicyFor`) for
        /// tapped links.
        public func userContentController(
            _ userContentController: WKUserContentController,
            didReceive message: WKScriptMessage
        ) {
            guard message.name == "fig", let body = message.body as? [String: Any] else { return }
            switch body["type"] as? String {
            case "height":
                if let number = body["height"] as? NSNumber {
                    let height = max(0, CGFloat(truncating: number))
                    if abs(height - parent.height) > 0.5 {
                        parent.height = height
                    }
                }
            case "link":
                if let text = body["url"] as? String, let url = URL(string: text) {
                    parent.onLink?(url)
                }
            default:
                break
            }
        }

        // MARK: Config encoding

        /// Stable (key-sorted) JSON for the shell's `render` call. Key order
        /// is sorted so equal configs produce equal strings, letting
        /// `requestRender` skip no-op dispatches.
        private static func renderConfig(for host: FigureHost) -> String {
            let theme: [String: Any] = [
                "bg": host.theme.background,
                "fg": host.theme.foreground,
                "dark": host.theme.isDark,
            ]
            let payload: [String: Any] = [
                "kind": host.kind.kindName,
                "source": host.kind.source,
                "display": host.kind.displayMode ?? false,
                "height": Double(host.echartsHeight),
                "mapHeight": Double(host.mapHeight),
                "theme": theme,
                "assets": FigureAssets.resolved(localScriptURL: host.localScriptURL),
            ]
            guard
                let data = try? JSONSerialization.data(withJSONObject: payload, options: [.sortedKeys]),
                let json = String(data: data, encoding: .utf8)
            else {
                return "{\"kind\":\"html\",\"source\":\"FigureHost: could not encode figure\",\"display\":false,\"theme\":{\"bg\":\"#ffffff\",\"fg\":\"#000000\",\"dark\":false}}"
            }
            return json
        }
    }
}

// MARK: - Shell

extension FigureAssets {
    /// The island's single document. It owns no rendering state itself: the
    /// native side pushes figures through `window.trackFigure.render`, and the
    /// only two messages it may send back are height and link notifications.
    ///
    /// Libraries are injected lazily, deduplicated, and only when a kind needs
    /// them (mermaid and echarts each weigh megabytes; KaTeX adds its own CSS).
    static let shellHTML = """
    <!doctype html>
    <html>
    <head>
    <meta charset="utf-8">
    <style>
      :root { color-scheme: light dark; }
      html, body { margin: 0; padding: 0; }
      body { overflow: hidden; }
      #figure { width: 100%; box-sizing: border-box; }
      .raw-svg svg { display: block; width: 100%; height: auto; }
      .katex-display { margin: 0.5em 0; }
      .katex { font-size: 1.06em; }
      .map-fence-marker { box-sizing: border-box; border: 2px solid #fff; border-radius: 50%; background: #e2483d; }
    </style>
    </head>
    <body>
    <div id="figure"></div>
    <script>
    (function () {
      "use strict";

      var figure = document.getElementById("figure");
      var assetCache = {};
      var currentChart = null;
      var renderSalt = 0;

      function post(msg) {
        window.webkit.messageHandlers.fig.postMessage(msg);
      }

      function postHeight() {
        post({ type: "height", height: Math.ceil(figure.getBoundingClientRect().height) });
      }

      function errorText(err) {
        return err && err.message ? err.message : String(err);
      }

      function loadCSS(href) {
        return new Promise(function (resolve, reject) {
          var link = document.createElement("link");
          link.rel = "stylesheet";
          link.href = href;
          link.onload = resolve;
          link.onerror = function () { reject(new Error("could not load " + href)); };
          document.head.appendChild(link);
        });
      }

      function loadScript(src) {
        return new Promise(function (resolve, reject) {
          var el = document.createElement("script");
          el.src = src;
          el.onload = resolve;
          el.onerror = function () { reject(new Error("could not load " + src)); };
          document.head.appendChild(el);
        });
      }

      function loadModule(url) {
        return import(url);
      }

      function loadAssets(items) {
        return Promise.all(items.map(function (item) {
          if (assetCache[item]) { return assetCache[item]; }
          var promise = item.slice(-4) === ".css" ? loadCSS(item) : loadScript(item);
          assetCache[item] = promise;
          promise.catch(function () { delete assetCache[item]; });
          return promise;
        }));
      }

      function withTransparentBackground(dot) {
        var brace = dot.indexOf("{");
        if (brace < 0) { return dot; }
        return dot.slice(0, brace + 1) + ' bgcolor="transparent"; ' + dot.slice(brace + 1);
      }

      function tileLayers(type) {
        var osm = "© OpenStreetMap contributors";
        var esri = "Tiles © Esri";
        if (type === "roadmap") {
          return { base: window.L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", { attribution: osm }) };
        }
        var imagery = window.L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}", { attribution: esri + ", © OpenStreetMap contributors" });
        if (type === "hybrid") {
          return {
            base: imagery,
            overlay: window.L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/Reference/World_Boundaries_and_Places/MapServer/tile/{z}/{y}/{x}", { attribution: esri })
          };
        }
        if (type === "satellite") { return { base: imagery }; }
        return { base: window.L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/World_Topo_Map/MapServer/tile/{z}/{y}/{x}", { attribution: esri }) };
      }

      function render(cfg) {
        var svgKinds = { svg: 1, dot: 1, d2: 1 };
        figure.className = svgKinds[cfg.kind] ? "raw-svg" : "";
        figure.textContent = "";
        document.body.style.backgroundColor = cfg.theme.bg;
        document.body.style.color = cfg.theme.fg;

        if (cfg.kind === "svg" || cfg.kind === "html") {
          figure.innerHTML = cfg.source;
          postHeight();
          return;
        }

        if (cfg.kind === "math") {
          loadAssets([cfg.assets.katexCSS, cfg.assets.katex]).then(function () {
            var node = document.createElement("span");
            figure.appendChild(node);
            try {
              window.katex.render(cfg.source, node, {
                displayMode: !!cfg.display,
                throwOnError: false,
                output: "html"
              });
            } catch (err) {
              node.textContent = "KaTeX error: " + err.message;
            }
            postHeight();
          }).catch(function (err) { figure.textContent = err.message; postHeight(); });
          return;
        }

        if (cfg.kind === "mermaid") {
          loadAssets([cfg.assets.mermaid]).then(function () {
            var node = document.createElement("div");
            figure.appendChild(node);
            node.textContent = cfg.source;
            window.mermaid.initialize({
              startOnLoad: false,
              theme: cfg.theme.dark ? "dark" : "default",
              themeVariables: { background: cfg.theme.bg }
            });
            window.mermaid.run({ nodes: [node] })
              .then(postHeight)
              .catch(function (err) { node.textContent = "Mermaid error: " + err.message; postHeight(); });
          }).catch(function (err) { figure.textContent = err.message; postHeight(); });
          return;
        }

        if (cfg.kind === "echarts") {
          loadAssets([cfg.assets.echarts]).then(function () {
            var node = document.createElement("div");
            node.style.width = "100%";
            node.style.height = cfg.height + "px";
            figure.appendChild(node);
            try {
              if (currentChart) { currentChart.dispose(); currentChart = null; }
              currentChart = window.echarts.init(node, cfg.theme.dark ? "dark" : null, { renderer: "svg" });
              currentChart.setOption(JSON.parse(cfg.source));
            } catch (err) {
              node.textContent = "ECharts error: " + err.message;
            }
            postHeight();
          }).catch(function (err) { figure.textContent = err.message; postHeight(); });
          return;
        }

        if (cfg.kind === "dot") {
          loadModule(cfg.assets.graphviz).then(function (mod) {
            return mod.Graphviz.load().then(function (graphviz) {
              var svg = graphviz.dot(withTransparentBackground(cfg.source));
              var start = svg.indexOf("<svg");
              figure.innerHTML = start >= 0 ? svg.slice(start) : svg;
              postHeight();
            });
          }).catch(function (err) { figure.textContent = "Graphviz error: " + errorText(err); postHeight(); });
          return;
        }

        if (cfg.kind === "d2") {
          loadModule(cfg.assets.d2).then(function (mod) {
            var d2 = new mod.D2();
            return d2.compile({
              fs: { index: cfg.source },
              options: { themeID: cfg.theme.dark ? 200 : 0, pad: 16 }
            }).then(function (res) {
              return d2.render(res.diagram, Object.assign({}, res.renderOptions, { noXMLTag: true, salt: String(++renderSalt) }));
            });
          }).then(function (svg) {
            figure.innerHTML = svg;
            postHeight();
          }).catch(function (err) { figure.textContent = "D2 error: " + errorText(err); postHeight(); });
          return;
        }

        if (cfg.kind === "drawio") {
          // The viewer defaults several asset roots to https://viewer.diagrams.net/…;
          // point them at dead local paths so anything the static build did not inline
          // degrades instead of phoning home, and stub MathJax so initMath is a no-op.
          window.MathJax = window.MathJax || {};
          window.PROXY_URL = "about:blank";
          window.STYLE_PATH = "about:blank";
          window.SHAPES_PATH = "about:blank";
          window.STENCIL_PATH = "about:blank";
          window.DRAW_MATH_URL = "about:blank";
          loadAssets([cfg.assets.drawio]).then(function () {
            if (window.Editor && window.Editor.MathJaxRender == null) {
              window.Editor.MathJaxRender = function () {};
            }
            if (!window.GraphViewer) { throw new Error("draw.io viewer loaded without GraphViewer"); }
            var host = document.createElement("div");
            figure.appendChild(host);
            host.dataset.mxgraph = JSON.stringify({ xml: cfg.source, page: 0, nav: false, toolbar: null });
            window.GraphViewer.createViewerForElement(host);
            postHeight();
          }).catch(function (err) { figure.textContent = "draw.io error: " + errorText(err); postHeight(); });
          return;
        }

        if (cfg.kind === "map") {
          loadAssets([cfg.assets.leafletCSS, cfg.assets.leaflet]).then(function () {
            var m = JSON.parse(cfg.source);
            var node = document.createElement("div");
            node.style.width = "100%";
            node.style.height = cfg.mapHeight + "px";
            figure.appendChild(node);
            var layers = tileLayers(m.type);
            var map = window.L.map(node, { attributionControl: true }).setView([m.lat, m.long], m.zoom);
            layers.base.addTo(map);
            if (layers.overlay) { layers.overlay.addTo(map); }
            m.markers.forEach(function (marker) {
              var popup = document.createElement("div");
              var link = document.createElement("a");
              link.href = "trackwiki://" + encodeURIComponent(marker.target);
              link.textContent = marker.display || marker.target;
              popup.appendChild(link);
              if (marker.description) {
                var p = document.createElement("p");
                p.textContent = marker.description;
                popup.appendChild(p);
              }
              window.L.marker([marker.lat, marker.long], {
                title: marker.display,
                alt: marker.display,
                icon: window.L.divIcon({ className: "map-fence-marker", iconSize: [16, 16] })
              }).addTo(map).bindPopup(popup);
            });
            postHeight();
          }).catch(function (err) { figure.textContent = errorText(err); postHeight(); });
          return;
        }
      }

      // Anchors inside a figure must never navigate the island: cancel the
      // click and hand the resolved URL to native (decidePolicyFor stays as
      // the backstop for cases this handler misses, e.g. modified clicks).
      document.addEventListener("click", function (event) {
        var target = event.target;
        if (!target || typeof target.closest !== "function") { return; }
        var anchor = target.closest("a");
        if (!anchor) { return; }
        var href = anchor.getAttribute("href");
        if (href && href !== "" && href.charAt(0) !== "#") {
          event.preventDefault();
          post({ type: "link", url: anchor.href });
        }
      });

      var observer = new ResizeObserver(postHeight);
      observer.observe(figure);
      postHeight();

      window.trackFigure = { render: render };
    })();
    </script>
    </body>
    </html>
    """
}