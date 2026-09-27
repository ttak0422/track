import AppKit
import Foundation
import PDFKit
import SwiftUI
import WebKit
@testable import TrackUI

let base = URL(string: "http://127.0.0.1:7331")!
let asset = GFMBody.assetHref("./assets/図表.echarts.json", vault: "other", baseURL: base)!
let items = URLComponents(url: asset, resolvingAgainstBaseURL: false)!.queryItems!
precondition(items.contains { $0.name == "vault" && $0.value == "other" })
precondition(MediaAssetContent.name(for: asset) == "図表.echarts.json")
precondition(MediaAssetContent.figure(text: "{}", url: asset) == .echarts(optionJSON: "{}"))
for source in ["example.com/page", "javascript:alert(1)", "https://example.com/a.pdf", "/assets/x"] {
    precondition(GFMBody.assetHref(source, vault: "other", baseURL: base) == nil, "only explicit vault asset paths route to the asset endpoint")
}
for (name, expected) in [("a.mmd", FigureKind.mermaid("test")), ("a.gv", .dot("test")), ("a.d2", .d2("test")), ("a.drawio", .drawio("test"))] {
    let url = GFMBody.assetHref("assets/" + name, vault: "other", baseURL: base)!
    precondition(MediaAssetContent.figure(text: "test", url: url) == expected)
}
let longText = (1...350).map { "Line \($0)" }.joined(separator: "\n")
precondition(MediaAssetContent.text(from: Data(longText.utf8)) == longText)
precondition(MediaAssetContent.text(from: Data([65, 0, 66])) == nil)
let option = #"{"series":[{"markLine":{"data":[{"xAxis":"2026-09-01","box":{"date":"September 1"},"label":{"formatter":"Full annotation headline"},"href":"https://example.com/source"},{"xAxis":"2026-09-02","box":{},"label":{"formatter":"Unsafe source stays text"},"href":"javascript:alert(1)"}]}}]}"#
let evidence = FigureEvidence.parse(option)
precondition(evidence.count == 2 && evidence[0].date == "September 1")
precondition(evidence[0].headline == "Full annotation headline" && evidence[0].source?.host == "example.com")
precondition(evidence[1].date == "2026-09-02" && evidence[1].source == nil)
precondition(FigureEvidence.parse("invalid").isEmpty)
let html = MediaAssetContent.isolatedHTML(url: asset)
precondition(html.contains("sandbox=\"allow-scripts") && !html.contains("allow-same-origin") && !html.contains("allow-top-navigation"))
precondition(html.contains("&amp;vault=other") && !html.contains("webkit.messageHandlers"))
precondition(MediaAssetContent.isolatedHTML(url: URL(string: "file:///tmp/a.html")!).isEmpty)
print("Figure/media checks passed: scoped asset routing, diagram attachments, full text, binary fallback, annotation/source retention and opaque HTML frame")

// Optional actual WebKit check: no CDN, microphone, gateway, or vault writes.
// The SVG is local source. This catches the initial dispatch and folded toolbar
// height regressions that pure string assertions would miss.
@MainActor
func evaluate(_ web: WKWebView, _ script: String) async throws -> String {
    try await withCheckedThrowingContinuation { continuation in
        web.evaluateJavaScript(script) { value, error in
            if let error { continuation.resume(throwing: error) }
            else { continuation.resume(returning: value.map { String(describing: $0) } ?? "") }
        }
    }
}

