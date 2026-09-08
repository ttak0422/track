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

    /// The name the shell JS dispatches on.
    var kindName: String {
        switch self {
        case .mermaid: return "mermaid"
        case .math: return "math"
        case .echarts: return "echarts"
        case .svg: return "svg"
        case .html: return "html"
        }
    }

    /// The payload passed to the renderer.
    var source: String {
        switch self {
        case .mermaid(let source), .echarts(let source), .svg(let source), .html(let source):
            return source
        case .math(let source, _):
            return source
        }
    }

    /// Whether math renders in display mode; nil for non-math kinds.
    var displayMode: Bool? {
        if case .math(_, let display) = self { return display }
        return nil
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

    /// Default CDN root for the pinned packages.
    public static let cdnRoot = URL(string: "https://cdn.jsdelivr.net/npm")!

    /// Default height (points) reserved for an echarts figure, which needs an
    /// explicit size before `init`; callers can reserve the same space while
    /// the island measures itself.
    public static let defaultEchartsHeight: CGFloat = 400

    /// Asset URLs by the name the shell JS knows ("mermaid", "katex",
    /// "katexCSS", "echarts").
    ///
    /// With no override these are the pinned jsdelivr URLs. With a local root
    /// they become `<root>/<basename>` (mermaid.min.js, katex.min.js,
    /// katex.min.css, echarts.min.js) so the same shell works fully offline
    /// from a directory of the pinned files.
    static func resolved(localScriptURL: URL?) -> [String: String] {
        let remotePaths = [
            "mermaid": "mermaid@\(mermaidVersion)/dist/mermaid.min.js",
            "katex": "katex@\(katexVersion)/dist/katex.min.js",
            "katexCSS": "katex@\(katexVersion)/dist/katex.min.css",
            "echarts": "echarts@\(echartsVersion)/dist/echarts.min.js",
        ]
        guard let local = localScriptURL else {
            return remotePaths.mapValues { cdnRoot.appending(path: $0).absoluteString }
        }
        let localBasenames = [
            "mermaid": "mermaid.min.js",
            "katex": "katex.min.js",
            "katexCSS": "katex.min.css",
            "echarts": "echarts.min.js",
        ]
        return localBasenames.mapValues { local.appending(path: $0).absoluteString }
    }
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

    public init(
        kind: FigureKind,
        height: Binding<CGFloat>,
        theme: FigureTheme,
        onLink: ((URL) -> Void)? = nil,
        localScriptURL: URL? = nil,
        echartsHeight: CGFloat = FigureAssets.defaultEchartsHeight
    ) {
        self.kind = kind
        self._height = height
        self.theme = theme
        self.onLink = onLink
        self.localScriptURL = localScriptURL
        self.echartsHeight = echartsHeight
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

      function post(msg) {
        window.webkit.messageHandlers.fig.postMessage(msg);
      }

      function postHeight() {
        post({ type: "height", height: Math.ceil(figure.getBoundingClientRect().height) });
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

      function loadAssets(items) {
        return Promise.all(items.map(function (item) {
          if (assetCache[item]) { return assetCache[item]; }
          var promise = item.slice(-4) === ".css" ? loadCSS(item) : loadScript(item);
          assetCache[item] = promise;
          promise.catch(function () { delete assetCache[item]; });
          return promise;
        }));
      }

      function render(cfg) {
        figure.className = cfg.kind === "svg" ? "raw-svg" : "";
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