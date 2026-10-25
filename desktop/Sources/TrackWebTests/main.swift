import Darwin
import Foundation
import TrackWebCore

@main
@MainActor
struct TrackWebTestRunner {
    private static var failures = 0

    static func main() async {
        await run("registered vault launch arguments") { try testRegisteredVaultArguments() }
        await run("explicit vault-path launch argument") { try testVaultPathArgument() }
        await run("unsafe or conflicting launch arguments") { try testInvalidLaunchArguments() }
        await run("exact local origins") { try testExactLocalOrigins() }
        await run("user-initiated external navigation") { try testExternalNavigationRequiresGesture() }
        await run("sandboxed external frames") { try testRemoteFramesStayInWebKit() }
        await run("same-document anchor protection") { try testSameDocumentAnchors() }
        await run("unsupported URL schemes") { try testUnsupportedSchemes() }
        await run("startup failure and retry") { try await testLaunchFailureCanRetry() }
        await run("readiness does not adopt another server") { try await testExternalServerCannotSatisfyReadiness() }
        await run("owned-child readiness and normal shutdown") { try await testReadyChildStopsNormally() }
        await run("unexpected child exit becomes a visible failure") { try await testUnexpectedChildExit() }
        await run("bounded force-stop of the owned child") { try await testHungChildIsForceStopped() }
        await run("startup cancellation") { try await testStartupCancellation() }

        await run("native speech lifecycle without audio or OS permission requests") { try await NativeSpeechTests.run() }

        if failures == 0 {
            print("All 15 Track logic regression tests passed.")
        } else {
            fputs("\(failures) Track logic regression test(s) failed.\n", stderr)
            exit(EXIT_FAILURE)
        }
    }

    private static func run(_ name: String, test: () async throws -> Void) async {
        do {
            try await test()
            print("PASS \(name)")
        } catch {
            failures += 1
            fputs("FAIL \(name): \(error.localizedDescription)\n", stderr)
        }
    }

    private static func expect(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        if !condition() { throw TestFailure(message) }
    }

    private static func testRegisteredVaultArguments() throws {
        let options = try LaunchOptions(arguments: ["--vault", "work"])
        try expect(options.vaultName == "work", "registry name was not parsed")
        try expect(options.trackArguments == ["--vault", "work", "web", "--addr", "127.0.0.1:18765"], "Go arguments changed")
        try expect(options.environmentOverrides.isEmpty, "registered selection must use the CLI --vault flag")
    }

    private static func testVaultPathArgument() throws {
        let options = try LaunchOptions(arguments: ["--vault-path", "/tmp/track-test"])
        try expect(options.vaultPath == "/tmp/track-test", "absolute path was not parsed")
        try expect(options.environmentOverrides["TRACK_VAULT"] == "/tmp/track-test", "path did not scope the child process")
        try expect(options.trackArguments.first == "web", "path selection unexpectedly changes CLI flags")
    }

    private static func testInvalidLaunchArguments() throws {
        try expectThrows { _ = try LaunchOptions(arguments: ["--vault", "work", "--vault-path", "/tmp/work"]) }
        try expectThrows { _ = try LaunchOptions(arguments: ["--vault-path", "relative/path"]) }
        try expectThrows { _ = try LaunchOptions(arguments: ["--unrestricted-shell"]) }
    }

    private static func testSameDocumentAnchors() throws {
        let source = URL(string: "http://127.0.0.1:18765/notes/100?q=one#first")!
        func anchor(_ target: String, link: Bool = true) -> Bool {
            WebURLPolicy.isSameDocumentAnchor(from: source, to: URL(string: target)!, isLink: link)
        }
        try expect(anchor("http://127.0.0.1:18765/notes/100?q=one#second"), "Contents anchor was treated as unload")
        try expect(anchor(source.absoluteString), "repeated anchor should remain in this document")
        try expect(!anchor(source.absoluteString, link: false), "reload must not bypass dirty protection")
        try expect(!anchor("http://127.0.0.1:18765/notes/101?q=one#second"), "different note bypassed dirty protection")
        try expect(!anchor("http://127.0.0.1:18765/notes/100?q=two#second"), "query navigation bypassed dirty protection")
        try expect(!anchor("http://127.0.0.1:18766/notes/100?q=one#second"), "other origin bypassed dirty protection")
        try expect(!anchor("http://127.0.0.1:18765/notes/100?q=one"), "fragmentless link can reload a document")
    }

    private static func testExactLocalOrigins() throws {
        let workspace = URL(string: "http://127.0.0.1:18765")!
        let apps = URL(string: "http://127.0.0.1:18766")!
        try expect(decide("http://127.0.0.1:18765/notes/1", workspace, apps) == .allowInWebView, "workspace origin was rejected")
        try expect(decide("http://127.0.0.1:18766/__track/apps/launch/demo/", workspace, apps) == .allowInWebView, "static-app origin was rejected")
        try expect(
            decide("http://localhost:18765/", workspace, apps, gesture: true) == .openInDefaultBrowser(URL(string: "http://localhost:18765/")!),
            "localhost alias must not be treated as the app's exact origin"
        )
    }

