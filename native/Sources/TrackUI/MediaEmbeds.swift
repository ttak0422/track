import AppKit
import Foundation
import MapKit
import PDFKit
import SwiftUI
import TrackAPI

// Media embeds for the native reader: the inline surfaces a note's
// `![alt](src)` attachments draw with. The web reader routes every src by the
// same rules (web/src/components/markdown/Embed.tsx + urls.ts); these views
// are the native ports of that routing, kept standalone so the Markdown
// renderer can mount them per embed. Every view degrades to a plain link when
// its content cannot be fetched or drawn, so an embed is never a dead end.
//
// - OgpCardView fetches Open Graph metadata through the server and draws a
//   card (image, site name, title, description).
// - PdfNoteView downloads the PDF and shows it in a PDFKit page strip.
// - TextAssetView fetches a text file and renders it monospaced.
// - YouTubeView / MapsView rebuild the privacy-enhanced embed URL and draw it
//   inside a FigureHost island (the web's iframe inside a MediaFrame).

// MARK: - Shared fallback

/// The shared degradation when an embed cannot render: a plain link, the
/// native counterpart of the web's `md-link embed-fallback`.
struct PlainLinkView: View {
    let url: URL

    var body: some View {
        Link(destination: url) {
            Text(url.absoluteString)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

// MARK: - URL assembly (ports of web/src/components/markdown/urls.ts)

/// The pure URL helpers the embed routing shares; each is a port of the
/// matching function in urls.ts so the native and web embeds agree on what a
/// src becomes (or stays). Only the helpers the native embeds use are ported.
enum MediaEmbedsURLs {
    /// `webHref`: upgrade a bare domain ("www.x.com", "example.com/path") to
    /// an https URL, leaving anything that already has a scheme (or is not
    /// domain-like) untouched.
    static func webHref(_ href: String) -> String {
        let trimmed = href.trimmingCharacters(in: .whitespaces)
        if trimmed.lowercased().hasPrefix("www.") {
            return "https://" + trimmed
        }
        // /^www\./i || /^[\w.-]+\.[a-z]{2,}(?:[/:?#]|$)/i
        let domainLike = "^[\\w.-]+\\.[a-z]{2,}(?:[/:?#]|$)"
        if trimmed.range(of: domainLike, options: [.regularExpression, .caseInsensitive]) != nil {
            return "https://" + trimmed
        }
        return href
    }

    /// `youtubeStartSeconds`: parse a YouTube timestamp — plain seconds
    /// ("90") or the 1h2m3s form; 0 for anything unparseable.
    static func youtubeStartSeconds(_ raw: String?) -> Int {
        guard let raw, !raw.isEmpty else { return 0 }
        if raw.range(of: "^[0-9]+$", options: .regularExpression) != nil {
            return Int(raw) ?? 0
        }
        let pattern = "^(?:([0-9]+)h)?(?:([0-9]+)m)?(?:([0-9]+)s)?$"
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: raw, range: NSRange(raw.startIndex..., in: raw)) else {
            return 0
        }
        func group(_ index: Int) -> Int {
            guard let range = Range(match.range(at: index), in: raw) else { return 0 }
            return Int(raw[range]) ?? 0
        }
        let hours = group(1), minutes = group(2), seconds = group(3)
        guard hours > 0 || minutes > 0 || seconds > 0 else { return 0 }
        return hours * 3600 + minutes * 60 + seconds
    }

    /// `youtubeEmbedUrl`: turn a YouTube watch/share/shorts/embed URL into the
    /// privacy-enhanced `youtube-nocookie.com/embed/…` URL, carrying a start
    /// time when the original had one. nil for anything that is not clearly a
    /// YouTube video URL.
    static func youtubeEmbedURL(from src: String) -> URL? {
        guard let url = URL(string: webHref(src)) else { return nil }
        let host = strippedHost(url)
        var id = ""
        if host == "youtu.be" {
            id = url.pathComponents.dropFirst().first ?? ""
        } else if host == "youtube.com" || host == "m.youtube.com" || host == "youtube-nocookie.com" {
            if url.path == "/watch" {
                id = queryParameter(of: url, named: "v") ?? ""
            } else {
                // /^\/(?:embed|shorts|live|v)\/([^/?#]+)/
                id = firstMatch("^/(?:embed|shorts|live|v)/([^/?#]+)", in: url.path) ?? ""
            }
        }
        // /^[\w-]{6,}$/
        guard id.range(of: "^[A-Za-z0-9_-]{6,}$", options: .regularExpression) != nil else { return nil }
        let start = youtubeStartSeconds(queryParameter(of: url, named: "t") ?? queryParameter(of: url, named: "start"))
        var result = "https://www.youtube-nocookie.com/embed/\(id)"
        if start > 0 { result += "?start=\(start)" }
        return URL(string: result)
    }

    /// `googleMapsEmbedUrl`: turn a Google Maps share/embed URL into the
    /// keyless inline embed `https://maps.google.com/maps?…&output=embed`.
    /// An existing `/maps/embed` URL passes through unchanged. nil for
    /// anything not clearly a Google Maps URL (goo.gl short links included).
    static func googleMapsEmbedURL(from src: String) -> URL? {
        guard let url = URL(string: webHref(src)) else { return nil }
        let host = strippedHost(url)
        guard host == "google.com" || host == "maps.google.com" else { return nil }
        if url.path.hasPrefix("/maps/embed") {
            return safeFrameURL(url.absoluteString)
        }
        let params = queryParameters(of: url)
        var query = params["q"]?.trimmingCharacters(in: .whitespaces) ?? ""
        var zoom = params["z"]?.trimmingCharacters(in: .whitespaces) ?? ""
        if query.isEmpty {
            // /@(-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?)(?:,(\d+(?:\.\d+)?)z)?/ on the path
            let at = matches("@(-?[0-9]+(?:\\.[0-9]+)?),(-?[0-9]+(?:\\.[0-9]+)?)(?:,([0-9]+(?:\\.[0-9]+)?)z)?", in: url.path)
            if let at, let lat = at[1], let lng = at[2] {
                query = "\(lat),\(lng)"
                if let zoomRaw = at[3] {
                    zoom = String(Int((Double(zoomRaw) ?? 0).rounded()))
                }
            } else {
                query = params["ll"]?.trimmingCharacters(in: .whitespaces) ?? ""
            }
        }
        guard !query.isEmpty else { return nil }
        var items = ["q=\(formEncode(query))"]
        if !zoom.isEmpty { items.append("z=\(formEncode(zoom))") }
        items.append("output=embed")
        return URL(string: "https://maps.google.com/maps?" + items.joined(separator: "&"))
    }

    /// Coordinates are deliberately a small, conservative subset: locations
    /// expressed as `q=lat,long` or `/@lat,long` can be shown without loading a
    /// web view. Place-name queries stay on the keyless embed fallback below.
    static func googleMapsCoordinate(from src: String) -> (latitude: Double, longitude: Double)? {
        guard let url = URL(string: webHref(src)) else { return nil }
        let query = queryParameters(of: url)["q"] ?? queryParameters(of: url)["ll"]
        let pathCoordinate = matches("@(-?[0-9]+(?:\\.[0-9]+)?),(-?[0-9]+(?:\\.[0-9]+)?)", in: url.path)
            .flatMap { groups -> String? in
                guard groups.count > 2, let latitude = groups[1], let longitude = groups[2] else { return nil }
                return "\(latitude),\(longitude)"
            }
        let raw = query ?? pathCoordinate
        let parts = raw?.split(separator: ",", maxSplits: 1).compactMap { Double($0) } ?? []
        guard parts.count == 2, abs(parts[0]) <= 90, abs(parts[1]) <= 180 else { return nil }
        return (parts[0], parts[1])
    }

    /// `safeFrameUrl`: only http(s) and same-origin relative paths are safe to
    /// load in a frame; javascript:/data: and other schemes return nil.
    static func safeFrameURL(_ target: String) -> URL? {
        let trimmed = target.trimmingCharacters(in: .whitespaces)
        let lower = trimmed.lowercased()
        if lower.hasPrefix("https://") || lower.hasPrefix("http://")
            || trimmed.hasPrefix("/") || trimmed.hasPrefix("./") {
            return URL(string: trimmed)
        }
        return nil
    }

    // MARK: Helpers

    private static func strippedHost(_ url: URL) -> String {
        (url.host ?? "")
            .replacingOccurrences(of: "^www\\.", with: "", options: .regularExpression)
            .lowercased()
    }

    /// First value per query name (JS `URLSearchParams.get` returns the first).
    private static func queryParameters(of url: URL) -> [String: String] {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return [:] }
        var result: [String: String] = [:]
        for item in components.queryItems ?? [] {
            if result[item.name] == nil { result[item.name] = item.value ?? "" }
        }
        return result
    }

    private static func queryParameter(of url: URL, named name: String) -> String? {
        queryParameters(of: url)[name]
    }

    private static func firstMatch(_ pattern: String, in text: String) -> String? {
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)),
              let range = Range(match.range(at: 1), in: text) else { return nil }
        return String(text[range])
    }

    /// First match's capture groups, indexed by group number (1-based, so
    /// `groups[1]` is the first capture); an unmatched optional group is nil.
    /// nil when nothing matches.
    private static func matches(_ pattern: String, in text: String) -> [String?]? {
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) else {
            return nil
        }
        var groups: [String?] = Array(repeating: nil, count: match.numberOfRanges)
        for index in 1..<match.numberOfRanges {
            if let range = Range(match.range(at: index), in: text) {
                groups[index] = String(text[range])
            }
        }
        return groups
    }

    /// application/x-www-form-urlencoded percent-encoding of a query value —
    /// the same output `URLSearchParams` produces in urls.ts (a comma becomes
    /// %2C, and so on).
    private static func formEncode(_ value: String) -> String {
        let allowed = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789*-._")
        return value.addingPercentEncoding(withAllowedCharacters: allowed) ?? value
    }
}

