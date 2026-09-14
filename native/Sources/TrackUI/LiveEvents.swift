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
// A dropped stream reconnects independently of the periodic refresh fallback.
// A missed event must not leave a view stale until another file changes.
// The stream is opened from a private ephemeral session so a stopped poller
// never tears down the shared session other requests run on.

@MainActor
@Observable
public final class LiveEventPoller {
    /// True while an SSE response is being read; false while reconnecting.
    public private(set) var isConnected = false
    /// Data for the lightweight activity banner/toast owned by the view.
    /// The current server emits `{}`, so the name is optional; the timestamp
    /// is still useful and is always recorded for refresh events.
    public private(set) var lastChangeAt: Date?
    public private(set) var lastChangedNoteName: String?

    private let baseURL: URL
    private let session: URLSession
    private let onChange: () async -> Void
    private let retryDelay: TimeInterval
    private var streamTask: Task<Void, Never>?
    private var pollTask: Task<Void, Never>?
    private var changeTask: Task<Void, Never>?
    private let pollInterval: TimeInterval
    private var refreshing = false

    /// - Parameters:
    ///   - baseURL: server origin (`http://127.0.0.1:<port>`); `/api/events`
    ///     is appended to it.
    ///   - onChange: called on the main actor for every `change`/`data` frame.
    ///   - retryDelay: pause before reconnecting after a failure or an ended stream.
    ///   - pollInterval: data refresh fallback, independent of SSE connectivity.
    public init(
        baseURL: URL,
        onChange: @escaping () async -> Void,
        retryDelay: TimeInterval = 30,
        pollInterval: TimeInterval = 30,
        session: URLSession? = nil
    ) {
        self.baseURL = baseURL
        self.session = session ?? Self.makeSession()
        self.onChange = onChange
        self.retryDelay = retryDelay
        self.pollInterval = pollInterval
    }

    /// Opens the stream (or does nothing if one is already running).
    public func start() {
        guard streamTask == nil else { return }
        // The loop captures self weakly, so releasing the poller without
        // stop() ends reconnecting once the in-flight read returns.
        streamTask = Task { [weak self] in
            await self?.run()
        }
        let interval = pollInterval
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                do { try await Task.sleep(for: .seconds(interval)) } catch { return }
                guard let self, !Task.isCancelled else { return }
                await self.refresh()
            }
        }
    }

    /// Closes the stream and stops reconnecting. Safe to call twice; start()
    /// after stop() begins a fresh loop.
    public func stop() {
        streamTask?.cancel()
        streamTask = nil
        pollTask?.cancel()
        pollTask = nil
        changeTask?.cancel()
        changeTask = nil
        isConnected = false
    }

    /// Refresh on a timer or when the app becomes active without inventing a
    /// user-visible "changed" notification. Only actual SSE events announce a change.
    public func refresh() async {
        guard !Task.isCancelled, !refreshing else { return }
        refreshing = true
        defer { refreshing = false }
        await onChange()
    }

    private func scheduleChange(payload: String?) {
        changeTask?.cancel()
        changeTask = Task { [weak self] in
            do { try await Task.sleep(for: .milliseconds(150)) } catch { return }
            guard let self, !Task.isCancelled else { return }
            self.recordChange(payload: payload)
            await self.refresh()
        }
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
            guard !Task.isCancelled else { return }
            isConnected = true
            await refresh()
            defer { isConnected = false }
            var parser = SSEParser()
            for try await line in bytes.lines {
                if Task.isCancelled { break }
                // push returns the completed frame's event name at the line
                // that closes it; nil means the line belongs to a frame that
                // is still open (or a keep-alive comment).
                if let event = parser.push(line), Self.isRefreshEvent(event) {
                    scheduleChange(payload: parser.lastData)
                }
            }
        } catch {
            // Refused, timed out, or cut short. run() reconnects after the
            // delay; stop() surfaces here through cancellation.
        }
    }

    private func recordChange(payload: String?) {
        lastChangeAt = Date()
        lastChangedNoteName = nil
        guard let payload, let data = payload.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return }
        for key in ["note_name", "note", "name", "title", "path"] {
            if let value = object[key] as? String, !value.isEmpty {
                lastChangedNoteName = value
                return
            }
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
    private(set) var lastData: String?

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
            if line.hasPrefix("data:") {
                lastData = String(line.dropFirst("data:".count)).trimmingCharacters(in: .whitespaces)
            }
            return completed
        }
        return nil
    }
}