    private static func testExternalNavigationRequiresGesture() throws {
        let workspace = URL(string: "http://127.0.0.1:18765")!
        let apps = URL(string: "http://127.0.0.1:18766")!
        try expect(decide("https://example.com/note", workspace, apps) == .cancel, "scripted external navigation was not rejected")
        try expect(
            decide("https://example.com/note", workspace, apps, gesture: true) == .openInDefaultBrowser(URL(string: "https://example.com/note")!),
            "direct external click did not route to the default browser"
        )
        try expect(decide("http://127.0.0.1:8765/", workspace, apps) == .cancel, "another local server was accepted")
    }

    private static func testRemoteFramesStayInWebKit() throws {
        let workspace = URL(string: "http://127.0.0.1:18765")!
        let apps = URL(string: "http://127.0.0.1:18766")!
        try expect(decide("https://www.youtube.com/embed/example", workspace, apps, main: false) == .allowInWebView, "remote iframe was rejected")
        try expect(decide("http://127.0.0.1:9000/user-app/", workspace, apps, main: false) == .cancel, "unrelated loopback service was allowed as an iframe")
    }

    private static func testUnsupportedSchemes() throws {
        let workspace = URL(string: "http://127.0.0.1:18765")!
        let apps = URL(string: "http://127.0.0.1:18766")!
        for value in [
            "file:///etc/passwd",
            "javascript:alert(1)",
            "mailto:hello@example.com",
            "custom://launch/something",
            "https://user:password@example.com/",
        ] {
            try expect(decide(value, workspace, apps, gesture: true) == .cancel, "unsafe scheme escaped: \(value)")
        }
        try expect(decide("about:blank", workspace, apps, main: false) == .allowInWebView, "blank iframe was rejected")
        try expect(decide("about:blank", workspace, apps) == .cancel, "top-level about:blank was accepted")
    }

    private static func decide(
        _ value: String,
        _ workspace: URL,
        _ apps: URL,
        main: Bool = true,
        gesture: Bool = false
    ) -> WebNavigationDecision {
        WebURLPolicy.decide(
            for: URL(string: value)!,
            isMainFrame: main,
            isUserInitiated: gesture,
            workspaceOrigin: workspace,
            staticAppsOrigin: apps
        )
    }

    private static func testLaunchFailureCanRetry() async throws {
        let first = FakeChild(launchError: FakeError.launchFailed)
        let retry = FakeChild()
        var attempt = 0
        let supervisor = makeSupervisor(factory: { _ in
            defer { attempt += 1 }
            return attempt == 0 ? first : retry
        })

        supervisor.start()
        try expect(isFailed(supervisor.state, containing: "launch"), "launch error was not visible")
        supervisor.start()
        try expect(supervisor.state == .starting && retry.isRunning, "retry did not start a new child")
        supervisor.stop()
        try await waitForState(supervisor, .stopped)
    }

    private static func testUnexpectedChildExit() async throws {
        let child = FakeChild()
        let supervisor = makeSupervisor(factory: { _ in child }, healthProbe: { _ in true })
        supervisor.start()
        child.emit("TRACK_DESKTOP_READY:\(child.readyToken ?? "")\n")
        try await waitForState(supervisor, .ready)
        child.exit(status: 23)

        try await waitUntil { if case .failed = supervisor.state { return true }; return false }
        try expect(isFailed(supervisor.state, containing: "status 23"), "unexpected child exit was not reported")
    }

    private static func testExternalServerCannotSatisfyReadiness() async throws {
        let child = FakeChild()
        let supervisor = makeSupervisor(
            factory: { _ in child },
            readinessTimeout: .milliseconds(60),
            pollInterval: .milliseconds(5),
            healthProbe: { _ in true }
        )

        supervisor.start()
        try await waitUntil { if case .failed = supervisor.state { return true }; return false }
        try expect(isFailed(supervisor.state, containing: "Another server is never adopted"), "HTTP alone was accepted as child readiness")
        try expect(child.terminateCalled, "timed-out child was not stopped")
    }

    private static func testReadyChildStopsNormally() async throws {
        let child = FakeChild()
        let supervisor = makeSupervisor(factory: { _ in child }, healthProbe: { _ in true })
        supervisor.start()
        child.emit("TRACK_DESKTOP_READY:\(child.readyToken ?? "")\n")
        try await waitForState(supervisor, .ready)

        var childExitConfirmed = false
        supervisor.stop { childExitConfirmed = true }
        try await waitForState(supervisor, .stopped)
        try expect(child.terminateCalled, "supervisor did not terminate its child")
        try expect(!child.forceTerminateCalled, "normal child shutdown escalated to force-kill")
        try expect(childExitConfirmed, "stop completion ran before child exit confirmation")
    }

