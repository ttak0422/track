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

// MARK: - Workspace shell

/// A compact navigation dock beside one reading sheet. Visited workspaces
/// retain their state while the dock changes the visible surface.
private struct MainTabView: View {
    let client: TrackClient
    @State private var vaultScope: VaultScope
    @State private var tasks: TasksModel
    @State private var liveEvents: LiveEventPoller
    @State private var visitedTabs: Set<MainTab> = [.notes]
    @Environment(\.colorScheme) private var colorScheme
    @State private var selectedTab = MainTab.notes
    @State private var openedNote: String?
    @State private var reader: NoteReaderModel
    /// A day handed from the Browse activity heatmap to the Calendar tab, so a
    /// heatmap tap lands on the same day the calendar would select by hand.
    @State private var calendarDay: String?

    init(client: TrackClient) {
        self.client = client
        _vaultScope = State(initialValue: VaultScope(client: client))
        _tasks = State(initialValue: TasksModel(client: client))
        _liveEvents = State(initialValue: LiveEventPoller(baseURL: client.baseURL) {
            NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
        })
        _reader = State(initialValue: NoteReaderModel(client: client))
        _liveEvents = State(initialValue: LiveEventPoller(baseURL: client.baseURL) {
            NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
        })
    }

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            navigationDock
                .padding(.horizontal, 8)
                .padding(.top, 44)
            ZStack {
                // Keep visited surfaces alive so navigation preserves drafts,
                // search, and scroll position without loading every view at launch.
                ForEach(MainTab.allCases.filter { visitedTabs.contains($0) }, id: \.self) { tab in
                    workspace(tab)
                        .environment(\.trackWorkspaceActive, selectedTab == tab)
                        .opacity(selectedTab == tab ? 1 : 0)
                        .allowsHitTesting(selectedTab == tab)
                        .disabled(selectedTab != tab)
                        .accessibilityHidden(selectedTab != tab)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(palette.panel)
        }
        .foregroundStyle(palette.text)
        .background(palette.bg)
        .tint(palette.mark)
        .environment(liveEvents)
        .environment(vaultScope)
        .toolbar { VaultSwitcher(model: vaultScope) }
        .task { await vaultScope.reload() }
        .onChange(of: selectedTab) { _, tab in visitedTabs.insert(tab) }
        .task { liveEvents.start() }
        .onDisappear { liveEvents.stop() }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            Task { await liveEvents.refresh() }
        }
        .sheet(isPresented: Binding(get: { openedNote != nil }, set: { if !$0 { openedNote = nil } })) {
            NoteReaderView(model: reader, baseURL: client.baseURL)
                .task { if let openedNote { await reader.open(TrackID(openedNote)) } }
        }
    }

    private var palette: TrackTheme { TrackTheme.palette(for: colorScheme) }

    private var navigationDock: some View {
        VStack(spacing: 6) {
            Rectangle().fill(palette.mark).frame(width: 18, height: 18)
                .padding(9)
                .accessibilityHidden(true)
            ForEach(MainTab.allCases.filter { $0 != .settings }, id: \.self) { tab in
                dockButton(tab)
            }
            Divider().overlay(palette.line)
            dockButton(.settings)
        }
        .padding(4)
        .frame(width: 44)
        .background(palette.panel, in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(palette.line, lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Workspace navigation")
    }

    private func dockButton(_ tab: MainTab) -> some View {
        Button { selectedTab = tab } label: {
            Image(systemName: tab.symbol)
                .font(.system(size: 18, weight: .regular))
                .frame(width: 36, height: 36)
                .foregroundStyle(selectedTab == tab ? palette.text : palette.muted)
                .background(selectedTab == tab ? palette.panelSoft : .clear,
                            in: RoundedRectangle(cornerRadius: 6))
                .overlay(alignment: .leading) {
                    if selectedTab == tab { Rectangle().fill(palette.mark).frame(width: 2, height: 18) }
                }
        }
        .buttonStyle(.plain)
        .help(tab.title)
        .accessibilityLabel(tab.title)
        .accessibilityAddTraits(selectedTab == tab ? .isSelected : [])
    }

    @ViewBuilder
    private func workspace(_ tab: MainTab) -> some View {
        switch tab {
        case .notes: SearchReaderView(client: client)
        case .calendar: CalendarView(client: client, initialDay: calendarDay)
        case .graph: GraphTabView(client: client)
        case .browse:
            BrowseTabView(client: client, openNote: { raw in
                selectedTab = .notes
                openedNote = raw
            }, openCalendar: { day in
                calendarDay = day
                selectedTab = .calendar
            })
        case .tasks: TasksView(model: tasks)
        case .voice: VoiceView(client: client)
        case .settings: SettingsTabView()
        }
    }

}

// MARK: - Browse tab

private enum MainTab: Hashable, CaseIterable {
    case notes, calendar, graph, browse, tasks, voice, settings

    var title: String {
        switch self {
        case .notes: "Notes"
        case .calendar: "Calendar"
        case .graph: "Graph"
        case .browse: "Browse"
        case .tasks: "Tasks"
        case .voice: "Voice"
        case .settings: "Settings"
        }
    }

    var symbol: String {
        switch self {
        case .notes: "doc.text"
        case .calendar: "calendar"
        case .graph: "network"
        case .browse: "folder"
        case .tasks: "checklist"
        case .voice: "mic"
        case .settings: "gearshape"
        }
    }
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
    let openCalendar: (String) -> Void

    init(client: TrackClient, openNote: @escaping (String) -> Void = { _ in }, openCalendar: @escaping (String) -> Void = { _ in }) {
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
                ActivityHeatmapView(model: model) { day in openCalendar(day) }
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
                Text("System follows the macOS appearance.")
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