if CommandLine.arguments.contains("--webview") {
    let app = NSApplication.shared
    app.setActivationPolicy(.accessory)
    var links: [URL] = []
    var reportedHeight: CGFloat = 250
    let measuredHeight = Binding(get: { reportedHeight }, set: { reportedHeight = $0 })
    let host = NSHostingView(rootView: FigureHost(kind: .svg("<svg xmlns='http://www.w3.org/2000/svg' width='640' height='200'><rect width='640' height='200' fill='#286957'/><text x='30' y='100' fill='white'>Native figure smoke check</text></svg>"), height: measuredHeight, theme: .light, onLink: { links.append($0) }))
    let window = NSWindow(contentRect: NSRect(x: 80, y: 80, width: 720, height: 320), styleMask: [.titled, .closable], backing: .buffered, defer: false)
    window.contentView = host
    window.orderFront(nil)
    func webView(in view: NSView) -> WKWebView? {
        if let web = view as? WKWebView { return web }
        for child in view.subviews { if let found = webView(in: child) { return found } }
        return nil
    }
    var ready: WKWebView?
    for _ in 0..<160 {
        if let web = webView(in: host), let count = try? await evaluate(web, "document.querySelectorAll('#figure svg').length"), count == "1" {
            ready = web
            break
        }
        try await Task.sleep(for: .milliseconds(50))
    }
    guard let web = ready else {
        if let web = webView(in: host) {
            let state = try? await evaluate(web, "[document.readyState, typeof window.trackFigure, document.body.innerText].join(' | ')")
            print("WebKit state: \(String(describing: state))")
        } else { print("No WKWebView mounted: \(host.subviews)") }
        try FigureAssets.shellHTML.write(toFile: "/private/tmp/native-figure-shell.html", atomically: true, encoding: .utf8)
        fatalError("Figure did not render its initial source")
    }
    let folded = try await evaluate(web, "document.getElementById('fold').click(); document.getElementById('toolbar').getBoundingClientRect().height")
    precondition(Double(folded)! >= 30, "folding must retain a visible expand control")
    try await Task.sleep(for: .milliseconds(100))
    precondition(reportedHeight >= 30 && reportedHeight < 50, "native height includes the collapsed toolbar")
    _ = try await evaluate(web, "document.getElementById('fold').click(); document.getElementById('zoomIn').click()")
    let zoom = try await evaluate(web, "document.getElementById('zoomReset').textContent")
    precondition(zoom == "110%")
    let snapshot = try await web.takeSnapshot(configuration: nil)
    if let tiff = snapshot.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
        try png.write(to: URL(fileURLWithPath: "/private/tmp/native-figure-smoke.png"))
    }
    print("WebKit checks passed: initial source dispatch, fold controls, zoom; snapshot /private/tmp/native-figure-smoke.png")
    // Exercise the real shell with a small in-memory chart engine so the
    // event/default wiring is verified without downloading ECharts from a CDN.
    _ = try await evaluate(web, """
    window.echarts = {init: function() {return {
      setOption: function(option) {window.testOption=option},
      on: function(name,handler) {if(name==='click') window.testClick=handler},
      resize:function(){}, dispose:function(){}
    }}};
    window.trackFigure.render({kind:'echarts', source:JSON.stringify({legend:{data:['A']},series:[{name:'A',data:[1,2]}]}),
      height:320, assets:{echarts:'data:text/javascript,void(0)'},
      theme:{bg:'#fff',fg:'#111',panelSoft:'#eee',chart:['#286957']}});
    """)
    for _ in 0..<100 {
        if try await evaluate(web, "typeof window.testClick") == "function" { break }
        try await Task.sleep(for: .milliseconds(50))
    }
    let legend = try await evaluate(web, "window.testOption.legend.data[0]")
    precondition(legend == "A", "retain the chart legend")
    _ = try await evaluate(web, "window.testClick({data:{href:'https://example.com/source',note:'other~42'}}); window.testClick({data:{note:'other~42'}}); window.testClick({data:{href:'javascript:alert(1)'}})")
    try await Task.sleep(for: .milliseconds(100))
    precondition(links.count == 2 && links[0].absoluteString == "https://example.com/source")
    precondition(links[1].absoluteString.removingPercentEncoding == "trackwiki://other~42")
    print("Chart shell checks passed: legend preserved, source URL preferred, qualified note provenance, unsafe URL ignored")
    window.orderOut(nil)
    let pdf = PDFDocument()
    for _ in 0..<3 { pdf.insert(PDFPage(image: NSImage(size: NSSize(width: 300, height: 200), flipped: false) { rect in NSColor.white.setFill(); rect.fill(); return true })!, at: pdf.pageCount) }
    var page = 1
    let pageBinding = Binding(get: { page }, set: { page = $0 })
    let pdfHost = NSHostingView(rootView: PDFDocumentView(document: pdf, page: pageBinding, displayMode: .deck, advance: {}, movePage: { _ in }))
    window.contentView = pdfHost
    window.orderFront(nil)
    try await Task.sleep(for: .milliseconds(150))
    func pdfView(in view: NSView) -> PDFView? {
        if let pdf = view as? PDFView { return pdf }
        return view.subviews.lazy.compactMap { pdfView(in: $0) }.first
    }
    guard let pdfView = pdfView(in: pdfHost) else { fatalError("PDF did not mount") }
    page = 2
    pdfHost.rootView = PDFDocumentView(document: pdf, page: pageBinding, displayMode: .deck, advance: {}, movePage: { _ in })
    try await Task.sleep(for: .milliseconds(100))
    precondition(pdfView.currentPage === pdf.page(at: 1), "direct page input navigates PDFKit")
    pdfView.go(to: pdf.page(at: 2)!)
    try await Task.sleep(for: .milliseconds(100))
    precondition(page == 3, "PDFKit page changes update native page controls")
    print("PDF checks passed: direct page selection and PDFKit-to-control synchronization")
    window.orderOut(nil)
}
