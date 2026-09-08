import SwiftUI
import TrackAPI

private struct GraphPreview: Identifiable {
    let node: GraphNode
    var id: String { node.noteID.raw }
}

// Graph surfaces mirroring the web's full graph and one-hop local graph
// (docs/spec/web.md). This native MVP renders nodes as a sortable title list —
// no Canvas force-directed layout. The full graph lists nodes by link degree
// (descending); the local graph shows the center plus its one-hop neighbours.
// Node taps hand the raw note id string to `onSelect`; TrackID resolution is
// the caller's job.

// MARK: - Graph model

@MainActor
@Observable
public final class GraphModel {
    public private(set) var full: Graph?
    public private(set) var local: Graph?
    public private(set) var isLoading = false
    public private(set) var error: String?

    private let client: TrackClient

    public init(client: TrackClient) {
        self.client = client
    }

    public func loadFull() async {
        isLoading = true
        defer { isLoading = false }
        error = nil
        do {
            full = try await client.getGraph().graph
        } catch {
            self.error = error.localizedDescription
        }
    }

    public func loadLocal(id: TrackID) async {
        isLoading = true
        defer { isLoading = false }
        error = nil
        do {
            local = try await client.getLocalGraph(id).graph
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Undirected link degree of `id` in `graph` (edge count touching it).
    public static func degree(of id: TrackID, in graph: Graph) -> Int {
        degree(of: id, in: graph.edges)
    }

    /// Undirected link degree of `id` in an edge list.
    public static func degree(of id: TrackID, in edges: [GraphEdge]) -> Int {
        var count = 0
        for edge in edges {
            if edge.sourceID == id || edge.targetID == id { count += 1 }
        }
        return count
    }

    /// Nodes ordered by degree descending, ties broken by title ascending.
    public static func nodesByDegree(_ graph: Graph) -> [GraphNode] {
        nodesByDegree(graph.nodes, edges: graph.edges)
    }

    /// Nodes ordered by degree descending, ties broken by title ascending.
    public static func nodesByDegree(_ nodes: [GraphNode], edges: [GraphEdge]) -> [GraphNode] {
        let degrees = degreeMap(edges)
        return nodes.sorted { lhs, rhs in
            let a = degrees[lhs.noteID] ?? 0
            let b = degrees[rhs.noteID] ?? 0
            if a != b { return a > b }
            return lhs.title < rhs.title
        }
    }

    // MARK: - Overview safeguard (web overviewGraph)

    /// How many nodes the whole-vault overview will draw. The bound comes from
    /// what a screen can show, not from what the layout can compute (web
    /// `overviewGraph.OVERVIEW_NODE_CAP`): a node-link picture stops saying
    /// anything well before the renderer stops keeping up. Past the cap the
    /// view names what it left out.
    public static let overviewNodeCap = 1000

    /// The whole-vault graph reduced to the part worth drawing, plus how many
    /// notes were left out so the view can say so.
    public struct OverviewGraph {
        public let nodes: [GraphNode]
        public let edges: [GraphEdge]
        public let hidden: Int
    }

    /// Reduce the whole-vault graph to the part worth drawing (web
    /// `overviewGraph`): a note with no link is not in the link graph, so it
    /// is not drawn; beyond the cap the best-connected notes are kept —
    /// cutting by degree keeps the structure an overview is for. A self link
    /// and a link to a note the payload never delivered draw nothing and are
    /// not counted. Ties break on note id so the same vault always yields the
    /// same slice.
    public static func overview(_ graph: Graph, cap: Int = overviewNodeCap) -> OverviewGraph {
        let nodes = graph.nodes
        let known = Set(nodes.map(\.noteID))
        var edges = graph.edges.filter {
            $0.sourceID != $0.targetID && known.contains($0.sourceID) && known.contains($0.targetID)
        }
        var degrees = degreeMap(edges)
        var kept = nodes.filter { degrees[$0.noteID] != nil }
        if kept.count > cap {
            kept.sort {
                let a = degrees[$0.noteID] ?? 0
                let b = degrees[$1.noteID] ?? 0
                if a != b { return a > b }
                return $0.noteID.raw < $1.noteID.raw
            }
            kept = Array(kept.prefix(cap))
            let inSlice = Set(kept.map(\.noteID))
            edges = edges.filter { inSlice.contains($0.sourceID) && inSlice.contains($0.targetID) }
            // The cut can strand a node whose only neighbours fell outside
            // it; one pass drops those so the overview never draws loose dots.
            degrees = degreeMap(edges)
            kept = kept.filter { degrees[$0.noteID] != nil }
        }
        return OverviewGraph(nodes: kept, edges: edges, hidden: nodes.count - kept.count)
    }

    /// Incident-edge counts per node. Only nodes with at least one edge
    /// appear, so membership alone answers "is this note in the link graph".
    private static func degreeMap(_ edges: [GraphEdge]) -> [TrackID: Int] {
        var degrees: [TrackID: Int] = [:]
        for edge in edges {
            degrees[edge.sourceID, default: 0] += 1
            degrees[edge.targetID, default: 0] += 1
        }
        return degrees
    }
}

// MARK: - Full graph view

public struct GraphFullView: View {
    @Bindable var model: GraphModel
    let onSelect: (String) -> Void
    @Environment(\.colorScheme) private var colorScheme
    @State private var selectedID: TrackID?
    @State private var centerShown = true
    @State private var hoveredNode: GraphPreview?

    public init(model: GraphModel, onSelect: @escaping (String) -> Void = { _ in }) {
        self.model = model
        self.onSelect = onSelect
    }

    public var body: some View {
        Group {
            if model.isLoading && model.full == nil {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = model.error, model.full == nil {
                ContentUnavailableView("Could not load graph", systemImage: "exclamationmark.triangle", description: Text(error))
            } else if let graph = model.full {
                // The whole vault is more than a picture can hold, so the
                // view draws the link graph's connected part up to a cap and
                // says what it left out (overviewGraph).
                let shown = GraphModel.overview(graph)
                let nodes = GraphModel.nodesByDegree(shown.nodes, edges: shown.edges)
                let mark = TrackTheme.palette(for: colorScheme).mark
                List {
                    Section {
                        ForEach(nodes, id: \.noteID) { node in
                            let isCenter = centerShown && Self.isCenter(node, in: graph)
                            let degree = GraphModel.degree(of: node.noteID, in: shown.edges)
                            Button {
                                selectedID = node.noteID
                                onSelect(node.noteID.raw)
                            } label: {
                                HStack(spacing: 8) {
                                    Circle()
                                        .fill(isCenter ? mark : Color.clear)
                                        .overlay {
                                            Circle().stroke(isCenter ? mark : Color.secondary, lineWidth: 1)
                                        }
                                        .frame(width: Self.radius(for: node, degree: degree, isCenter: isCenter) * 2,
                                               height: Self.radius(for: node, degree: degree, isCenter: isCenter) * 2)
                                    Text(Self.label(for: node, in: nodes, selectedID: selectedID))
                                        .lineLimit(1)
                                        .truncationMode(.tail)
                                        .foregroundStyle(isCenter ? mark : .primary)
                                        .fontWeight(isCenter ? .medium : .regular)
                                    Spacer()
                                    Text("\(degree)")
                                        .font(.caption).foregroundStyle(.secondary)
                                }
                            }
                            .buttonStyle(.plain)
                            .contentShape(Rectangle())
                            .onHover { hovering in hoveredNode = hovering ? GraphPreview(node: node) : nil }
                            .popover(item: $hoveredNode, attachmentAnchor: .point(.trailing), arrowEdge: .leading) { preview in
                                Self.preview(preview.node, degree: GraphModel.degree(of: preview.node.noteID, in: shown.edges))
                            }
                        }
                    } header: {
                        VStack(alignment: .leading, spacing: 6) {
                            Text(Self.caption(drawn: nodes.count, hidden: shown.hidden))
                            HStack(spacing: 12) {
                                Button(selectedID == nil ? "選択なし" : "選択を解除") { selectedID = nil }
                                    .disabled(selectedID == nil)
                                Button(centerShown ? "中心を解除" : "中心を表示") { centerShown.toggle() }
                            }
                            .font(.caption)
                        }
                    }
                }
            }
        }
        .task { await model.loadFull() }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard !model.isLoading else { return }
            Task { await model.loadFull() }
        }
    }

    private static func isCenter(_ node: GraphNode, in graph: Graph) -> Bool {
        node.center == true || node.noteID == graph.centerID
    }

    private static func radius(for node: GraphNode, degree: Int, isCenter: Bool) -> CGFloat {
        let grade = node.size.flatMap { (1...5).contains($0) ? [4, 6, 8.5, 12, 17][$0 - 1] : nil }
        let value = grade ?? (6 + min(8, sqrt(Double(degree)) * 2))
        return CGFloat(isCenter ? max(10, value) : value)
    }

    private static func label(for node: GraphNode, in nodes: [GraphNode], selectedID: TrackID?) -> String {
        let index = nodes.firstIndex { $0.noteID == node.noteID } ?? nodes.count
        return index < 12 || node.center == true || selectedID == node.noteID ? node.title : "ノード \(node.noteID.raw)"
    }

    /// How many notes the view is showing and — since the overview draws only
    /// the linked part of the vault up to a cap — how many it left out (web
    /// `graphCountCaption`). One line, because both halves answer the same
    /// question.
    private static func caption(drawn: Int, hidden: Int) -> String {
        hidden > 0 ? "\(drawn)件描画・\(hidden)件未描画" : "\(drawn)件"
    }

    @ViewBuilder
    fileprivate static func preview(_ node: GraphNode, degree: Int) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(node.title).font(.headline).lineLimit(2)
            Text("次数 \(degree)").font(.caption).foregroundStyle(.secondary)
            Text("本文プレビューはノードデータに含まれていません")
                .font(.caption).foregroundStyle(.secondary)
        }
        .padding(10)
        .frame(maxWidth: 260, alignment: .leading)
    }
}

