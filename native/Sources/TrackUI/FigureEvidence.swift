import Foundation
import SwiftUI

/// Preserve the engine's box annotations alongside the plot, including on
/// narrow preview surfaces where a full rail would hide the headline.
public struct FigureEvidence {
    public let date: String
    public let headline: String
    public let source: URL?

    public static func parse(_ json: String) -> [FigureEvidence] {
        guard let data = json.data(using: .utf8),
              let option = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let series = option["series"] as? [[String: Any]] else { return [] }
        return series.flatMap { series in
            let marks = (series["markLine"] as? [String: Any])?["data"] as? [[String: Any]] ?? []
            return marks.compactMap { mark -> FigureEvidence? in
                guard let box = mark["box"] as? [String: Any] else { return nil }
                let href = (mark["href"] as? String).flatMap(URL.init(string:))
                return FigureEvidence(
                    date: box["date"] as? String ?? mark["xAxis"] as? String ?? "",
                    headline: (mark["label"] as? [String: Any])?["formatter"] as? String ?? "",
                    source: ["http", "https"].contains(href?.scheme?.lowercased() ?? "") ? href : nil
                )
            }
        }
    }
}

struct FigureEvidenceView: View {
    let items: [FigureEvidence]
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                HStack(alignment: .top, spacing: 12) {
                    Text(item.date).monospacedDigit().foregroundStyle(.secondary)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(item.headline).textSelection(.enabled)
                        if let url = item.source { Link(url.host ?? "Source", destination: url) }
                    }
                }
            }
        }
        .font(.caption)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
