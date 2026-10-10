import AppKit
import Foundation
import TrackWebCore
import WebKit

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, NSMenuItemValidation {
    private let application: NSApplication
    private let workspaceOrigin: URL
    private let websiteDataStore: WKWebsiteDataStore?
    private let restartTestMode: Bool
    private var restartFixture: RestartFixture?
    private let smokeMode: Bool
    private let recoveryTestMode: Bool
    private let shutdownTestMode: Bool
    private let searchTestMode: Bool
    private var terminationApproved = false
    private var terminationPending = false
    private var terminationWasConfirmed = false
    private var smokeFinished = false
    private var searchTestActionCounts: [Int: Int] = [:]
    private var supervisor: WebServerSupervisor?
    private var webView: WKWebView?
    private var coordinator: WebViewCoordinator?
    private var textFinder: WebViewFindController?
    private var findBarContainerView: SearchFindBarContainerView?
    private var statusView: NSView?
    private var progressIndicator: NSProgressIndicator?
    private var statusTitle: NSTextField?
    private var statusDetail: NSTextField?
    private var retryButton: NSButton?
    private var launchFailure: String?
    private var launchOptions: LaunchOptions?
    private var smokeResultURL: URL?
    private var recoveryPhase: RecoveryTestPhase = .waitingForInitialPage
    private var recoveryDraftToken: String?
    private var initialSmokeReport: [String: Any]?
    private var shutdownTestTriggerURL: URL?
    private var shutdownTestResultURL: URL?
    private var shutdownTestTask: Task<Void, Never>?
    private var shutdownTestStartUptime: TimeInterval?

    init(
        application: NSApplication = .shared,
        arguments: [String] = ProcessInfo.processInfo.arguments,
        workspaceOrigin: URL = URL(string: "http://127.0.0.1:18765")!,
        websiteDataStore: WKWebsiteDataStore? = nil
    ) {
        self.application = application
        self.workspaceOrigin = workspaceOrigin
        self.websiteDataStore = websiteDataStore
        let appArguments = Array(arguments.dropFirst())
        restartTestMode = appArguments.contains("--restart-test")
        recoveryTestMode = appArguments.contains("--recovery-test")
        shutdownTestMode = appArguments.contains("--shutdown-test")
        searchTestMode = appArguments.contains("--search-test")
        smokeMode = appArguments.contains("--smoke-test") || recoveryTestMode || searchTestMode || restartTestMode
        super.init()
        smokeResultURL = ProcessInfo.processInfo.environment["TRACK_WEB_SMOKE_RESULT"].map(URL.init(fileURLWithPath:))
        shutdownTestTriggerURL = ProcessInfo.processInfo.environment["TRACK_WEB_SHUTDOWN_TEST_TRIGGER"].map(URL.init(fileURLWithPath:))
        shutdownTestResultURL = ProcessInfo.processInfo.environment["TRACK_WEB_SHUTDOWN_TEST_RESULT"].map(URL.init(fileURLWithPath:))

        do {
            if restartTestMode {
                guard appArguments == ["--restart-test"] else {
                    throw CocoaError(.validationMissingMandatoryProperty)
                }
                restartFixture = try RestartFixture()
            }
            let testArguments = ["--restart-test", "--smoke-test", "--recovery-test", "--shutdown-test", "--search-test"]
            let userArguments = appArguments.filter { !testArguments.contains($0) && !$0.hasPrefix("-psn_") }
            launchOptions = try LaunchOptions(arguments: userArguments)
        } catch {
            launchFailure = error.localizedDescription
        }
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Fail before creating any WebView or launching a server when fixture validation fails.
        if restartTestMode {
            guard let fixture = restartFixture, launchFailure == nil else {
                fputs("Invalid restart fixture: \(launchFailure ?? "missing fixture")\n", stderr)
                terminationApproved = true
                application.terminate(nil)
                return
            }
            Task { @MainActor in
                do {
                    // Initialize WebKit on the main thread before its asynchronous store enumeration.
                    // This ephemeral object never opens the default persistent store.
                    _ = WKWebsiteDataStore.nonPersistent()
                    let identifiers = await WKWebsiteDataStore.allDataStoreIdentifiers
                    let receipt = fixture.root.appendingPathComponent("store-owned")
                    if fixture.phase == "seed" {
                        guard !identifiers.contains(fixture.identifier) else {
                            throw CocoaError(.fileWriteFileExists)
                        }
                        try Data(fixture.identifier.uuidString.utf8).write(to: receipt, options: .withoutOverwriting)
                    } else {
                        guard try String(contentsOf: receipt, encoding: .utf8) == fixture.identifier.uuidString else {
                            throw CocoaError(.validationMissingMandatoryProperty)
                        }
                    }
                    if fixture.phase == "cleanup" {
                        try await WKWebsiteDataStore.remove(forIdentifier: fixture.identifier)
                        try Data("cleaned".utf8).write(to: fixture.root.appendingPathComponent("cleaned"))
                        self.terminationApproved = true
                        self.application.terminate(nil)
                        return
                    }
                    if fixture.phase == "restore" && !identifiers.contains(fixture.identifier) {
                        throw CocoaError(.fileNoSuchFile)
                    }
                    self.launchWorkspace()
                } catch {
                    self.finishSmoke(["ok": false, "error": "Restart fixture: \(error)"])
                }
            }
            return
        }
        launchWorkspace()
    }

    private func launchWorkspace() {
        application.setActivationPolicy(.regular)
        installMenu()
        createWindow()
        if searchTestMode {
            startSearchTest()
            return
        }
        if let launchFailure {
            display(.failed(launchFailure))
            if shutdownTestMode {
                writeShutdownTestResult(ok: false, error: launchFailure)
                terminationWasConfirmed = true
                application.terminate(nil)
            }
            return
        }
        startServer()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if terminationApproved { return .terminateNow }
        if terminationPending { return .terminateLater }
        if shutdownTestMode { terminationWasConfirmed = true }
        let alreadyConfirmed = terminationWasConfirmed
        terminationWasConfirmed = false
        terminationPending = true
        // Return terminateLater before even an immediate clean/cancel result can reply to AppKit.
        Task { @MainActor in
            if alreadyConfirmed || self.coordinator == nil {
                self.stopForTermination(sender)
            } else {
                self.coordinator?.confirmDiscardingEdits(reason: "quitting Track") { [weak self] confirmed in
                    guard let self, self.terminationPending else { return }
                    if confirmed {
                        self.stopForTermination(sender)
                    } else {
                        self.terminationPending = false
                        sender.reply(toApplicationShouldTerminate: false)
                    }
                }
            }
        }
        return .terminateLater
    }

    private func stopForTermination(_ sender: NSApplication) {
        if shutdownTestMode {
            shutdownTestStartUptime = ProcessInfo.processInfo.systemUptime
            writeShutdownTestResult(ok: false, error: "waiting for supervised child exit", phase: "stopping")
        }

        let childStopped: @MainActor () -> Void = { [weak self] in
            guard let self else {
                sender.reply(toApplicationShouldTerminate: true)
                return
            }
            self.terminationPending = false
            self.terminationApproved = true
            self.writeShutdownTestResult(ok: true)
            sender.reply(toApplicationShouldTerminate: true)
        }
        if let supervisor {
            supervisor.stop(completion: childStopped)
        } else {
            Task { @MainActor in childStopped() }
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        shutdownTestTask?.cancel()
        supervisor?.stop()
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        if terminationApproved { return true }
        if terminationPending { return false }
        // Keep the last window and its editor DOM alive until the deferred app termination has
        // confirmed that the supervised server process really exited.
        application.terminate(nil)
        return false
    }

    func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        if menuItem.action == #selector(goBack(_:)) { return webView?.canGoBack ?? false }
        if menuItem.action == #selector(goForward(_:)) { return webView?.canGoForward ?? false }
        if menuItem.action == #selector(performTextFinderAction(_:)) {
            guard let action = NSTextFinder.Action(rawValue: menuItem.tag) else { return false }
            return textFinder?.validateAction(action) ?? false
        }
        return true
    }

    @objc private func retryServer(_ sender: Any?) {
        if let launchFailure {
            display(.failed(launchFailure))
            if smokeMode { finishSmoke(["ok": false, "error": launchFailure]) }
            return
        }
        supervisor?.start()
    }

    @objc private func goBack(_ sender: Any?) {
        guard let webView, webView.canGoBack else { return }
        webView.goBack() // The coordinator checks live editor state before replacing the document.
    }

    @objc private func goForward(_ sender: Any?) {
        guard let webView, webView.canGoForward else { return }
        webView.goForward() // The coordinator checks live editor state before replacing the document.
    }

    @objc private func reloadWorkspace(_ sender: Any?) {
        guard let webView else { return }
        webView.reload() // The coordinator checks live editor state before replacing the document.
    }

    private func startServer() {
        guard let options = launchOptions,
              let executableURL = Bundle.main.resourceURL?.appendingPathComponent("track", isDirectory: false) else {
            let message = "The app bundle is missing its Track command or launch configuration. Rebuild with `make desktop-app`."
            display(.failed(message))
            if smokeMode { finishSmoke(["ok": false, "error": message]) }
            if shutdownTestMode {
                writeShutdownTestResult(ok: false, error: message)
                terminationWasConfirmed = true
                application.terminate(nil)
            }
            return
        }

        let endpoint = workspaceOrigin.appendingPathComponent("api/vaults")
        let serverArguments = options.trackArguments.dropLast() + ["127.0.0.1:\(workspaceOrigin.port!)"]
        let supervisor = WebServerSupervisor(
            endpoint: endpoint,
            healthProbe: ServerHealthProbe.isReady,
            childFactory: { token in
                guard FileManager.default.isExecutableFile(atPath: executableURL.path) else {
                    throw CocoaError(.fileNoSuchFile, userInfo: [NSFilePathErrorKey: executableURL.path])
                }
                return TrackServerProcess(
                    executableURL: executableURL,
                    arguments: Array(serverArguments),
                    environmentOverrides: options.environmentOverrides
                )
            }
        )
        supervisor.onStateChange = { [weak self] state in
            self?.display(state)
        }
        self.supervisor = supervisor
        supervisor.start()
    }

    private func createWindow() {
        let root = SearchFindBarContainerView(frame: .zero)
        let configuration = WKWebViewConfiguration()
        // The production workspace keeps its tabs and settings at the stable loopback origin across
        // launches. The smoke test is fully isolated from a user's WebKit website data.
        if let fixture = restartFixture {
            configuration.websiteDataStore = WKWebsiteDataStore(forIdentifier: fixture.identifier)
        } else {
            configuration.websiteDataStore = websiteDataStore ?? (smokeMode || shutdownTestMode ? .nonPersistent() : .default())
        }
        if recoveryTestMode {
            // Exercise WKUIDelegate's popup callback without granting this behavior to the shipped app.
            configuration.preferences.javaScriptCanOpenWindowsAutomatically = true
        }
        let webView = WKWebView(frame: .zero, configuration: configuration)
        webView.translatesAutoresizingMaskIntoConstraints = false
        webView.allowsBackForwardNavigationGestures = true
        webView.isHidden = true

        let coordinator = WebViewCoordinator(
            workspaceOrigin: workspaceOrigin,
            staticAppsOrigin: URL(string: "http://127.0.0.1:\(workspaceOrigin.port! + 1)")!
        )
        coordinator.window = nil
        coordinator.webView = webView
        coordinator.restartFixture = restartFixture
        coordinator.smokeMode = smokeMode && !searchTestMode
        if recoveryTestMode {
            coordinator.externalURLHandler = { _ in }
        }
        coordinator.onConfirmDataLoss = { [weak self] reason, completion in
            if self?.recoveryTestMode == true {
                completion(true)
                return
            }
            self?.confirmPotentialDataLoss(reason: reason, completion: completion)
        }
        coordinator.onSmokeResult = { [weak self] result, error in
            guard let self else { return }
            if let error {
                self.finishSmoke(["ok": false, "error": error])
            } else if let result {
                self.handleSmokeResult(result)
            } else {
                self.finishSmoke(["ok": false, "error": "WKWebView smoke test returned no result"])
            }
        }
        webView.navigationDelegate = coordinator
        webView.uiDelegate = coordinator

        let textFinder = WebViewFindController(webView: webView)
        root.findBarView = textFinder.bar
        textFinder.onVisibilityChange = { [weak root] visible in
            root?.isFindBarVisible = visible
        }
        self.textFinder = textFinder
        findBarContainerView = root

        let statusView = makeStatusView()
        root.contentHost.addSubview(webView)
        root.contentHost.addSubview(statusView)
        NSLayoutConstraint.activate([
            webView.leadingAnchor.constraint(equalTo: root.contentHost.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: root.contentHost.trailingAnchor),
            webView.topAnchor.constraint(equalTo: root.contentHost.topAnchor),
            webView.bottomAnchor.constraint(equalTo: root.contentHost.bottomAnchor),
            statusView.leadingAnchor.constraint(equalTo: root.contentHost.leadingAnchor),
            statusView.trailingAnchor.constraint(equalTo: root.contentHost.trailingAnchor),
            statusView.topAnchor.constraint(equalTo: root.contentHost.topAnchor),
            statusView.bottomAnchor.constraint(equalTo: root.contentHost.bottomAnchor),
        ])

        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1200, height: 800),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false
        )
        window.title = "Track"
        WorkspaceWindowChrome.apply(to: window)
        window.minSize = NSSize(width: 680, height: 480)
        window.tabbingMode = .disallowed
        window.isReleasedWhenClosed = false
        window.contentView = root
        window.delegate = self
        window.center()
        window.makeKeyAndOrderFront(nil)
        application.activate(ignoringOtherApps: !smokeMode)

        coordinator.window = window
        self.webView = webView
        self.coordinator = coordinator
        self.statusView = statusView
    }

    private func startSearchTest() {
        guard let webView, let statusView else {
            finishSmoke(["ok": false, "error": "search test window was not created"])
            return
        }
        statusView.isHidden = true
        webView.isHidden = false
        window?.makeKeyAndOrderFront(nil)
        application.activate(ignoringOtherApps: true)
        let marker = "TrackSearchSmokeMarker"
        let html = """
        <!doctype html><html><head><meta charset="utf-8">
        <style>body { font: 24px system-ui; margin: 40px; background: white; color: black; }</style>
        </head><body><main>
        <p id="english-first">First \(marker) occurrence.</p>
        <p id="english-second">Second \(marker) occurrence.</p>
        <p id="japanese-first">最初の日本語検索です。</p>
        <p id="japanese-second">次の日本語検索です。</p>
        <p hidden>Hidden \(marker) 日本語検索</p>
        </main></body></html>
        """
        webView.loadHTMLString(html, baseURL: URL(string: "http://127.0.0.1:18765/"))
        waitForSearchTestDocument(attempt: 0)
    }

    private func waitForSearchTestDocument(attempt: Int) {
        guard let webView else {
            finishSmoke(["ok": false, "error": "search test WKWebView disappeared"])
            return
        }
        webView.evaluateJavaScript("document.body?.innerText.includes('TrackSearchSmokeMarker') === true") { [weak self] result, error in
            guard let self else { return }
            if error == nil, result as? Bool == true {
                self.runSearchMenuProbe()
                return
            }
            guard attempt < 100 else {
                self.finishSmoke(["ok": false, "error": error?.localizedDescription ?? "search test fixture did not load"])
                return
            }
            Task { @MainActor [weak self] in
                try? await Task.sleep(for: .milliseconds(50))
                self?.waitForSearchTestDocument(attempt: attempt + 1)
            }
        }
    }

    private func runSearchMenuProbe() {
        guard let webView, let window else {
            finishSmoke(["ok": false, "error": "search test lost its window or WKWebView"])
            return
        }
        let editMenu = application.mainMenu?.items.first(where: { $0.submenu?.title == "Edit" })?.submenu
        let findMenu = editMenu?.items.first(where: { $0.title == "Find" })?.submenu
        let find = findMenu?.item(withTitle: "Find…")
        let next = findMenu?.item(withTitle: "Find Next")
        let previous = findMenu?.item(withTitle: "Find Previous")
        let finderAction = #selector(performTextFinderAction(_:))
        let menuConfigured = find?.action == finderAction
            && find?.target === self
            && find?.tag == NSTextFinder.Action.showFindInterface.rawValue
            && find?.keyEquivalent == "f"
            && find?.keyEquivalentModifierMask == [.command]
            && next?.action == finderAction
            && next?.target === self
            && next?.tag == NSTextFinder.Action.nextMatch.rawValue
            && next?.keyEquivalent == "g"
            && next?.keyEquivalentModifierMask == [.command]
            && previous?.action == finderAction
            && previous?.target === self
            && previous?.tag == NSTextFinder.Action.previousMatch.rawValue
            && previous?.keyEquivalent == "G"
            && previous?.keyEquivalentModifierMask == [.command, .shift]

        _ = window.makeFirstResponder(webView)
        let probe = NativeFindSmokeTest(webView: webView, window: window, bar: { [weak self] in
            self?.findBarContainerView?.findBarView
        }, dispatch: { [weak self] action in
            guard let self else { return false }
            switch action {
            case .showFindInterface:
                return self.dispatchSearchShortcut(action, characters: "f", keyCode: 3, modifiers: [.command], window: window)
            case .nextMatch:
                return self.dispatchSearchShortcut(action, characters: "g", keyCode: 5, modifiers: [.command], window: window)
            case .previousMatch:
                return self.dispatchSearchShortcut(action, characters: "G", keyCode: 5, modifiers: [.command, .shift], window: window)
            default: return false
            }
        })
        Task { @MainActor [weak self] in
            self?.finishSmoke(await probe.run(menuConfigured: menuConfigured))
        }
    }

    private func dispatchSearchShortcut(
        _ expectedAction: NSTextFinder.Action,
        characters: String,
        keyCode: UInt16,
        modifiers: NSEvent.ModifierFlags,
        window: NSWindow
    ) -> Bool {
        let previousCount = searchTestActionCounts[expectedAction.rawValue, default: 0]
        _ = application.mainMenu?.performKeyEquivalent(
            with: keyEvent(characters, keyCode: keyCode, modifiers: modifiers, window: window)
        )
        return searchTestActionCounts[expectedAction.rawValue, default: 0] > previousCount
    }

    private func keyEvent(_ characters: String, keyCode: UInt16, modifiers: NSEvent.ModifierFlags, window: NSWindow) -> NSEvent {
        NSEvent.keyEvent(
            with: .keyDown,
            location: .zero,
            modifierFlags: modifiers,
            timestamp: ProcessInfo.processInfo.systemUptime,
            windowNumber: window.windowNumber,
            context: nil,
            characters: characters,
            charactersIgnoringModifiers: characters,
            isARepeat: false,
            keyCode: keyCode
        )!
    }

    private func makeStatusView() -> NSView {
        let container = NSView(frame: .zero)
        container.translatesAutoresizingMaskIntoConstraints = false

        let spinner = NSProgressIndicator(frame: .zero)
        spinner.style = .spinning
        spinner.controlSize = .regular
        spinner.isDisplayedWhenStopped = false

        let title = NSTextField(labelWithString: "Starting Track…")
        title.font = .systemFont(ofSize: 18, weight: .semibold)
        title.alignment = .center

        let detail = NSTextField(wrappingLabelWithString: "The app is starting its private loopback server.")
        detail.alignment = .center
        detail.maximumNumberOfLines = 12
        detail.lineBreakMode = .byWordWrapping

        let retry = NSButton(title: "Retry", target: self, action: #selector(retryServer(_:)))
        retry.keyEquivalent = "\r"
        retry.isHidden = true

        let stack = NSStackView(views: [spinner, title, detail, retry])
        stack.orientation = .vertical
        stack.alignment = .centerX
        stack.spacing = 14
        stack.translatesAutoresizingMaskIntoConstraints = false
        container.addSubview(stack)
        detail.widthAnchor.constraint(lessThanOrEqualToConstant: 620).isActive = true
        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: container.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: container.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: container.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: container.trailingAnchor, constant: -24),
        ])

        progressIndicator = spinner
        statusTitle = title
        statusDetail = detail
        retryButton = retry
        return container
    }

    private func display(_ state: WebServerSupervisor.State) {
        guard isViewLoaded else { return }
        switch state {
        case .stopped:
            statusTitle?.stringValue = "Track is stopped"
            statusDetail?.stringValue = "Start the local server to open the workspace."
            progressIndicator?.stopAnimation(nil)
            progressIndicator?.isHidden = true
            retryButton?.isHidden = false
            showStatus()
        case .starting:
            statusTitle?.stringValue = "Starting Track…"
            statusDetail?.stringValue = "Starting the bundled Go server on 127.0.0.1:18765 and waiting for its live API."
            progressIndicator?.isHidden = false
            progressIndicator?.startAnimation(nil)
            retryButton?.isHidden = true
            showStatus()
        case .ready:
            progressIndicator?.stopAnimation(nil)
            progressIndicator?.isHidden = true
            retryButton?.isHidden = true
            statusView?.isHidden = true
            webView?.isHidden = false
            if coordinator?.hasCompletedWorkspaceNavigation == true {
                coordinator?.serverDidRecover()
            } else {
                coordinator?.loadWorkspace()
            }
            if recoveryTestMode, recoveryPhase == .restarting {
                verifyRecoveryDraft()
            }
            if shutdownTestMode {
                writeShutdownTestResult(ok: false, error: "waiting for test trigger", phase: "server-ready")
                waitForShutdownTestTrigger()
            }
        case .stopping:
            statusTitle?.stringValue = "Stopping Track…"
            statusDetail?.stringValue = "The app is shutting down only the Go process it started."
            progressIndicator?.isHidden = false
            progressIndicator?.startAnimation(nil)
            retryButton?.isHidden = true
            showStatus()
        case .failed(let message):
            statusTitle?.stringValue = "Track is unavailable"
            statusDetail?.stringValue = message
            progressIndicator?.stopAnimation(nil)
            progressIndicator?.isHidden = true
            retryButton?.isHidden = false
            showStatus()
            if smokeMode { finishSmoke(["ok": false, "error": message]) }
            if shutdownTestMode {
                writeShutdownTestResult(ok: false, error: message)
                terminationWasConfirmed = true
                application.terminate(nil)
            }
        }
    }

    private var isViewLoaded: Bool { statusView != nil }

    private func showStatus() {
        webView?.isHidden = true
        statusView?.isHidden = false
    }

    private func confirmPotentialDataLoss(
        reason: String,
        completion: @escaping @MainActor @Sendable (Bool) -> Void
    ) {
        guard let window else {
            completion(false)
            return
        }
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Unsaved edits may be lost"
        alert.informativeText = "Track cannot reliably detect unsaved input in every editor, embedded page, or static app. Continue with \(reason)?"
        alert.addButton(withTitle: "Continue")
        alert.addButton(withTitle: "Cancel")
        alert.beginSheetModal(for: window) { response in
            completion(response == .alertFirstButtonReturn)
        }
    }

    private func finishSmoke(_ result: [String: Any]) {
        guard smokeMode, !smokeFinished else { return }
        smokeFinished = true
        var report = result
        if report["ok"] == nil { report["ok"] = false }
        do {
            guard let smokeResultURL else {
                throw CocoaError(.fileNoSuchFile, userInfo: [NSLocalizedDescriptionKey: "TRACK_WEB_SMOKE_RESULT is not set"])
            }
            let data = try JSONSerialization.data(withJSONObject: report, options: [.sortedKeys])
            try data.write(to: smokeResultURL, options: .atomic)
        } catch {
            fputs("Track smoke result: \(error.localizedDescription)\n", stderr)
        }
        if searchTestMode { terminationApproved = true }
        terminationWasConfirmed = true
        if restartTestMode && webView == nil { terminationApproved = true }
        application.terminate(nil)
    }

    private func handleSmokeResult(_ result: [String: Any]) {
        guard recoveryTestMode else {
            finishSmoke(result)
            return
        }
        guard result["ok"] as? Bool == true,
              let coordinator,
              let supervisor else {
            finishSmoke(result)
            return
        }

        initialSmokeReport = result
        let popupToken = UUID().uuidString
        coordinator.runTargetBlankProbe(token: popupToken) { [weak self] events, error in
            guard let self,
                  error == nil,
                  let events,
                  self.targetBlankProbePassed(events) else {
                self?.finishSmoke([
                    "ok": false,
                    "error": error ?? "WKWebView target=_blank policy did not process each destination once",
                    "targetBlank": events ?? [:],
                ])
                return
            }

            var report = self.initialSmokeReport ?? [:]
            report["targetBlank"] = ["ok": true, "events": events]
            self.initialSmokeReport = report
            let draftToken = UUID().uuidString
            coordinator.installRecoveryDraft(token: draftToken) { [weak self] installed in
                guard let self, installed else {
                    self?.finishSmoke(["ok": false, "error": "could not install a test draft in WKWebView"])
                    return
                }
                self.recoveryDraftToken = draftToken
                self.recoveryPhase = .stopping
                supervisor.stop { [weak self] in
                    guard let self else { return }
                    self.recoveryPhase = .restarting
                    supervisor.start()
                }
            }
        }
    }

    private func targetBlankProbePassed(_ events: [String: Int]) -> Bool {
        return events["created.script"] == 1
            && events["cancel.script"] == 1
            && events["external-open.script", default: 0] == 0
            && events["created.internal"] == 1
            && events["internal.internal"] == 1
    }

    private func verifyRecoveryDraft() {
        guard recoveryPhase == .restarting,
              let token = recoveryDraftToken,
              let coordinator else { return }
        recoveryPhase = .verifying
        coordinator.verifyRecoveryDraft(token: token) { [weak self] report, error in
            guard let self else { return }
            var result = self.initialSmokeReport ?? [:]
            let expectedValue = "unsaved draft \(token)"
            let rootChildren = (report?["rootChildren"] as? NSNumber)?.intValue
                ?? (report?["rootChildren"] as? Int)
                ?? 0
            let preserved = error == nil
                && report?["token"] as? String == token
                && report?["value"] as? String == expectedValue
            result["recovery"] = [
                "ok": preserved,
                "sameDocument": report?["token"] as? String == token,
                "draftValue": report?["value"] as? String ?? "",
                "rootChildren": rootChildren,
                "currentURL": report?["currentURL"] as? String ?? "",
                "error": error as Any? ?? NSNull(),
            ]
            result["ok"] = preserved
            self.finishSmoke(result)
        }
    }

    private func waitForShutdownTestTrigger() {
        guard shutdownTestTask == nil else { return }
        guard let shutdownTestTriggerURL else {
            writeShutdownTestResult(ok: false, error: "TRACK_WEB_SHUTDOWN_TEST_TRIGGER is not set")
            terminationWasConfirmed = true
            application.terminate(nil)
            return
        }
        shutdownTestTask = Task { @MainActor [weak self] in
            while !Task.isCancelled {
                if FileManager.default.fileExists(atPath: shutdownTestTriggerURL.path) {
                    try? FileManager.default.removeItem(at: shutdownTestTriggerURL)
                    guard let self else { return }
                    self.terminationWasConfirmed = true
                    // Drive the same selector as the menu Quit action from an AppKit run-loop turn;
                    // calling terminate from a Swift task can bypass the normal event-loop handoff.
                    self.application.perform(#selector(NSApplication.terminate(_:)), with: nil, afterDelay: 0)
                    return
                }
                do {
                    try await Task.sleep(for: .milliseconds(25))
                } catch {
                    return
                }
            }
        }
    }

    private func writeShutdownTestResult(ok: Bool, error: String? = nil, phase: String = "terminated") {
        guard shutdownTestMode, let shutdownTestResultURL else { return }
        var result: [String: Any] = ["ok": ok, "phase": phase]
        if let shutdownTestStartUptime {
            result["stopDurationMs"] = Int((ProcessInfo.processInfo.systemUptime - shutdownTestStartUptime) * 1_000)
        }
        if let error { result["error"] = error }
        do {
            let data = try JSONSerialization.data(withJSONObject: result, options: [.sortedKeys])
            try data.write(to: shutdownTestResultURL, options: .atomic)
        } catch {
            fputs("Track shutdown test result: \(error.localizedDescription)\n", stderr)
        }
    }

    private func installMenu() {
        let menu = NSMenu()

        let appItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(NSMenuItem(title: "About Track", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: ""))
        appMenu.addItem(.separator())
        appMenu.addItem(NSMenuItem(title: "Hide Track", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h"))
        appMenu.addItem(NSMenuItem(title: "Hide Others", action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "h").also { $0.keyEquivalentModifierMask = [.command, .option] })
        appMenu.addItem(NSMenuItem(title: "Show All", action: #selector(NSApplication.unhideAllApplications(_:)), keyEquivalent: ""))
        appMenu.addItem(.separator())
        appMenu.addItem(NSMenuItem(title: "Quit Track", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"))
        appItem.submenu = appMenu
        menu.addItem(appItem)

        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(NSMenuItem(title: "Undo", action: Selector(("undo:")), keyEquivalent: "z"))
        editMenu.addItem(NSMenuItem(title: "Redo", action: Selector(("redo:")), keyEquivalent: "Z"))
        editMenu.addItem(.separator())
        editMenu.addItem(NSMenuItem(title: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x"))
        editMenu.addItem(NSMenuItem(title: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c"))
        editMenu.addItem(NSMenuItem(title: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v"))
        editMenu.addItem(NSMenuItem(title: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a"))
        editMenu.addItem(.separator())
        let findItem = NSMenuItem(title: "Find", action: nil, keyEquivalent: "")
        let findMenu = NSMenu(title: "Find")
        findMenu.addItem(textFinderMenuItem(
            title: "Find…",
            action: .showFindInterface,
            keyEquivalent: "f",
            modifiers: [.command]
        ))
        findMenu.addItem(textFinderMenuItem(
            title: "Find Next",
            action: .nextMatch,
            keyEquivalent: "g",
            modifiers: [.command]
        ))
        findMenu.addItem(textFinderMenuItem(
            title: "Find Previous",
            action: .previousMatch,
            keyEquivalent: "G",
            modifiers: [.command, .shift]
        ))
        findItem.submenu = findMenu
        editMenu.addItem(findItem)
        editItem.submenu = editMenu
        menu.addItem(editItem)

        let viewItem = NSMenuItem()
        let viewMenu = NSMenu(title: "View")
        viewMenu.addItem(NSMenuItem(title: "Back", action: #selector(goBack(_:)), keyEquivalent: "[").also { $0.target = self })
        viewMenu.addItem(NSMenuItem(title: "Forward", action: #selector(goForward(_:)), keyEquivalent: "]").also { $0.target = self })
        viewMenu.addItem(.separator())
        viewMenu.addItem(NSMenuItem(title: "Reload", action: #selector(reloadWorkspace(_:)), keyEquivalent: "r").also { $0.target = self })
        viewItem.submenu = viewMenu
        menu.addItem(viewItem)

        WorkspaceWindowChrome.installWindowMenu(in: menu, application: application)
        application.mainMenu = menu
    }

    private func textFinderMenuItem(
        title: String,
        action: NSTextFinder.Action,
        keyEquivalent: String,
        modifiers: NSEvent.ModifierFlags
    ) -> NSMenuItem {
        NSMenuItem(title: title, action: #selector(performTextFinderAction(_:)), keyEquivalent: keyEquivalent).also {
            $0.target = self
            $0.tag = action.rawValue
            $0.keyEquivalentModifierMask = modifiers
        }
    }

    @objc private func performTextFinderAction(_ sender: Any?) {
        guard let menuItem = sender as? NSMenuItem,
              let action = NSTextFinder.Action(rawValue: menuItem.tag) else { return }
        if searchTestMode { searchTestActionCounts[action.rawValue, default: 0] += 1 }
        textFinder?.performAction(action)
    }

    private var window: NSWindow? { NSApp.windows.first }
}

private enum ServerHealthProbe {
    static func isReady(_ endpoint: URL) async -> Bool {
        var request = URLRequest(url: endpoint)
        request.httpMethod = "GET"
        request.timeoutInterval = 1
        request.cachePolicy = .reloadIgnoringLocalCacheData
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            guard let response = response as? HTTPURLResponse,
                  response.statusCode == 200,
                  let payload = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  payload["active"] is [String: Any],
                  payload["vaults"] is [[String: Any]] else {
                return false
            }
            return true
        } catch {
            return false
        }
    }
}

private enum RecoveryTestPhase {
    case waitingForInitialPage
    case stopping
    case restarting
    case verifying
}

@MainActor
private final class SearchFindBarContainerView: NSView {
    let contentHost = NSView(frame: .zero)
    var findBarView: NSView? {
        didSet {
            oldValue?.removeFromSuperview()
            if let findBarView { addSubview(findBarView) }
            needsLayout = true
        }
    }
    var isFindBarVisible = false {
        didSet {
            needsLayout = true
            layoutSubtreeIfNeeded()
        }
    }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        addSubview(contentHost)
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) is unsupported")
    }

    override func layout() {
        super.layout()
        let findBarHeight = isFindBarVisible ? (findBarView?.frame.height ?? 0) : 0
        contentHost.frame = NSRect(x: 0, y: 0, width: bounds.width, height: max(0, bounds.height - findBarHeight))
        if let findBarView {
            findBarView.isHidden = !isFindBarVisible
            findBarView.frame = NSRect(
                x: 0,
                y: contentHost.frame.maxY,
                width: bounds.width,
                height: findBarView.frame.height
            )
        }
    }

}

private extension NSMenuItem {
    @discardableResult
    func also(_ configure: (NSMenuItem) -> Void) -> NSMenuItem {
        configure(self)
        return self
    }
}
