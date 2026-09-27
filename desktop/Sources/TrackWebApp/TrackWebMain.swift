import AppKit

@main
@MainActor
struct TrackWebMain {
    static func main() {
        let application = NSApplication.shared
        let delegate = AppDelegate(application: application)
        application.delegate = delegate
        application.run()
    }
}
