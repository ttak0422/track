import Darwin
import Foundation
import TrackWebCore

@MainActor
final class TrackServerProcess: WebServerChild {
    private let executableURL: URL
    private let arguments: [String]
    private let environmentOverrides: [String: String]
    private let process = Process()
    private let lifecycle = ProcessLifecycleState()
    private var outputPipe: Pipe?

    var onOutput: ((String) -> Void)?
    var onTermination: ((Int32, String) -> Void)?

    // Foundation's Process.isRunning can be false while a child is SIGSTOP'd. The termination handler
    // is the authoritative reaping signal used by the supervisor before it releases the app.
    var isRunning: Bool { lifecycle.isActive }
    var forceTerminationHandler: @Sendable () -> Void {
        let lifecycle = self.lifecycle
        return { lifecycle.signal(SIGKILL) }
    }

    init(executableURL: URL, arguments: [String], environmentOverrides: [String: String]) {
        self.executableURL = executableURL
        self.arguments = arguments
        self.environmentOverrides = environmentOverrides
    }

    func launch(readyToken: String) throws {
        process.executableURL = executableURL
        process.arguments = arguments
        var environment = ProcessInfo.processInfo.environment
        for (key, value) in environmentOverrides {
            environment[key] = value
        }
        environment["TRACK_WEB_DESKTOP_PARENT_PID"] = String(ProcessInfo.processInfo.processIdentifier)
        environment["TRACK_WEB_DESKTOP_READY_TOKEN"] = readyToken
        process.environment = environment

        let pipe = Pipe()
        outputPipe = pipe
        process.standardOutput = pipe
        process.standardError = pipe
        pipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            guard !data.isEmpty else {
                handle.readabilityHandler = nil
                return
            }
            let text = String(decoding: data, as: UTF8.self)
            Task { @MainActor [weak self] in
                self?.onOutput?(text)
            }
        }
        let lifecycle = self.lifecycle
        process.terminationHandler = { [weak self] terminatedProcess in
            let status = terminatedProcess.terminationStatus
            let reason = terminatedProcess.terminationReason == .exit ? "normally" : "by signal"
            lifecycle.markTerminated()
            DispatchQueue.main.async { [weak self] in
                guard let self else { return }
                self.outputPipe?.fileHandleForReading.readabilityHandler = nil
                self.onTermination?(status, reason)
            }
        }

        do {
            try process.run()
            lifecycle.markLaunched(pid: process.processIdentifier)
            // The child inherited its copies of the write descriptor. Keeping this parent copy open
            // would prevent the asynchronous reader from seeing EOF after the child exits.
            pipe.fileHandleForWriting.closeFile()
        } catch {
            pipe.fileHandleForReading.readabilityHandler = nil
            try? pipe.fileHandleForReading.close()
            try? pipe.fileHandleForWriting.close()
            outputPipe = nil
            throw error
        }
    }

    func terminate() {
        lifecycle.signal(SIGTERM)
    }
}

private final class ProcessLifecycleState: @unchecked Sendable {
    private let lock = NSLock()
    private var launched = false
    private var terminated = false
    private var pid: Int32 = 0

    var isActive: Bool {
        lock.lock()
        defer { lock.unlock() }
        return launched && !terminated
    }

    func markLaunched(pid: Int32) {
        lock.lock()
        launched = true
        self.pid = pid
        lock.unlock()
    }

    func markTerminated() {
        lock.lock()
        terminated = true
        lock.unlock()
    }

    func signal(_ signal: Int32) {
        lock.lock()
        let ownedPID = launched && !terminated ? pid : 0
        lock.unlock()
        guard ownedPID > 1 else { return }
        // This PID is recorded from Process.run and remains owned until Foundation's termination
        // handler confirms and reaps that exact child. No port lookup or process-name search occurs.
        _ = kill(ownedPID, signal)
    }
}
