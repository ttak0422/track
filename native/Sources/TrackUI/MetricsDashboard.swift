import Foundation
import Observation
import SwiftUI
import TrackAPI

@MainActor @Observable
final class MetricsDashboardModel {
    private(set) var response: MetricsDashboardResponse?
    private(set) var entities: [String] = []
    private(set) var metrics: [String] = []
    private(set) var error: String?
    private(set) var isLoading = false
    private var generation = 0

    func load(client: TrackClient?, spec: String, vault: String, from: String, to: String, entity: String, metric: String) async {
        generation += 1
        let token = generation
        response = nil
        error = nil
        isLoading = true
        defer { if token == generation { isLoading = false } }
        guard let client else { error = "Dashboard needs a connected workspace."; return }
        do {
            let fresh = try await client.renderMetricsDashboard(spec: spec, vault: vault, from: from, to: to, entity: entity, metric: metric)
            guard token == generation, !Task.isCancelled else { return }
            response = fresh
            entities = fresh.entities
            metrics = fresh.metrics
        } catch {
            guard token == generation, !Task.isCancelled else { return }
            self.error = (error as? APIError)?.message ?? error.localizedDescription
        }
    }
}

struct MetricsDashboardView: View {
    let spec: String
    let vault: String
    let client: TrackClient?
    @State private var model = MetricsDashboardModel()
    @State private var entity = ""
    @State private var metric = ""
    @State private var from = ""
    @State private var to = ""
    @State private var refresh = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ViewThatFits(in: .horizontal) {
                HStack { target; series; dates; actions }
                VStack(alignment: .leading) { HStack { target; series }; dates; actions }
            }
            .controlSize(.small)
            if model.isLoading { ProgressView("Loading dashboard…").controlSize(.small) }
            if let error = model.error {
                Label(error, systemImage: "exclamationmark.triangle")
                    .font(.caption).foregroundStyle(.secondary)
            }
            if let dashboard = model.response {
                Text(dashboard.title).trackSectionLabel()
                Text(dashboard.asof.isEmpty ? "No data in selected range" : "Latest sample: \(dashboard.asof)")
                    .font(.caption).foregroundStyle(.secondary)
                if dashboard.panels.isEmpty { Text("No panels configured").foregroundStyle(.secondary) }
                MetricsPanelLayout(grids: dashboard.panels.map(\.grid)) {
                    ForEach(dashboard.panels) { panel in MetricsPanelView(panel: panel) }
                }
            }
        }
        .task(id: [spec, vault, entity, metric, from, to, String(refresh)]) {
            await model.load(client: client, spec: spec, vault: vault, from: from, to: to, entity: entity, metric: metric)
        }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in refresh += 1 }
    }

    private var target: some View {
        Picker("Target", selection: $entity) {
            Text("All targets").tag("")
            ForEach(model.entities, id: \.self) { Text($0).tag($0) }
            if !entity.isEmpty && !model.entities.contains(entity) { Text(entity).tag(entity) }
        }
        .frame(maxWidth: 240)
    }

    private var series: some View {
        Picker("Metric", selection: $metric) {
            Text("All metrics").tag("")
            ForEach(model.metrics, id: \.self) { Text($0).tag($0) }
            if !metric.isEmpty && !model.metrics.contains(metric) { Text(metric).tag(metric) }
        }
        .frame(maxWidth: 240)
    }

    private var dates: some View {
        HStack {
            TextField("From YYYY-MM-DD", text: $from)
                .accessibilityLabel("From date, YYYY-MM-DD, inclusive")
            TextField("To YYYY-MM-DD", text: $to)
                .accessibilityLabel("To date, YYYY-MM-DD, inclusive")
        }
        .textFieldStyle(.roundedBorder)
        .frame(maxWidth: 320)
    }

    private var actions: some View {
        HStack {
            Button("Reset") { entity = ""; metric = ""; from = ""; to = ""; refresh += 1 }
            Button("Refresh", systemImage: "arrow.clockwise") { refresh += 1 }
                .help("Reread local vault data")
                .disabled(model.isLoading)
        }
        .buttonStyle(.borderless)
    }
}

