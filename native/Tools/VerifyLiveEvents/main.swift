import Foundation

// Compile beside LiveEvents.swift: no network or UI session is required.
final class OfflineProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() { client?.urlProtocol(self, didFailWithError: URLError(.notConnectedToInternet)) }
    override func stopLoading() {}
}

@main
struct VerifyLiveEvents {
    @MainActor
    static func main() async throws {
        var parser = SSEParser()
        precondition(parser.push(": keepalive") == nil)
        precondition(parser.push("event: change") == nil)
        precondition(parser.push("data: {}") == "change")
        precondition(parser.push("event: data") == nil)
        precondition(parser.push("data: {}") == "data")
        precondition(!LiveEventPoller.isRefreshEvent("follow"))

        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [OfflineProtocol.self]
        let session = URLSession(configuration: config)
        var refreshes = 0
        let poller = LiveEventPoller(
            baseURL: URL(string: "http://127.0.0.1:1")!,
            onChange: { refreshes += 1 }, retryDelay: 60,
            pollInterval: 0.02, session: session
        )
        poller.start()
        poller.start()
        let deadline = ContinuousClock.now.advanced(by: .seconds(2))
        while refreshes < 2 && ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        precondition(refreshes >= 2, "offline SSE must still refresh data periodically")
        precondition(poller.lastChangeAt == nil, "polling must not create change notifications")
        poller.stop()
        let stoppedCount = refreshes
        try await Task.sleep(for: .milliseconds(80))
        precondition(refreshes == stoppedCount, "stop must cancel fallback work")
        await poller.refresh()
        precondition(refreshes == stoppedCount + 1, "activation can explicitly refresh")
        session.invalidateAndCancel()
        print("Live events: offline refresh, idempotent start, stop, activation, and SSE checks passed")
    }
}
