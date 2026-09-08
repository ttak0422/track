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

// MARK: - Root

private struct RootView: View {
    let process: TrackProcess

    // Appearance settings, read at the root so the whole window follows them:
    // the Settings tab writes the same keys and every re-render re-applies
    // both. "system" stores nothing (ThemeMode.storedValue → nil), matching
    // the web's applyTheme.
    @AppStorage(TrackAppearance.themeKey) private var themeRaw: String?
    @AppStorage(TrackAppearance.fontScaleKey) private var fontScale = TrackAppearance.defaultFontScale

    private var themeMode: ThemeMode { ThemeMode(stored: themeRaw) }

    var body: some View {
        switch process.state {
        case .stopped, .starting:
            ProgressView("Starting track…")
                .frame(minWidth: 900, minHeight: 600)
        case .failed(let message):
            ContentUnavailableView("Could not start track", systemImage: "exclamationmark.triangle", description: Text(message))
                .frame(minWidth: 900, minHeight: 600)
        case .ready(let client):
            MainTabView(client: client)
                .preferredColorScheme(themeMode.preferredColorScheme)
                .environment(\.trackFontScale, TrackAppearance.clampFontScale(fontScale))
                .frame(minWidth: 900, minHeight: 600)
        }
    }
}

// MARK: - Tab shell

/// The seven surfaces of the native workspace: Notes (search + reader), the
/// activity Calendar, the link Graph, the vault Browse panes, Tasks, Voice
/// dictation, and Settings. Every tab is built from the ready client handed
/// over by TrackProcess; each owns its view model in @State so tab switches
/// and the appearance re-renders above keep their loaded data.
private struct MainTabView: View {
    let client: TrackClient
    @State private var tasks: TasksModel
    @State private var poller: LiveEventPoller?

    init(client: TrackClient) {
        self.client = client
        _tasks = State(initialValue: TasksModel(client: client))
    }

    var body: some View {
        TabView {
            SearchReaderView(client: client)
                .tabItem { Label("Notes", systemImage: "doc.text") }
            CalendarView(client: client)
                .tabItem { Label("Calendar", systemImage: "calendar") }
            GraphTabView(client: client)
                .tabItem { Label("Graph", systemImage: "network") }
            BrowseTabView(client: client)
                .tabItem { Label("Browse", systemImage: "folder") }
            TasksView(model: tasks)
                .tabItem { Label("Tasks", systemImage: "checklist") }
            VoiceView(client: client)
                .tabItem { Label("Voice", systemImage: "mic") }
            SettingsTabView()
                .tabItem { Label("Settings", systemImage: "gearshape") }
        }
        .task {
            // Live vault updates (web useLiveEvents): the poller posts
            // .trackVaultChanged and the data views reload themselves.
            let poller = LiveEventPoller(baseURL: client.baseURL) {
                NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
            }
            self.poller = poller
            poller.start()
        }
        .onDisappear { poller?.stop() }
    }
}

// MARK: - Browse tab

private enum BrowsePane: String, CaseIterable, Identifiable {
    case hierarchy
    case tags
    case activity

    var id: String { rawValue }

    var title: String {
        switch self {
        case .hierarchy: return "Hierarchy"
        case .tags: return "Tags"
        case .activity: return "Activity"
        }
    }
}

/// The vault-wide browsing surfaces over one shared BrowseModel: the "up"
/// hierarchy tree, the tag index, and the activity heatmap. Hierarchy taps
/// have no reader to hand off to yet, so the chosen note is echoed in a
/// status strip instead of opened.
private struct BrowseTabView: View {
    let client: TrackClient
    @State private var model: BrowseModel
    @State private var pane: BrowsePane = .hierarchy
    @State private var selectedNote: String?

    init(client: TrackClient) {
        self.client = client
        _model = State(initialValue: BrowseModel(client: client))
    }