    private static func testHungChildIsForceStopped() async throws {
        let child = FakeChild(ignoresTerminate: true)
        let supervisor = makeSupervisor(factory: { _ in child }, stopGrace: .milliseconds(20))
        supervisor.start()
        var childExitConfirmed = false
        supervisor.stop { childExitConfirmed = true }
        try expect(!childExitConfirmed, "stop completion ran before force-stop of a suspended child")
        try await waitUntil { child.forceTerminateCalled && childExitConfirmed }
        try expect(child.terminateCalled, "graceful termination was skipped")
        try expect(child.forceTerminateCalled, "owned process was not force-stopped after the deadline")
    }

    private static func testStartupCancellation() async throws {
        let child = FakeChild()
        let supervisor = makeSupervisor(factory: { _ in child }, healthProbe: { _ in true })
        supervisor.start()
        supervisor.stop()
        try await waitForState(supervisor, .stopped)
        try expect(supervisor.state != .ready, "cancelled startup was reported as ready")
        try expect(child.terminateCalled, "startup cancellation did not stop the child")
    }

    private static func makeSupervisor(
        factory: @escaping WebServerSupervisor.ChildFactory,
        readinessTimeout: Duration = .seconds(1),
        pollInterval: Duration = .milliseconds(5),
        stopGrace: Duration = .milliseconds(40),
        healthProbe: @escaping WebServerSupervisor.HealthProbe = { _ in false }
    ) -> WebServerSupervisor {
        WebServerSupervisor(
            endpoint: URL(string: "http://127.0.0.1:18765/api/vaults")!,
            healthProbe: healthProbe,
            readinessTimeout: readinessTimeout,
            pollInterval: pollInterval,
            stopGrace: stopGrace,
            childFactory: factory
        )
    }

    private static func waitForState(_ supervisor: WebServerSupervisor, _ expected: WebServerSupervisor.State) async throws {
        try await waitUntil { supervisor.state == expected }
    }

    private static func waitUntil(condition: @escaping @MainActor () -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(2))
        while ContinuousClock.now < deadline {
            if condition() { return }
            try? await Task.sleep(for: .milliseconds(5))
        }
        throw TestFailure("asynchronous state did not arrive before its deadline")
    }

    private static func isFailed(_ state: WebServerSupervisor.State, containing text: String) -> Bool {
        guard case .failed(let message) = state else { return false }
        return message.localizedCaseInsensitiveContains(text)
    }

    private static func expectThrows(_ body: () throws -> Void) throws {
        var didThrow = false
        do {
            try body()
        } catch {
            didThrow = true
        }
        try expect(didThrow, "expected an invalid launch argument to throw")
    }
}

@MainActor
private final class FakeChild: WebServerChild {
    private let launchError: Error?
    private let ignoresTerminate: Bool
    private(set) var readyToken: String?
    private(set) var isRunning = false
    private(set) var terminateCalled = false
    private(set) var forceTerminateCalled = false
    var onOutput: ((String) -> Void)?
    var onTermination: ((Int32, String) -> Void)?
    var forceTerminationHandler: @Sendable () -> Void {
        { [weak self] in
            Task { @MainActor [weak self] in self?.forceTerminate() }
        }
    }

    init(launchError: Error? = nil, ignoresTerminate: Bool = false) {
        self.launchError = launchError
        self.ignoresTerminate = ignoresTerminate
    }

    func launch(readyToken: String) throws {
        if let launchError { throw launchError }
        self.readyToken = readyToken
        isRunning = true
    }

    func terminate() {
        terminateCalled = true
        if !ignoresTerminate { finish(status: 0, reason: "exit") }
    }

    func forceTerminate() {
        forceTerminateCalled = true
        finish(status: 9, reason: "signal")
    }

    func emit(_ text: String) {
        onOutput?(text)
    }

    func exit(status: Int32) {
        finish(status: status, reason: "exit")
    }

    private func finish(status: Int32, reason: String) {
        guard isRunning else { return }
        isRunning = false
        onTermination?(status, reason)
    }
}

private struct TestFailure: Error, LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

private enum FakeError: Error, LocalizedError {
    case launchFailed
    var errorDescription: String? { "fixture launch error" }
}


@MainActor
enum NativeSpeechTests {
    private struct Failure: Error { let message: String }
    private static func expect(_ value: @autoclosure () -> Bool, _ message: String) throws {
        if !value() { throw Failure(message: message) }
    }