/// Grafana's 24 columns with content-sized rows; narrow readers stack panels.
struct MetricsPanelLayout: Layout {
    let grids: [MetricsDashboardPanel.Grid]
    private let gap: CGFloat = 12

    func frames(width: CGFloat, heights: [CGFloat]) -> [CGRect] {
        if width < 600 {
            var y: CGFloat = 0
            return heights.map { height in
                defer { y += height + gap }
                return CGRect(x: 0, y: y, width: width, height: height)
            }
        }
        let row = zip(grids, heights).reduce(CGFloat(32)) { current, entry in
            max(current, (entry.1 + gap) / CGFloat(max(1, entry.0.h)))
        }
        let column = (width + gap) / 24
        return grids.map {
            CGRect(x: CGFloat($0.x) * column, y: CGFloat($0.y) * row,
                   width: CGFloat($0.w) * column - gap, height: CGFloat($0.h) * row - gap)
        }
    }

    private func frames(width: CGFloat, subviews: Subviews) -> [CGRect] {
        let heights = zip(grids, subviews).map { grid, view in
            let w = width < 600 ? width : CGFloat(grid.w) * (width + gap) / 24 - gap
            return view.sizeThatFits(ProposedViewSize(width: w, height: nil)).height
        }
        return frames(width: width, heights: heights)
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? 800
        return CGSize(width: width, height: frames(width: width, subviews: subviews).map(\.maxY).max() ?? 0)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for (view, frame) in zip(subviews, frames(width: bounds.width, subviews: subviews)) {
            view.place(at: CGPoint(x: bounds.minX + frame.minX, y: bounds.minY + frame.minY),
                       proposal: ProposedViewSize(frame.size))
        }
    }
}

private struct MetricsPanelView: View {
    let panel: MetricsDashboardPanel
    @State private var chartHeight: CGFloat
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.openURL) private var openURL

    init(panel: MetricsDashboardPanel) {
        self.panel = panel
        _chartHeight = State(initialValue: max(180, CGFloat(panel.grid.h) * 32 - 48))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Divider()
            Text(panel.title).trackSectionLabel()
            if let error = panel.error {
                Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(.secondary)
            } else if panel.type == "timeseries", let option = panel.echartsJSON {
                FigureHost(kind: .echarts(optionJSON: option), height: $chartHeight,
                           theme: colorScheme == .dark ? .dark : .light,
                           onLink: { if ["http", "https"].contains($0.scheme ?? "") { openURL($0) } },
                           echartsHeight: max(180, CGFloat(panel.grid.h) * 32 - 48))
                    .frame(height: chartHeight)
                FigureEvidenceView(items: FigureEvidence.parse(option))
            } else if panel.values.isEmpty {
                Text("No data in selected range").foregroundStyle(.secondary)
            } else if panel.type == "table" {
                table
            } else if panel.type == "stat" {
                ForEach(Array(panel.values.enumerated()), id: \.offset) { _, value in
                    VStack(alignment: .leading, spacing: 4) {
                        Text([value.entity, value.name].filter { !$0.isEmpty }.joined(separator: " · "))
                        Text(value.display).font(.title2).monospacedDigit()
                        Text(value.time).font(.caption).foregroundStyle(.secondary)
                        Text(value.state).font(.caption)
                    }
                }
            } else {
                Text("No chart data").foregroundStyle(.secondary)
            }
        }
        .font(.callout)
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private var table: some View {
        ScrollView(.horizontal) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 8) {
                GridRow {
                    Text("Target"); Text("Metric"); Text("Value"); Text("Sample time"); Text("State")
                }.font(.caption.bold())
                ForEach(Array(panel.values.enumerated()), id: \.offset) { _, value in
                    GridRow {
                        Text(value.entity); Text(value.name); Text(value.display).monospacedDigit()
                        Text(value.time); Text(value.state)
                    }
                }
            }
            .fixedSize(horizontal: true, vertical: false)
        }
    }
}
