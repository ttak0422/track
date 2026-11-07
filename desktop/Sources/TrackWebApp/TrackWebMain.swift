import AppKit
import TrackWebCore

@main
@MainActor
struct TrackWebMain {
    static func main() {
        if CommandLine.arguments.dropFirst().elementsEqual(["--version"]) {
            print(BuildInfo.current.details)
            return
        }
        let application = NSApplication.shared
        let delegate = AppDelegate(application: application)
        application.delegate = delegate
        application.run()
    }
}