    static func run() async throws {
        let driver = FakeSpeechDriver()
        let session = NativeSpeechSession(driver: driver, captureLimit: .seconds(60), finishLimit: .milliseconds(10))
        var events: [(String, String, String)] = []
        session.emit = { events.append(($0, $1, $2)) }
        await session.start(id: "first", locale: "ja-JP").value
        try expect(driver.locales == ["ja-JP"] && driver.capturing, "Japanese capture did not start")
        let first = driver.receive!
        first(.partial("途中"))
        first(.partial("途中の文"))
        try expect(events.filter { $0.1 == "partial" }.count == 2, "partial hypotheses were lost")
        await session.start(id: "busy", locale: "ja-JP").value
        try expect(driver.starts == 1 && driver.capturing, "concurrent start replaced the live microphone")
        session.stop(id: "wrong")
        try expect(driver.capturing, "another owner stopped capture")
        session.stop(id: "first")
        session.stop(id: "first")
        try expect(!driver.capturing && driver.finishes == 1, "stop did not release capture once")
        first(.partial("late interim"))
        first(.final("確定"))
        first(.final("確定"))
        try expect(events.filter { $0.1 == "final" }.count == 1, "final transcript duplicated")
        try expect(events.last?.1 == "ended", "final did not end the task")
        await session.start(id: "restart", locale: "ja-JP").value
        first(.final("stale"))
        try expect(driver.capturing && events.last?.1 == "started", "old callback affected restarted task")
        session.cancel()
        try expect(!driver.capturing, "closing/unmount cancellation left capture running")
        let count = events.count
        driver.receive?(.partial("cancelled"))
        try expect(events.count == count, "cancelled callback was delivered")
        print("PASS speech start/stop/final deduplication/restart/concurrent ownership/cancel")

        for reason in ["Microphone denied", "Microphone restricted", "Speech denied", "Speech restricted"] {
            driver.permissionError = reason
            let starts = driver.starts
            await session.start(id: reason, locale: "ja-JP").value
            try expect(driver.starts == starts && !driver.capturing, "permission failure started capture")
            try expect(events.suffix(2).first?.2 == reason && events.last?.1 == "ended", "permission error was not surfaced")
        }
        driver.permissionError = nil
        driver.startError = NSError(domain: "Fixture", code: 1, userInfo: [NSLocalizedDescriptionKey: "Unavailable Japanese recognizer"])
        await session.start(id: "unavailable", locale: "ja-JP").value
        try expect(!driver.capturing && events.suffix(2).first?.2 == "Unavailable Japanese recognizer", "failed start left resources")
        driver.startError = nil
        await session.start(id: "runtime-error", locale: "ja-JP").value
        driver.receive?(.failure("Network unavailable"))
        try expect(!driver.capturing && events.last?.1 == "ended", "runtime error left capture active")
        print("PASS speech permission denied/restricted/unavailable/start and runtime failures")

        await session.start(id: "timeout", locale: "ja-JP").value
        session.stop(id: "timeout")
        try await Task.sleep(for: .milliseconds(40))
        try expect(!driver.capturing && events.last?.1 == "ended" && events.suffix(2).first?.1 == "error", "missing final hung the task")
        let timed = NativeSpeechSession(driver: driver, captureLimit: .milliseconds(10), finishLimit: .milliseconds(10))
        await timed.start(id: "limit", locale: "ja-JP").value
        try await Task.sleep(for: .milliseconds(40))
        try expect(!driver.capturing, "capture duration limit left the microphone active")
        print("PASS speech bounded finishing and maximum capture duration")

        let pendingDriver = FakeSpeechDriver()
        pendingDriver.deferPermission = true
        let pending = NativeSpeechSession(driver: pendingDriver)
        let start = pending.start(id: "pending", locale: "ja-JP")
        while pendingDriver.permissionReply == nil { await Task.yield() }
        pending.stop(id: "pending")
        pendingDriver.permissionReply?.resume(returning: nil)
        await start.value
        try expect(pendingDriver.starts == 0, "late permission response started a stopped session")
        print("PASS speech stop while permissions pending")
    }
}

@MainActor
private final class FakeSpeechDriver: SpeechDriver {
    var permissionError: String?
    var startError: Error?
    var deferPermission = false
    var permissionReply: CheckedContinuation<String?, Never>?
    var receive: (@MainActor (SpeechUpdate) -> Void)?
    var locales: [String] = []
    var capturing = false
    var starts = 0
    var finishes = 0
    func authorize() async -> String? {
        if deferPermission { return await withCheckedContinuation { permissionReply = $0 } }
        return permissionError
    }
    func start(locale: String, receive: @escaping @MainActor (SpeechUpdate) -> Void) throws {
        self.receive = receive
        starts += 1
        locales.append(locale)
        capturing = true
        if let startError { throw startError }
    }
    func stopAudio() { capturing = false }
    func finishAudio() { finishes += 1 }
    func cancel() { capturing = false }
}
