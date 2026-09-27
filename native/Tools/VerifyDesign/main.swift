import AppKit
import SwiftUI
import TrackUI

@main
struct VerifyDesign {
    @MainActor
    static func main() throws {
        precondition(ContentWidthMode.normal.proseWidth(scale: 1) == 640)
        precondition(ContentWidthMode.normal.proseWidth(scale: 2) == 1280)
        precondition(ContentWidthMode.wide.proseWidth(scale: 1) == 1280)
        precondition(ContentWidthMode.full.proseWidth(scale: 1).isInfinite)
        precondition(NSFont(name: TrackTypography.readingFamily, size: 16) != nil)
        for palette in [TrackTheme.light, .dark] {
            for ink in [palette.text, palette.muted, palette.faint] {
                for ground in [palette.panel, palette.panelSoft] {
                    let a = luminance(ink), b = luminance(ground)
                    precondition((max(a, b) + 0.05) / (min(a, b) + 0.05) >= 4.5,
                                 "Reading inks must retain WCAG AA contrast")
                }
            }
        }
        // Measure the actual reader, so a theme modifier that accidentally
        // resets paragraph styles fails instead of merely compiling.
        let host = NSHostingView(rootView: GFMBody(
            markdown: String(repeating: "日本語の本文を読みます。", count: 12),
            baseURL: URL(string: "http://127.0.0.1:1")!, vault: ""
        ).frame(width: 320))
        precondition(host.fittingSize.height > 200, "Reader paragraph line spacing was lost")
        let query = """
        ```track-view
        {"layout":"gallery","showTitle":false,"columns":["title","description"],"groups":[{"rows":[{"title":"Hidden title destination","cells":["Hidden title destination","Long metadata remains available"]}]}]}
        ```
        """
        let queryHost = NSHostingView(rootView: GFMBody(markdown: query,
            baseURL: URL(string: "http://127.0.0.1:1")!, vault: "").frame(width: 320))
        precondition(queryHost.fittingSize.width <= 320 && queryHost.fittingSize.height >= 144,
                     "Gallery must retain its cover and metadata within a narrow column")
        func measuredHeight(_ markdown: String) -> CGFloat {
            NSHostingView(rootView: GFMBody(markdown: markdown,
                baseURL: URL(string: "http://127.0.0.1:1")!, vault: "").frame(width: 320)).fittingSize.height
        }
        precondition(measuredHeight("本文\n\n## 見出し\n\n本文") >= measuredHeight("本文\n\n本文\n\n本文") + 26,
                     "Anchored headings must retain their space above the heading")
        if CommandLine.arguments.count == 4, CommandLine.arguments[1] == "--snapshots" {
            try snapshots(markdown: String(contentsOfFile: CommandLine.arguments[2], encoding: .utf8), directory: CommandLine.arguments[3])
        }
        print("Native design: Japanese layout, prose widths, font, light/dark contrast passed")
    }

    @MainActor
    private static func snapshots(markdown: String, directory: String) throws {
        NSApplication.shared.setActivationPolicy(.accessory)
        let suite = "VerifyDesign-\(UUID())"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        let output = URL(fileURLWithPath: directory, isDirectory: true)
        try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
        for (width, height) in [(900, 650), (1280, 800), (1600, 1000)] {
            for (name, scheme) in [("light", ColorScheme.light), ("dark", .dark)] {
                let palette = TrackTheme.palette(for: scheme)
                let content = ScrollView {
                    GFMBody(markdown: markdown, baseURL: URL(string: "http://127.0.0.1:1")!, vault: "")
                        .padding(32)
                }
                .frame(width: CGFloat(width), height: CGFloat(height))
                .background(palette.panel).foregroundStyle(palette.text)
                .environment(\.colorScheme, scheme)
                .environment(\.trackFontScale, 1)
                .defaultAppStorage(defaults)
                let host = NSHostingView(rootView: content)
                host.frame = NSRect(x: 0, y: 0, width: width, height: height)
                let window = NSWindow(contentRect: host.frame, styleMask: [.borderless], backing: .buffered, defer: false)
                window.isReleasedWhenClosed = false
                window.contentView = host
                host.layoutSubtreeIfNeeded()
                RunLoop.main.run(until: Date().addingTimeInterval(0.2))
                let bitmap = host.bitmapImageRepForCachingDisplay(in: host.bounds)!
                host.cacheDisplay(in: host.bounds, to: bitmap)
                let data = bitmap.representation(using: .png, properties: [:])!
                try data.write(to: output.appendingPathComponent("reader-\(name)-\(width)x\(height).png"))
                window.close()
            }
        }
    }

    private static func luminance(_ color: Color) -> Double {
        let color = NSColor(color).usingColorSpace(.sRGB)!
        func linear(_ value: CGFloat) -> Double {
            let value = Double(value)
            return value <= 0.04045 ? value / 12.92 : pow((value + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * linear(color.redComponent) + 0.7152 * linear(color.greenComponent)
            + 0.0722 * linear(color.blueComponent)
    }
}