// MARK: - Open Graph card

/// The rich card for an embedded http(s) page (web Embed's OgpCard): fetch
/// the link's Open Graph metadata through the server and draw image, site
/// name, title and description. While loading the host is shown; on a failed
/// or blocked fetch the embed degrades to a plain link.
public struct OgpCardView: View {
    private let client: TrackClient
    private let url: URL
    @State private var ogp: OgpResponse?
    @State private var failed = false

    public init(client: TrackClient, url: URL) {
        self.client = client
        self.url = url
    }

    public var body: some View {
        Group {
            if failed {
                PlainLinkView(url: url)
            } else if let ogp {
                card(ogp)
            } else {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text(url.host ?? url.absoluteString)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task(id: url) { await load() }
    }

    private func load() async {
        do {
            ogp = try await client.getOgp(url: url.absoluteString)
        } catch {
            failed = true
        }
    }

    private func card(_ ogp: OgpResponse) -> some View {
        Link(destination: url) {
            HStack(alignment: .top, spacing: 12) {
                if let image = ogp.image.flatMap(URL.init(string:)) {
                    AsyncImage(url: image) { phase in
                        switch phase {
                        case .success(let image):
                            image.resizable().scaledToFill()
                        default:
                            // Missing/undecodable image: the card carries on
                            // without the thumbnail rather than showing a hole.
                            Color.clear
                        }
                    }
                    .frame(width: 120, height: 76)
                    .clipped()
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text(ogp.siteName ?? url.host ?? url.absoluteString)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(ogp.title ?? url.absoluteString)
                        .font(.headline)
                        .lineLimit(2)
                    if let description = ogp.description, !description.isEmpty {
                        Text(description)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(3)
                    }
                }
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
        .buttonStyle(.plain)
    }
}

// MARK: - PDF embed

/// An embedded PDF (web Embed's PdfDeck): the file is downloaded and drawn as
/// a continuous, auto-scaling PDFView page strip. Degrades to a plain link
/// when the fetch fails or the data is not a PDF.
public struct PdfNoteView: View {
    private let assetURL: URL
    @State private var document: PDFDocument?
    @State private var failed = false
    @State private var page = 1

    public init(assetURL: URL) {
        self.assetURL = assetURL
    }

    public var body: some View {
        Group {
            if failed {
                PlainLinkView(url: assetURL)
            } else if let document {
                VStack(spacing: 6) {
                    PDFDocumentView(document: document, page: $page)
                    .frame(height: 420)
                    pdfControls(pageCount: document.pageCount)
                }
            } else {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Loading PDF…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task(id: assetURL) { await load() }
    }

    private func load() async {
        do {
            let (data, response) = try await URLSession.shared.data(from: assetURL)
            if let http = response as? HTTPURLResponse, !(200..<300).contains(http.statusCode) {
                failed = true
                return
            }
            guard let document = PDFDocument(data: data) else {
                failed = true
                return
            }
            page = 1
            self.document = document
        } catch {
            failed = true
        }
    }

    /// The strip remains a PDFView (and therefore keeps its native scrolling),
    /// while this small quiet-chip bar provides the same orientation and direct
    /// page access as PdfDeck.
    private func pdfControls(pageCount: Int) -> some View {
        HStack(spacing: 8) {
            Button("‹") { page = max(1, page - 1) }
                .disabled(page <= 1)
                .accessibilityLabel("Previous page")
            TextField("Page", value: $page, format: .number)
                .frame(width: 42)
                .multilineTextAlignment(.center)
                .onSubmit { page = min(pageCount, max(1, page)) }
            Text("/ \(pageCount)")
                .foregroundStyle(.secondary)
            Button("›") { page = min(pageCount, page + 1) }
                .disabled(page >= pageCount)
                .accessibilityLabel("Next page")
        }
        .buttonStyle(.borderless)
        .font(.caption)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color(nsColor: .controlBackgroundColor))
        .overlay(RoundedRectangle(cornerRadius: 5).stroke(Color(nsColor: .separatorColor), lineWidth: 0.5))
    }
}

/// PDFView wrapped for SwiftUI.
private struct PDFDocumentView: NSViewRepresentable {
    let document: PDFDocument
    @Binding var page: Int

    func makeCoordinator() -> Coordinator { Coordinator(page: $page) }

    func makeNSView(context: Context) -> PDFView {
        let view = PDFView()
        view.document = document
        view.autoScales = true
        view.displayMode = .singlePageContinuous
        view.displayDirection = .vertical
        view.backgroundColor = .clear
        context.coordinator.observe(view)
        return view
    }

    func updateNSView(_ nsView: PDFView, context: Context) {
        if nsView.document !== document {
            nsView.document = document
        }
        guard document.pageCount > 0, let targetPage = document.page(at: max(0, min(document.pageCount - 1, page - 1))) else {
            return
        }
        let target = min(document.pageCount, max(1, page))
        if target != page { page = target }
        if nsView.currentPage !== targetPage {
            nsView.go(to: targetPage)
        }
    }

    final class Coordinator {
        private var page: Binding<Int>
        private var observer: NSObjectProtocol?

        init(page: Binding<Int>) { self.page = page }

        func observe(_ view: PDFView) {
            observer = NotificationCenter.default.addObserver(
                forName: Notification.Name.PDFViewPageChanged,
                object: view,
                queue: .main
            ) { [weak self, weak view] _ in
                guard let self, let view, let current = view.currentPage,
                      let index = view.document?.index(for: current) else { return }
                self.page.wrappedValue = index + 1
            }
        }

        deinit {
            if let observer { NotificationCenter.default.removeObserver(observer) }
        }
    }
}

// MARK: - Text-file embed

/// An embedded text-file attachment (web Embed's TextAssetEmbed): the content
/// is fetched and rendered as monospaced text. A failed fetch or non-UTF-8
/// data (the web's `isBinaryText` sniff, covered natively by the decode)
/// degrades to a plain link instead of dumping mojibake.
public struct TextAssetView: View {
    private let url: URL
    @State private var text: String?
    @State private var failed = false

    public init(url: URL) {
        self.url = url
    }

    public var body: some View {
        Group {
            if failed {
                PlainLinkView(url: url)
            } else if let text {
                ScrollView {
                    Text(text)
                        .font(.system(.body, design: .monospaced))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .textSelection(.enabled)
                }
                .frame(maxHeight: 420)
            } else {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Loading…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task(id: url) { await load() }
    }

    private func load() async {
        do {
            let (data, response) = try await URLSession.shared.data(from: url)
            if let http = response as? HTTPURLResponse, !(200..<300).contains(http.statusCode) {
                failed = true
                return
            }
            guard let text = String(data: data, encoding: .utf8) else {
                failed = true
                return
            }
            self.text = text
        } catch {
            failed = true
        }
    }
}

// MARK: - YouTube / Maps embeds

/// The shared FigureHost wrapper the video/map embeds draw through: one lazy
/// island that renders the embed iframe and resizes itself to the content
/// height the island reports back through the binding.
private struct FigureEmbedView: View {
    let html: String
    @State private var height: CGFloat
    @Environment(\.colorScheme) private var colorScheme

    init(html: String, defaultHeight: CGFloat) {
        self.html = html
        _height = State(initialValue: defaultHeight)
    }

    var body: some View {
        FigureHost(
            kind: .html(html),
            height: $height,
            theme: colorScheme == .dark ? .dark : .light
        )
        .frame(height: height)
    }
}

/// An embedded YouTube video (web Embed's `embed-video` iframe): the src is
/// rebuilt as a privacy-enhanced `youtube-nocookie.com` embed URL and drawn in
/// a FigureHost island. Anything that is not clearly a YouTube URL degrades to
/// a plain link.
public struct YouTubeView: View {
    private let src: String

    public init(src: String) {
        self.src = src
    }

    public var body: some View {
        if let embed = MediaEmbedsURLs.youtubeEmbedURL(from: src) {
            FigureEmbedView(
                html: Self.iframeHTML(
                    embedURL: embed,
                    title: "YouTube video",
                    // The web's embed iframe allow list, verbatim.
                    allow: "accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share",
                    style: "width:100%;aspect-ratio:16/9;border:0"
                ),
                defaultHeight: 240
            )
        } else {
            fallback
        }
    }

    @ViewBuilder
    private var fallback: some View {
        if let url = URL(string: MediaEmbedsURLs.webHref(src)) {
            PlainLinkView(url: url)
        } else {
            Text(src).font(.caption).foregroundStyle(.secondary)
        }
    }

    fileprivate static func iframeHTML(embedURL: URL, title: String, allow: String, style: String) -> String {
        let src = embedURL.absoluteString
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "\"", with: "&quot;")
            .replacingOccurrences(of: "<", with: "&lt;")
        return #"<div class="embed"><iframe src="\#(src)" title="\#(title)" loading="lazy" allow="\#(allow)" allowfullscreen style="\#(style)"></iframe></div>"#
    }
}

/// An embedded Google Maps location (web Embed's `embed-map` iframe): the src
/// is rebuilt as a keyless `output=embed` URL and drawn in a FigureHost
/// island. Anything that is not clearly a Google Maps URL degrades to a plain
/// link.
public struct MapsView: View {
    private let src: String

    public init(src: String) {
        self.src = src
    }

    public var body: some View {
        if let coordinate = MediaEmbedsURLs.googleMapsCoordinate(from: src) {
            NativeMapView(latitude: coordinate.latitude, longitude: coordinate.longitude)
                .frame(height: 320)
        } else if let embed = MediaEmbedsURLs.googleMapsEmbedURL(from: src) {
            FigureEmbedView(
                html: YouTubeView.iframeHTML(
                    embedURL: embed,
                    title: "Map",
                    allow: "",
                    style: "width:100%;height:320px;border:0"
                ),
                defaultHeight: 320
            )
        } else {
            fallback
        }
    }

    @ViewBuilder
    private var fallback: some View {
        if let url = URL(string: MediaEmbedsURLs.webHref(src)) {
            PlainLinkView(url: url)
        } else {
            Text(src).font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// A permission-free native map surface for coordinate URLs. MapKit's map
/// tiles are public display content; this view does not request location
/// services, so no Info.plist usage description is needed. URLs containing a
/// place name deliberately use MapsView's existing keyless web fallback.
private struct NativeMapView: NSViewRepresentable {
    let latitude: Double
    let longitude: Double

    func makeNSView(context: Context) -> MKMapView {
        let view = MKMapView()
        view.isRotateEnabled = false
        view.isPitchEnabled = false
        view.showsCompass = true
        return view
    }

    func updateNSView(_ view: MKMapView, context: Context) {
        let coordinate = CLLocationCoordinate2D(latitude: latitude, longitude: longitude)
        let span = max(0.001, 180 / pow(2, 10.0))
        view.setRegion(MKCoordinateRegion(center: coordinate, span: MKCoordinateSpan(
            latitudeDelta: span,
            longitudeDelta: span
        )), animated: false)
        view.removeAnnotations(view.annotations)
        let pin = MKPointAnnotation()
        pin.coordinate = coordinate
        view.addAnnotation(pin)
    }
}
