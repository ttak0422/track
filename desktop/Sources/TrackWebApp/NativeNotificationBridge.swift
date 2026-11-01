import AppKit
import Foundation
import UserNotifications
import WebKit

@MainActor
protocol ActivityNotificationDriver: AnyObject {
    var open: ((String) -> Void)? { get set }
    func permission() async -> String
    func authorize() async throws -> Bool
    func post(identifier: String) async throws
    func remove(identifiers: [String])
}

@MainActor
final class AppleActivityNotificationDriver: NSObject, ActivityNotificationDriver, UNUserNotificationCenterDelegate {
    private lazy var center: UNUserNotificationCenter = {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        return center
    }()
    var open: ((String) -> Void)?

    func permission() async -> String {
        // SwiftPM test executables are not app bundles and cannot use Notification Center.
        guard Bundle.main.bundleIdentifier != nil else { return "Unavailable" }
        let settings = await center.notificationSettings()
        switch settings.authorizationStatus {
        case .notDetermined: return "Not requested"
        case .denied: return "Denied in System Settings"
        case .authorized, .provisional, .ephemeral:
            return settings.alertSetting == .enabled || settings.notificationCenterSetting == .enabled
                ? "Allowed" : "Delivery disabled in System Settings"
        @unknown default: return "Unavailable"
        }
    }

    func authorize() async throws -> Bool {
        // Only the explicit Enable menu action may request permission.
        if await permission() == "Not requested" {
            return try await center.requestAuthorization(options: [.alert])
        }
        return await permission() == "Allowed"
    }

    func post(identifier: String) async throws {
        let content = UNMutableNotificationContent()
        content.title = "Track"
        content.body = "Your vault has new activity. Open Track to view it."
        // No note titles, bodies, paths or identifiers reach Notification Center or the lock screen.
        try await center.add(UNNotificationRequest(identifier: identifier, content: content, trigger: nil))
    }

    func remove(identifiers: [String]) {
        guard Bundle.main.bundleIdentifier != nil else { return }
        center.removeDeliveredNotifications(withIdentifiers: identifiers)
        center.removePendingNotificationRequests(withIdentifiers: identifiers)
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                           withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .list])
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                           withCompletionHandler completionHandler: @escaping () -> Void) {
        let identifier = response.notification.request.identifier
        if response.actionIdentifier == UNNotificationDefaultActionIdentifier {
            Task { @MainActor [weak self] in self?.open?(identifier) }
        }
        completionHandler()
    }
}

@MainActor
final class NativeNotificationBridge: NSObject, WKScriptMessageHandlerWithReply, NSMenuDelegate {
    private static let workspacePath = #"^(/|/(markdown|graph|voice|calendar|tasks|empty)/?|/notes/([a-z0-9-]+~)?[A-Za-z0-9]+/?|/day/[0-9]{4}-[0-9]{2}-[0-9]{2}/?|/tags/.+)$"#
    private let origin: URL
    private var trustedDocument = false
    private let driver: any ActivityNotificationDriver
    private let saveEnabled: (Bool) -> Void
    private(set) var enabled: Bool
    private var changing = false
    private var permissionState = "Checking permission…"
    private var delivered: [String: String] = [:]
    private var seen = Set<String>()
    private var posting: [String: Task<Bool, Never>] = [:]
    private var postingIdentifiers = Set<String>()
    private var preferenceGeneration = 0
    private var day = ""
    private var ready = false
    private var pendingOpen: String?
    weak var webView: WKWebView?
    private let toggleItem = NSMenuItem(title: "Enable macOS Notifications…", action: nil, keyEquivalent: "")
    private let statusItem = NSMenuItem(title: "Off · activity uses in-app notifications", action: nil, keyEquivalent: "")

    init(origin: URL, driver: any ActivityNotificationDriver, enabled: Bool = false, saveEnabled: @escaping (Bool) -> Void = { _ in }) {
        self.origin = origin
        self.driver = driver
        self.enabled = enabled
        self.saveEnabled = saveEnabled
        super.init()
        driver.open = { [weak self] identifier in self?.open(identifier) }
    }