// MARK: - Local graph view

public struct LocalGraphView: View {
    @Bindable var model: GraphModel
    let centerID: TrackID
    let onSelect: (String) -> Void
    @Environment(\.colorScheme) private var colorScheme
    @State private var selectedID: TrackID?
    @State private var centerShown = true
    @State private var hoveredNode: GraphPreview?

    public init(model: GraphModel, centerID: TrackID, onSelect: @escaping (String) -> Void = { _ in }) {
        self.model = model
        self.centerID = centerID
        self.onSelect = onSelect
    }

    public var body: some View {
        Group {
            if model.isLoading && model.local == nil {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = model.error, model.local == nil {
                ContentUnavailableView("Could not load graph", systemImage: "exclamationmark.triangle", description: Text(error))
            } else if let graph = model.local {
                let neighbors = Self.neighbors(of: graph)
                let mark = TrackTheme.palette(for: colorScheme).mark
                VStack(alignment: .leading, spacing: 4) {
                    Text(Self.caption(drawn: graph.nodes.count, hidden: 0))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text("Center").font(.caption).foregroundStyle(.secondary)
                    Button {
                        selectedID = graph.centerID
                        onSelect(graph.centerID.raw)
                    } label: {
                        HStack(spacing: 8) {
                            Circle().fill(centerShown ? mark : Color.clear)
                                .overlay { Circle().stroke(centerShown ? mark : Color.secondary, lineWidth: 1) }
                                .frame(width: Self.radius(for: graph.nodes.first { $0.noteID == graph.centerID }, graph: graph, isCenter: centerShown) * 2,
                                       height: Self.radius(for: graph.nodes.first { $0.noteID == graph.centerID }, graph: graph, isCenter: centerShown) * 2)
                            Text(Self.title(for: graph.centerID, in: graph))
                                .foregroundStyle(centerShown ? mark : .primary)
                                .fontWeight(.medium)
                        }
                    }
                    .buttonStyle(.plain)
                    .onHover { hovering in
                        hoveredNode = hovering ? graph.nodes.first { $0.noteID == graph.centerID }.map(GraphPreview.init) : nil
                    }
                    .popover(item: $hoveredNode, attachmentAnchor: .point(.trailing), arrowEdge: .leading) { node in
                        GraphFullView.preview(node.node, degree: GraphModel.degree(of: node.node.noteID, in: graph))
                    }
                    HStack(spacing: 12) {
                        Button(selectedID == nil ? "選択なし" : "選択を解除") { selectedID = nil }
                            .disabled(selectedID == nil)
                        Button(centerShown ? "中心を解除" : "中心を表示") { centerShown.toggle() }
                    }
                    .font(.caption)
                    if !neighbors.isEmpty {
                        Divider()
                        Text("Linked").font(.caption).foregroundStyle(.secondary)
                        ForEach(neighbors, id: \.noteID) { node in
                            Button {
                                selectedID = node.noteID
                                onSelect(node.noteID.raw)
                            } label: {
                                HStack(spacing: 8) {
                                    Circle().stroke(Color.secondary, lineWidth: 1)
                                        .frame(width: Self.radius(for: node, graph: graph, isCenter: false) * 2,
                                               height: Self.radius(for: node, graph: graph, isCenter: false) * 2)
                                    Text(node.title).lineLimit(1).truncationMode(.tail)
                                    Spacer()
                                    Text("\(GraphModel.degree(of: node.noteID, in: graph))")
                                        .font(.caption).foregroundStyle(.secondary)
                                }
                            }
                                .buttonStyle(.plain)
                                .onHover { hovering in hoveredNode = hovering ? GraphPreview(node: node) : nil }
                                .popover(item: $hoveredNode, attachmentAnchor: .point(.trailing), arrowEdge: .leading) { preview in
                                    GraphFullView.preview(preview.node, degree: GraphModel.degree(of: preview.node.noteID, in: graph))
                                }
                        }
                    }
                }
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task { await model.loadLocal(id: centerID) }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard !model.isLoading else { return }
            Task { await model.loadLocal(id: centerID) }
        }
    }

    private static func neighbors(of graph: Graph) -> [GraphNode] {
        graph.nodes.filter { $0.noteID != graph.centerID && $0.center != true }
    }

    private static func title(for id: TrackID, in graph: Graph) -> String {
        graph.nodes.first { $0.noteID == id }?.title ?? id.raw
    }

    private static func radius(for node: GraphNode?, graph: Graph, isCenter: Bool) -> CGFloat {
        guard let node else { return isCenter ? 10 : 6 }
        let grade = node.size.flatMap { (1...5).contains($0) ? [4, 6, 8.5, 12, 17][$0 - 1] : nil }
        let value = grade ?? (6 + min(8, sqrt(Double(GraphModel.degree(of: node.noteID, in: graph))) * 2))
        return CGFloat(isCenter ? max(10, value) : value)
    }

    private static func caption(drawn: Int, hidden: Int) -> String {
        hidden > 0 ? "\(drawn)件描画・\(hidden)件未描画" : "\(drawn)件"
    }
}
