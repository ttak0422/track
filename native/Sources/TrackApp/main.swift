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
    @AppStorage(TrackAppearance.fontSizeKey) private var fontSize = TrackAppearance.baseFontSize
    @AppStorage(TrackAppearance.previewFontSizeKey) private var previewFontSize = TrackAppearance.baseFontSize
    // Read the pre-parity key as a fallback only when the new reader setting
    // is still at its default.
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
                .environment(\.trackFontScale, readerScale)
                .environment(\.trackPreviewFontScale, TrackAppearance.scale(forFontSize: previewFontSize))
                .frame(minWidth: 900, minHeight: 600)
        }
    }

    private var readerScale: Double {
        if UserDefaults.standard.object(forKey: TrackAppearance.fontSizeKey) == nil,
           fontScale != TrackAppearance.defaultFontScale {
            return TrackAppearance.clampFontScale(fontScale)
        }
        return TrackAppearance.scale(forFontSize: fontSize)
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
    @State private var selectedTab = MainTab.notes
    @State private var openedNote: String?
    @State private var reader: NoteReaderModel

    init(client: TrackClient) {
        self.client = client
        _tasks = State(initialValue: TasksModel(client: client))
        _reader = State(initialValue: NoteReaderModel(client: client))
    }

    var body: some View {
        TabView(selection: $selectedTab) {
            SearchReaderView(client: client)
                .tag(MainTab.notes)
                .tabItem { Label("Notes", systemImage: "doc.text") }
            CalendarView(client: client)
                .tag(MainTab.calendar)
                .tabItem { Label("Calendar", systemImage: "calendar") }
            GraphTabView(client: client)
                .tag(MainTab.graph)
                .tabItem { Label("Graph", systemImage: "network") }
            BrowseTabView(
                client: client,
                openNote: { raw in
                    selectedTab = .notes
                    openedNote = raw
                },
                openCalendar: { selectedTab = .calendar }
            )
                .tag(MainTab.browse)
                .tabItem { Label("Browse", systemImage: "folder") }
            TasksView(model: tasks)
                .tag(MainTab.tasks)
                .tabItem { Label("Tasks", systemImage: "checklist") }
            VoiceView(client: client)
                .tag(MainTab.voice)
                .tabItem { Label("Voice", systemImage: "mic") }
            SettingsTabView()
                .tag(MainTab.settings)
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
        .sheet(isPresented: Binding(get: { openedNote != nil }, set: { if !$0 { openedNote = nil } })) {
            NoteReaderView(model: reader, baseURL: client.baseURL)
                .task { if let openedNote { await reader.open(TrackID(openedNote)) } }
        }
    }
}

// MARK: - Browse tab

private enum MainTab: Hashable {
    case notes, calendar, graph, browse, tasks, voice, settings
}

private enum BrowsePane: String, CaseIterable, Identifiable {
    case hierarchy, tags, activity, history

    var id: String { rawValue }

    var title: String {
        switch self {
        case .hierarchy: return "Hierarchy"
        case .tags: return "Tags"
        case .activity: return "Activity"
        case .history: return "History"
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
    let openNote: (String) -> Void
    let openCalendar: () -> Void

    init(client: TrackClient, openNote: @escaping (String) -> Void = { _ in }, openCalendar: @escaping () -> Void = {}) {
        self.client = client
        self.openNote = openNote
        self.openCalendar = openCalendar
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
                HierarchyView(model: model) { raw in
                    selectedNote = raw
                    openNote(raw)
                }
            case .tags:
                TagView(model: model) { raw in
                    selectedNote = raw
                    openNote(raw)
                }
            case .activity:
                ActivityHeatmapView(model: model) { _ in openCalendar() }
            case .history:
                BrowseHistoryView { raw in
                    selectedNote = raw
                    openNote(raw)
                }
            }
            if let selectedNote {
                Divider()
                HStack(spacing: 8) {
                    Text("Selected: \(selectedNote)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    Spacer()
                    Button("Open") { openNote(selectedNote) }
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
    @AppStorage(TrackAppearance.fontSizeKey) private var fontSize = TrackAppearance.baseFontSize
    @AppStorage(TrackAppearance.previewFontSizeKey) private var previewFontSize = TrackAppearance.baseFontSize
    @AppStorage(TrackAppearance.contentWidthKey) private var contentWidthRaw: String?

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
                Picker("Content width", selection: contentWidthBinding) {
                    ForEach(ContentWidthMode.allCases, id: \.self) { mode in
                        Text(mode.label).tag(mode)
                    }
                }
                .pickerStyle(.radioGroup)
            } footer: {
                Text("Normal is 880 pt, Wide is 1280 pt, and Full uses the available window width.")
            }
            Section {
                Stepper(
                    value: fontSizeBinding,
                    in: TrackAppearance.fontSizeRange,
                    step: 1
                ) {
                    Text("Text size \(fontSize, specifier: "%.0f") pt")
                }
                Stepper(
                    value: previewFontSizeBinding,
                    in: TrackAppearance.fontSizeRange,
                    step: 1
                ) {
                    Text("Preview text size \(previewFontSize, specifier: "%.0f") pt")
                }
            } footer: {
                Text("Reader and preview text sizes are independent (13–32 pt).")
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

    private var fontSizeBinding: Binding<Double> {
        Binding(
            get: { min(max(fontSize, TrackAppearance.fontSizeRange.lowerBound), TrackAppearance.fontSizeRange.upperBound) },
            set: { fontSize = min(max($0, TrackAppearance.fontSizeRange.lowerBound), TrackAppearance.fontSizeRange.upperBound) }
        )
    }

    private var previewFontSizeBinding: Binding<Double> {
        Binding(
            get: { min(max(previewFontSize, TrackAppearance.fontSizeRange.lowerBound), TrackAppearance.fontSizeRange.upperBound) },
            set: { previewFontSize = min(max($0, TrackAppearance.fontSizeRange.lowerBound), TrackAppearance.fontSizeRange.upperBound) }
        )
    }

    private var contentWidthBinding: Binding<ContentWidthMode> {
        Binding(
            get: { ContentWidthMode(stored: contentWidthRaw) },
            set: { contentWidthRaw = $0.storedValue }
        )
    }
}