    var body: some View {
        VStack(spacing: 0) {
            Picker("Browse", selection: $pane) {
                ForEach(BrowsePane.allCases) { pane in
                    Text(pane.title).tag(pane)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            Divider()
            switch pane {
            case .hierarchy:
                HierarchyView(model: model) { raw in selectedNote = raw }
            case .tags:
                TagView(model: model)
            case .activity:
                ActivityHeatmapView(model: model)
            }
            if let selectedNote {
                Divider()
                HStack(spacing: 8) {
                    Text("Selected: \(selectedNote)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    Spacer()
                    Button("Clear") { self.selectedNote = nil }
                        .buttonStyle(.plain)
                        .font(.caption)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
            }
        }
    }
}

// MARK: - Graph tab

/// The link graph surfaces over one shared GraphModel. The full graph lists
/// the vault by link degree; picking a note (or typing its id) switches to
/// the one-hop local graph centered on it, and "Full graph" climbs back out.
private struct GraphTabView: View {
    let client: TrackClient
    @State private var model: GraphModel
    @State private var centerID: TrackID?
    @State private var centerText = ""

    init(client: TrackClient) {
        self.client = client
        _model = State(initialValue: GraphModel(client: client))
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                TextField("Note id", text: $centerText)
                    .textFieldStyle(.plain)
                    .onSubmit(jumpToCenter)
                Button("Local graph") { jumpToCenter() }
                    .buttonStyle(.plain)
                    .disabled(centerText.trimmingCharacters(in: .whitespaces).isEmpty)
                if centerID != nil {
                    Button("Full graph") { clearCenter() }
                        .buttonStyle(.plain)
                }
                Spacer()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            Divider()
            if let centerID {
                LocalGraphView(model: model, centerID: centerID) { raw in setCenter(raw) }
            } else {
                GraphFullView(model: model) { raw in setCenter(raw) }
            }
        }
    }

    private func jumpToCenter() {
        let raw = centerText.trimmingCharacters(in: .whitespaces)
        guard !raw.isEmpty else { return }
        setCenter(raw)
    }

    private func setCenter(_ raw: String) {
        centerID = TrackID(raw)
        centerText = raw
    }

    private func clearCenter() {
        centerID = nil
        centerText = ""
    }
}

// MARK: - Settings tab

/// Appearance controls: the ThemeMode picker writes "track.theme" (the same
/// key as the web's themeState.ts) and the font-scale stepper writes
/// "track.fontScale". RootView observes both, so a change here re-themes the
/// whole window immediately.
private struct SettingsTabView: View {
    @AppStorage(TrackAppearance.themeKey) private var themeRaw: String?
    @AppStorage(TrackAppearance.fontScaleKey) private var fontScale = TrackAppearance.defaultFontScale

    var body: some View {
        Form {
            Section {
                Picker("Theme", selection: themeBinding) {
                    ForEach(ThemeMode.allCases, id: \.self) { mode in
                        Text(mode.label).tag(mode)
                    }
                }
                .pickerStyle(.radioGroup)
            } footer: {
                Text("Color tokens follow docs/spec/design.md. System follows the macOS appearance.")
            }
            Section {
                Stepper(
                    value: fontScaleBinding,
                    in: TrackAppearance.fontScaleRange,
                    step: 0.05
                ) {
                    Text("Text size \(TrackAppearance.clampFontScale(fontScale), specifier: "%.2f")×")
                }
            } footer: {
                Text("Scales the reading surface from \(TrackAppearance.fontScaleRange.lowerBound, specifier: "%.2f")× to \(TrackAppearance.fontScaleRange.upperBound, specifier: "%.2f")×.")
            }
        }
        .formStyle(.grouped)
    }

    private var themeBinding: Binding<ThemeMode> {
        Binding(
            get: { ThemeMode(stored: themeRaw) },
            set: { themeRaw = $0.storedValue }
        )
    }

    private var fontScaleBinding: Binding<Double> {
        Binding(
            get: { TrackAppearance.clampFontScale(fontScale) },
            set: { fontScale = TrackAppearance.clampFontScale($0) }
        )
    }
}
