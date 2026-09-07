import Foundation
import Observation

// The app-wide "vault changed" signal. LiveEventPoller posts it on every
// `change`/`data` frame; views showing vault data observe it to reload — the
// native analogue of the web's queryClient.invalidateQueries.
public extension Notification.Name {
    static let trackVaultChanged = Notification.Name("track.vaultChanged")
}

// LiveEventPoller — the native /api/events SSE client, mirroring the web's
// useLiveEvents hook (web/src/hooks/useLiveEvents.ts) against the workspace
// change stream (internal/track/webui/watch.go handleEvents).
//
// The server writes named Server-Sent Events and keeps idle connections alive
// with ":" comment frames. Two names mean "something you drew changed":
// `change` after a vault reindex and `data` after a file under a vault's
// data/ directory changed. Both make the poller call `onChange`, which the
// app uses to invalidate whatever data it is showing — the native analogue of
// queryClient.invalidateQueries.
//
// A dropped or refused stream is retried after a delay (30 s by default): the
// connection *is* the web's live channel, so the delay is the native stand-in
// for the 30 s poll backstop the parity inventory keeps beside EventSource.
// The stream is opened from a private ephemeral session so a stopped poller
// never tears down the shared session other requests run on.

@MainActor
@Observable
public final class LiveEventPoller {
    /// True while an SSE response is being read; false while reconnecting.
    public private(set) var isConnected = false

    private let baseURL: URL
    private let session: URLSession
    private let onChange: () async -> Void
    private let retryDelay: TimeInterval
    private var streamTask: Task<Void, Never>?

    /// - Parameters:
    ///   - baseURL: server origin (`http://127.0.0.1:<port>`); `/api/events`
    ///     is appended to it.
    ///   - onChange: called on the main actor for every `change`/`data` frame.
    ///   - retryDelay: pause before reconnecting after a failure or an ended
    ///     stream; 30 s by default (the poll backstop).
    public init(
        baseURL: URL,
        onChange: @escaping () async -> Void,
        retryDelay: TimeInterval = 30
    ) {
        self.baseURL = baseURL
        self.session = Self.makeSession()
        self.onChange = onChange
        self.retryDelay = retryDelay
    }

    /// Opens the stream (or does nothing if one is already running).
    public func start() {
        guard streamTask == nil else { return }
        // The loop captures self weakly, so releasing the poller without
        // stop() ends reconnecting once the in-flight read returns.
        streamTask = Task { [weak self] in
            await self?.run()
        }
    }

    /// Closes the stream and stops reconnecting. Safe to call twice; start()
    /// after stop() begins a fresh loop.
    public func stop() {
        streamTask?.cancel()
        streamTask = nil
    }

    // MARK: - Run loop

    private func run() async {
        while !Task.isCancelled {
            await openStream()
            if Task.isCancelled { break }
            try? await Task.sleep(for: .seconds(retryDelay))
        }
    }

    private func openStream() async {
        var request = URLRequest(url: baseURL.appendingPathComponent("api/events"))
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        do {
            let (bytes, response) = try await session.bytes(for: request)
            guard (response as? HTTPURLResponse)?.statusCode == 200 else { return }
            isConnected = true
            defer { isConnected = false }
            var parser = SSEParser()
            for try await line in bytes.lines {
                if Task.isCancelled { break }
                // push returns the completed frame's event name at the line
                // that closes it; nil means the line belongs to a frame that
                // is still open (or a keep-alive comment).
                if let event = parser.push(line), Self.isRefreshEvent(event) {
                    await onChange()
                }
            }
        } catch {
            // Refused, timed out, or cut short. run() reconnects after the
            // delay; stop() surfaces here through cancellation.
        }
    }

    /// The frames that mean "data you drew changed" — the pair useLiveEvents
    /// listens for (`change` → invalidate notes/search/calendar/graph/tasks,
    /// `data` → invalidate embedded charts).
    static func isRefreshEvent(_ name: String) -> Bool {
        name == "change" || name == "data"
    }

    private static func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 60
        // One stream at a time; reconnects must not pile up.
        configuration.httpMaximumConnectionsPerHost = 1
        return URLSession(configuration: configuration)
    }
}

/// Incremental SSE frame scanner. Feed it the body line by line; it returns
/// the completed frame's event name at the line that closes the frame. Only
/// the "event:" field is tracked — watch.go writes
/// `event: <name>\ndata: {}\n\n`, and the payload is always "{}" for the
/// frames the app acts on.
///
/// The scanner does not wait for the spec's blank separator line, because
/// `URLSession.AsyncBytes.lines` omits empty lines: a frame here is closed by
/// its `data:` line. ":" comment frames (the server's keep-alive pings) carry
/// no event field and read as nil.
struct SSEParser {
    private var pendingEvent: String?

    mutating func push(_ line: String) -> String? {
        if line.hasPrefix(":") {
            return nil // comment / keep-alive
        }
        if line.hasPrefix("event:") {
            pendingEvent = String(line.dropFirst("event:".count))
                .trimmingCharacters(in: .whitespaces)
            return nil
        }
        // The `data:` line closes a frame. An empty line (kept by readers that
        // preserve separators) closes an event the server sent without data.
        if line.hasPrefix("data:") || line.isEmpty {
            let completed = pendingEvent
            pendingEvent = nil
            return completed
        }
        return nil
    }
}
