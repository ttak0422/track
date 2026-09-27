import Foundation

@MainActor
public protocol WebServerChild: AnyObject {
    var isRunning: Bool { get }
    var onOutput: ((String) -> Void)? { get set }
    var onTermination: ((Int32, String) -> Void)? { get set }
    var forceTerminationHandler: @Sendable () -> Void { get }

    func launch(readyToken: String) throws
    func terminate()
}

@MainActor
public final class WebServerSupervisor {
    public enum State: Equatable {
        case stopped
        case starting
        case ready
        case stopping
        case failed(String)
    }

    public typealias ChildFactory = @MainActor (_ readyToken: String) throws -> any WebServerChild
    public typealias HealthProbe = @Sendable (URL) async -> Bool

    public private(set) var state: State = .stopped {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let endpoint: URL
    private let healthProbe: HealthProbe
    private let childFactory: ChildFactory
    private let readinessTimeout: Duration
    private let pollInterval: Duration
    private let stopGrace: Duration
    private var child: (any WebServerChild)?
    private var readinessTask: Task<Void, Never>?
    private var generation = 0
    private var activeToken = ""
    private var output = ""
    private var sawReadyMarker = false
    private var stopping = false
    private var failedCleanup = false
    private var stopCompletions: [@MainActor () -> Void] = []

    public init(
        endpoint: URL,
        healthProbe: @escaping HealthProbe,
        readinessTimeout: Duration = .seconds(20),
        pollInterval: Duration = .milliseconds(100),
        stopGrace: Duration = .seconds(3),
        childFactory: @escaping ChildFactory
    ) {
        self.endpoint = endpoint
        self.healthProbe = healthProbe
        self.readinessTimeout = readinessTimeout
        self.pollInterval = pollInterval
        self.stopGrace = stopGrace
        self.childFactory = childFactory
    }

    public func start() {
        if child != nil {
            state = .failed("The previous Track web process is still stopping. Wait a moment, then retry.")
            return
        }
        readinessTask?.cancel()
        child?.onOutput = nil
        child?.onTermination = nil
        child = nil
        generation += 1
        let launchGeneration = generation
        let token = UUID().uuidString.lowercased()
        activeToken = token
        output = ""
        sawReadyMarker = false
        stopping = false
        failedCleanup = false
        state = .starting

        do {
            let newChild = try childFactory(token)
            child = newChild
            newChild.onOutput = { [weak self] text in
                self?.receivedOutput(text, generation: launchGeneration)
            }
            let childID = ObjectIdentifier(newChild)
            newChild.onTermination = { [weak self] status, reason in
                self?.childDidTerminate(childID: childID, status: status, reason: reason, generation: launchGeneration)
            }
            try newChild.launch(readyToken: token)
            readinessTask = Task { [weak self] in
                await self?.waitUntilReady(childID: childID, token: token, generation: launchGeneration)
            }
        } catch {
            child = nil
            state = .failed("Could not launch the bundled Go server: \(error.localizedDescription)")
        }
    }

    /// Initiates a nonblocking, bounded shutdown. The force signal is sent only through the handle
    /// returned by this supervisor's child factory, never by searching for a port owner or process name.
    public func stop(completion: @escaping @MainActor () -> Void = {}) {
        if stopping {
            stopCompletions.append(completion)
            return
        }
        readinessTask?.cancel()
        readinessTask = nil
        guard let child else {
            state = .stopped
            Task { @MainActor in completion() }
            return
        }
        stopCompletions.append(completion)
        stopping = true
        state = .stopping
        guard child.isRunning else {
            // A launched Foundation Process may report isRunning=false just before its termination
            // handler reaches the main actor. Keep ownership until that callback confirms the exit.
            return
        }
        child.terminate()
        forceTerminateAfterGrace(child)
    }

    private func waitUntilReady(childID: ObjectIdentifier, token: String, generation: Int) async {
        let deadline = ContinuousClock.now.advanced(by: readinessTimeout)
        while !Task.isCancelled && ContinuousClock.now < deadline {
            guard generation == self.generation,
                  let child,
                  ObjectIdentifier(child) == childID else { return }
            guard child.isRunning else { return }
            if sawReadyMarker, await healthProbe(endpoint) {
                guard !Task.isCancelled,
                      generation == self.generation,
                      child.isRunning,
                      activeToken == token else { return }
                readinessTask = nil
                state = .ready
                return
            }
            do {
                try await Task.sleep(for: pollInterval)
            } catch {
                return
            }
        }
        guard !Task.isCancelled, generation == self.generation else { return }
        let explanation = sawReadyMarker
            ? "The Go server started but its live API did not become ready at \(endpoint.absoluteString)."
            : "The Go server did not confirm ownership of its fixed loopback listeners. Another server is never adopted."
        guard let child = self.child,
              ObjectIdentifier(child) == childID else { return }
        fail(explanation, child: child)
    }

    private func receivedOutput(_ text: String, generation: Int) {
        guard generation == self.generation else { return }
        output += text
        if output.count > 8_000 {
            output = String(output.suffix(8_000))
        }
        if output.contains("TRACK_DESKTOP_READY:\(activeToken)") {
            sawReadyMarker = true
        }
    }

    private func childDidTerminate(childID: ObjectIdentifier, status: Int32, reason: String, generation: Int) {
        guard generation == self.generation,
              let child,
              ObjectIdentifier(child) == childID else { return }
        readinessTask?.cancel()
        readinessTask = nil
        self.child = nil
        if stopping {
            stopping = false
            state = .stopped
            let completions = stopCompletions
            stopCompletions.removeAll()
            completions.forEach { $0() }
            return
        }
        if failedCleanup {
            failedCleanup = false
            return
        }
        let detail = output.trimmingCharacters(in: .whitespacesAndNewlines)
        let message = "The bundled Go server exited \(reason) with status \(status) before or during startup."
        state = .failed(detail.isEmpty ? message : "\(message)\n\n\(detail)")
    }

    private func fail(_ explanation: String, child: any WebServerChild) {
        let detail = output.trimmingCharacters(in: .whitespacesAndNewlines)
        let message = detail.isEmpty ? explanation : "\(explanation)\n\n\(detail)"
        state = .failed(message)
        guard child.isRunning else { return }
        failedCleanup = true
        child.terminate()
        forceTerminateAfterGrace(child)
    }

    private func forceTerminateAfterGrace(_ child: any WebServerChild) {
        let grace = stopGrace
        let forceTerminate = child.forceTerminationHandler
        Task.detached {
            do {
                try await Task.sleep(for: grace)
            } catch {
                return
            }
            forceTerminate()
        }
    }
}
