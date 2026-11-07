import Foundation

/// The identity of the sources used to build this executable, not the current checkout.
public struct BuildInfo: Sendable {
    public let revision: String
    public let state: String
    public let builtAt: String

    public init(revision: String, state: String, builtAt: String) {
        self.revision = revision
        self.state = state
        self.builtAt = builtAt
    }

    // Direct SwiftPM builds have no build stamp. Never infer their identity from a nearby
    // checkout, a previous app bundle, or the template's fixed marketing version.
    #if !TRACK_BUILD_METADATA
    public static let current = BuildInfo(revision: "unknown", state: "unknown", builtAt: "unknown")
    #endif

    public var summary: String {
        guard revision != "unknown" else { return "Unknown source revision" }
        let suffix = state == "modified" ? " (modified)" : state == "clean" ? "" : " (state unknown)"
        return "Commit \(revision.prefix(12))\(suffix)"
    }

    public var details: String {
        "Track for macOS\nCommit: \(revision)\nSource state: \(state)\nBuilt (UTC): \(builtAt)"
    }
}