    func menuItem() -> NSMenuItem {
        let item = NSMenuItem(title: "Activity Notifications", action: nil, keyEquivalent: "")
        let menu = NSMenu(title: item.title)
        menu.autoenablesItems = false
        menu.delegate = self
        statusItem.isEnabled = false
        menu.addItem(statusItem)
        toggleItem.target = self
        toggleItem.action = #selector(toggle)
        menu.addItem(toggleItem)
        let settings = NSMenuItem(title: "Open macOS Notification Settings…", action: #selector(openSettings), keyEquivalent: "")
        settings.target = self
        menu.addItem(settings)
        item.submenu = menu
        updateMenu()
        return item
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        Task { permissionState = await driver.permission(); updateMenu() }
    }

    private func updateMenu() {
        statusItem.title = enabled ? "On · \(permissionState)" : "Off · \(permissionState) · using in-app notifications"
        toggleItem.title = enabled ? "Disable macOS Notifications" : "Enable macOS Notifications…"
        toggleItem.isEnabled = !changing
    }

    @objc private func toggle() { Task { await setEnabled(!enabled) } }

    func setEnabled(_ value: Bool) async {
        guard !changing else { return }
        changing = true
        preferenceGeneration += 1
        updateMenu()
        if value {
            do { enabled = try await driver.authorize() }
            catch { enabled = false }
        } else {
            enabled = false
            driver.remove(identifiers: Array(delivered.keys) + Array(postingIdentifiers))
            pendingOpen = nil
            delivered.removeAll()
            seen.removeAll()
        }
        permissionState = await driver.permission()
        saveEnabled(enabled)
        changing = false
        updateMenu()
    }

    @objc private func openSettings() {
        NSWorkspace.shared.open(URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension")!)
    }

    func install(in configuration: WKWebViewConfiguration) {
        configuration.userContentController.addScriptMessageHandler(self, contentWorld: .page, name: "trackNotifications")
        let json = String(data: try! JSONSerialization.data(withJSONObject: [origin.absoluteString]), encoding: .utf8)!
        let pathJSON = String(data: try! JSONSerialization.data(withJSONObject: [Self.workspacePath]), encoding: .utf8)!
        configuration.userContentController.addUserScript(WKUserScript(source: """
        if (\(json).includes(location.origin) && new RegExp(\(pathJSON)[0]).test(location.pathname)) {
          Object.defineProperty(window, '__trackNativeNotifications', { value: {
            postMessage: message => window.webkit.messageHandlers.trackNotifications.postMessage(message)
          }});
        }
        """, injectionTime: .atDocumentStart, forMainFrameOnly: true))
    }

    private func allows(_ url: URL?) -> Bool {
        guard let url else { return false }
        return url.scheme == origin.scheme && url.host == origin.host && url.port == origin.port
            && (url.path.isEmpty ? "/" : url.path).range(of: Self.workspacePath, options: .regularExpression) != nil
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage,
                               replyHandler: @escaping @MainActor @Sendable (Any?, String?) -> Void) {
        guard trustedDocument, message.frameInfo.isMainFrame, message.webView === webView,
              allows(message.frameInfo.request.url), allows(webView?.url),
              let body = message.body as? [String: String] else { replyHandler(false, nil); return }
        if body == ["action": "ready"] {
            ready = true
            if let pendingOpen { open(pendingOpen) }
            replyHandler(true, nil)
            return
        }
        guard body["action"] == "activity", body.count == 3,
              let noteID = body["noteID"], noteID.count <= 200,
              noteID.range(of: "^([a-z0-9-]+~)?[A-Za-z0-9]+$", options: .regularExpression) != nil,
              let day = body["day"], day.range(of: "^[0-9]{4}-[0-9]{2}-[0-9]{2}$", options: .regularExpression) != nil else {
            replyHandler(false, nil); return
        }
        Task { replyHandler(await post(noteID: noteID, day: day), nil) }
    }

    func post(noteID: String, day: String) async -> Bool {
        guard enabled, await driver.permission() == "Allowed", enabled else { return false }
        if self.day != day { self.day = day; seen.removeAll() }
        let key = "\(day):\(noteID)"
        if seen.contains(key) { return true }
        if let pending = posting[key] { return await pending.value }
        guard delivered.count + posting.count < 1000 else { return false }
        let generation = preferenceGeneration
        let identifier = "track-activity-\(UUID().uuidString)"
        postingIdentifiers.insert(identifier)
        let task = Task { @MainActor in
            do {
                try await driver.post(identifier: identifier)
                guard enabled, generation == preferenceGeneration else { driver.remove(identifiers: [identifier]); return false }
                delivered[identifier] = noteID
                seen.insert(key)
                return true
            } catch { return false }
        }
        posting[key] = task
        let result = await task.value
        posting[key] = nil
        postingIdentifiers.remove(identifier)
        return result
    }

    func pageChanged() { ready = false; trustedDocument = false }

    // Capture committed document identity: history.replaceState must not promote an HTML asset.
    func documentCommitted(_ url: URL?) { trustedDocument = allows(url) }

    private func open(_ identifier: String) {
        guard enabled else { return }
        webView?.window?.makeKeyAndOrderFront(nil)
        NSApplication.shared.activate(ignoringOtherApps: true)
        // Routes stay in memory: a click after quitting cannot open an ID in a different launch vault.
        guard let noteID = delivered[identifier] else { return }
        guard ready, let webView, allows(webView.url) else { pendingOpen = identifier; return }
        pendingOpen = nil
        let json = String(data: try! JSONSerialization.data(withJSONObject: ["noteID": noteID]), encoding: .utf8)!
        webView.evaluateJavaScript("window.dispatchEvent(new CustomEvent('track-native-notification-open', {detail: \(json)}))")
    }
}
