import Foundation

public enum WebNavigationDecision: Equatable {
    case allowInWebView
    case openInDefaultBrowser(URL)
    case cancel
}

public enum WebURLPolicy {
    /// Workspace and static-app origins stay exact and stable. A port collision is a startup failure,
    /// never a reason to follow whichever other loopback service answers first.
    public static func decide(
        for url: URL,
        isMainFrame: Bool,
        isUserInitiated: Bool,
        workspaceOrigin: URL,
        staticAppsOrigin: URL
    ) -> WebNavigationDecision {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              components.user == nil,
              components.password == nil,
              let scheme = components.scheme?.lowercased() else {
            return .cancel
        }

        if matchesOrigin(components, origin: workspaceOrigin) || matchesOrigin(components, origin: staticAppsOrigin) {
            return .allowInWebView
        }

        if scheme == "about", url.absoluteString == "about:blank", !isMainFrame {
            return .allowInWebView
        }

        // Remote HTML embeds (for example maps and video frames) stay in WebKit's web-content sandbox.
        // Only a top-level external destination is handed to the OS, and then only for an explicit
        // link gesture. Script redirects and unsupported schemes never reach NSWorkspace.
        if scheme == "http" || scheme == "https" {
            guard let host = components.host, !host.isEmpty else { return .cancel }
            if !isMainFrame {
                // Do not let user content silently probe/adopt an unrelated loopback service as an
                // iframe. The two exact Track origins were admitted above.
                return isLoopbackHost(host) ? .cancel : .allowInWebView
            }
            if isUserInitiated { return .openInDefaultBrowser(url) }
        }

        return .cancel
    }

    private static func matchesOrigin(_ candidate: URLComponents, origin: URL) -> Bool {
        guard let expected = URLComponents(url: origin, resolvingAgainstBaseURL: false),
              candidate.scheme?.lowercased() == expected.scheme?.lowercased(),
              candidate.host?.lowercased() == expected.host?.lowercased() else {
            return false
        }
        return effectivePort(candidate) == effectivePort(expected)
    }

    private static func effectivePort(_ components: URLComponents) -> Int? {
        if let port = components.port { return port }
        switch components.scheme?.lowercased() {
        case "http": return 80
        case "https": return 443
        default: return nil
        }
    }

    private static func isLoopbackHost(_ host: String) -> Bool {
        let normalized = host.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        return normalized == "localhost"
            || normalized.hasSuffix(".localhost")
            || normalized == "::1"
            || normalized.hasPrefix("127.")
    }
}
