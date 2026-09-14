import Foundation

public enum MediaAssetContent {
    public static func name(for url: URL) -> String {
        URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?
            .first(where: { $0.name == "name" })?.value ?? url.lastPathComponent
    }
    public static func figure(text: String, url: URL) -> FigureKind? {
        let name = name(for: url).lowercased()
        if name.hasSuffix(".echarts.json") { return .echarts(optionJSON: text) }
        switch (name as NSString).pathExtension {
        case "mermaid", "mmd": return .mermaid(text)
        case "dot", "gv": return .dot(text)
        case "d2": return .d2(text)
        case "drawio": return .drawio(text)
        default: return nil
        }
    }
    public static func text(from data: Data) -> String? {
        guard !data.contains(0) else { return nil }
        return String(data: data, encoding: .utf8)
    }
    public static func isolatedHTML(url: URL) -> String {
        guard ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return "" }
        let escaped = url.absoluteString.replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "\"", with: "&quot;")
            .replacingOccurrences(of: "<", with: "&lt;")
        return """
        <!doctype html><meta charset="utf-8"><style>
        html,body,iframe{margin:0;width:100%;height:100%;border:0}html{color-scheme:light dark}
        </style><iframe title="Embedded page" src="\(escaped)"
        sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox allow-downloads allow-modals"
        allow="clipboard-write"></iframe>
        """
    }
}
