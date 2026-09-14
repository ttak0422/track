import Foundation
import TrackAPI

public enum ShareLinks {
    /// A vault reference is labeled as such; the local API base is never
    /// presented as a published note URL.
    public static func wikilink(id: TrackID, title: String) -> String {
        "[[\(id.raw)|\(title)]]"
    }

    public static func xIntentURL(title: String, publishedURL: URL?) -> URL? {
        guard let publishedURL, ["https", "http"].contains(publishedURL.scheme?.lowercased() ?? ""),
              publishedURL.host != nil else { return nil }
        var components = URLComponents(string: "https://x.com/intent/tweet")!
        components.queryItems = [URLQueryItem(name: "text", value: "\(title)\n\n\(publishedURL.absoluteString)")]
        return components.url
    }
}
