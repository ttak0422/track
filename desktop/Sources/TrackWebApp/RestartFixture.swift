import Foundation

/// Only the disposable, separately identified app made by desktop-restart-test.py can opt in.
/// Never accepts a default store, arbitrary store name, or the production app's bundle identity.
struct RestartFixture {
    let identifier: UUID
    let root: URL
    let phase: String
    let script: String

    init() throws {
        let env = ProcessInfo.processInfo.environment
        guard let path = env["TRACK_WEB_RESTART_ROOT"],
              let id = env["TRACK_WEB_RESTART_ID"], let uuid = UUID(uuidString: id),
              uuid.uuidString.lowercased() == id,
              Bundle.main.bundleIdentifier == "com.ttak0422.track.restart-fixture.\(id)",
              let phase = env["TRACK_WEB_RESTART_PHASE"],
              ["seed", "restore", "cleanup"].contains(phase) else {
            throw CocoaError(.validationMissingMandatoryProperty)
        }
        let root = URL(fileURLWithPath: path).standardizedFileURL
        guard root.lastPathComponent == "track-restart-\(id)",
              try String(contentsOf: root.appendingPathComponent("owner"), encoding: .utf8) == id,
              env["TRACK_CONFIG"] == root.appendingPathComponent("machine.yml").path,
              env["TRACK_CACHE_DIR"] == root.appendingPathComponent("cache").path,
              env["TRACK_VAULT"] == nil,
              env["TRACK_WEB_SMOKE_RESULT"] == root.appendingPathComponent("\(phase).json").path else {
            throw CocoaError(.validationMissingMandatoryProperty)
        }
        self.identifier = uuid
        self.root = root
        self.phase = phase
        script = phase == "cleanup" ? "" : try String(
            contentsOf: root.appendingPathComponent("\(phase).js"), encoding: .utf8
        )
    }
}
