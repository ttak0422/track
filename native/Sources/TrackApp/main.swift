import SwiftUI
import TrackAPI
import TrackUI

@main
struct TrackApp: App {
    @State private var process: TrackProcess

    init() {
        _process = State(initialValue: TrackProcess(executableURL: Self.trackExecutableURL()))
    }

    var body: some Scene {
        WindowGroup {
            RootView(process: process)
                .task { process.start() }
                .onDisappear { process.stop() }
        }
    }

    private static func trackExecutableURL() -> URL {
        if let override = ProcessInfo.processInfo.environment["TRACK_EXECUTABLE"] {
            return URL(fileURLWithPath: override)
        }
        return Bundle.main.bundleURL
            .appendingPathComponent("Contents/Helpers/track")
    }
}

private struct RootView: View {
    let process: TrackProcess

    var body: some View {
        switch process.state {
        case .stopped, .starting:
            ProgressView("Starting track…")
                .frame(minWidth: 900, minHeight: 600)
        case .failed(let message):
            ContentUnavailableView("Could not start track", systemImage: "exclamationmark.triangle", description: Text(message))
                .frame(minWidth: 900, minHeight: 600)
        case .ready(let client):
            TabView {
                SearchReaderView(client: client)
                    .tabItem { Label("Notes", systemImage: "doc.text") }
                TasksView(model: TasksModel(client: client))
                    .tabItem { Label("Tasks", systemImage: "checklist") }
            }
        }
    }
}
