import Foundation

// Owns the bundled `track` sidecar: picks a free loopback port, launches
// `track web --addr 127.0.0.1:<port>`, publishes the client, and stops the
// server on teardown. The Go engine stays the source of truth; this is only
// a process wrapper around `track web` / `track web stop`.

@MainActor
@Observable
public final class TrackProcess {
    public enum State: Sendable {
        case stopped
        case starting
        case ready(TrackClient)
        case failed(String)
    }

    public private(set) var state: State = .stopped

    private let executableURL: URL
    private let vaultPath: String?
    private var task: Process?

    /// - Parameter executableURL: bundled `track` binary (e.g. in Resources).
    /// - Parameter vaultPath: served vault; nil = the binary's default vault.
    ///   Unregistered checkouts work without setup (agent-workflows.md: `TRACK_VAULT=<path>` prefix).
    public init(executableURL: URL, vaultPath: String? = nil) {
        self.executableURL = executableURL
        self.vaultPath = vaultPath
    }

    public func start() {
        guard case .stopped = state else { return }
        state = .starting
        do {
            let port = try Self.freePort()
            let proc = Process()
            proc.executableURL = executableURL
            proc.arguments = ["web", "--addr", "127.0.0.1:\(port)"]
            if let vaultPath {
                var env = ProcessInfo.processInfo.environment
                env["TRACK_VAULT"] = vaultPath
                proc.environment = env
            }
            // Silence the server's stderr URL print; readiness is probed below.
            proc.standardOutput = FileHandle.nullDevice
            proc.standardError = FileHandle.nullDevice
            try proc.run()
            task = proc
            let client = TrackClient(baseURL: URL(string: "http://127.0.0.1:\(port)")!)
            Task { await self.waitReady(client: client, proc: proc) }
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    public func stop() {
        task?.terminate()
        task = nil
        state = .stopped
    }

    // MARK: - Private

    private func waitReady(client: TrackClient, proc: Process) async {
        // `track web` binds fast; poll search with an empty query instead of
        // inventing a health endpoint.
        for _ in 0..<50 {
            if !proc.isRunning {
                state = .failed("track web exited during startup")
                return
            }
            do {
                _ = try await client.searchNotes(query: "__ready__", limit: 1)
                state = .ready(client)
                return
            } catch {
                try? await Task.sleep(for: .milliseconds(100))
            }
        }
        state = .failed("track web did not answer on its port")
    }

    private static func freePort() throws -> Int {
        // Bind port 0 and read back the ephemeral port, then release it.
        // A race remains (another process could grab it); waitReady retries
        // only our own server, so a collision surfaces as failed, not silent.
        let sock = socket(AF_INET, SOCK_STREAM, 0)
        guard sock >= 0 else { throw POSIXError(.EIO) }
        defer { close(sock) }
        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = 0
        addr.sin_addr.s_addr = INADDR_ANY
        let bound = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(sock, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        guard bound == 0 else { throw POSIXError(.EADDRINUSE) }
        var out = sockaddr_in()
        var len = socklen_t(MemoryLayout<sockaddr_in>.size)
        let named = withUnsafeMutablePointer(to: &out) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                getsockname(sock, $0, &len)
            }
        }
        guard named == 0 else { throw POSIXError(.EIO) }
        // sin_port is network byte order.
        return Int(CFSwapInt16BigToHost(out.sin_port))
    }
}
