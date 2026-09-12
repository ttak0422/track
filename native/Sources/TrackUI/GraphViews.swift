import SwiftUI
import TrackAPI

private struct GraphPreview: Identifiable {
    let node: GraphNode
    var id: String { node.noteID.raw }
}

// Graph surfaces mirroring the web's full graph and one-hop local graph
// (docs/spec/web.md). Both views offer the pannable/zoomable GraphCanvas
// (force-directed node-link drawing, web GraphCanvas parity) with the
// degree-ranked title list kept as a List fallback. Node taps hand the raw
// note id string to `onSelect`; TrackID resolution is the caller's job.

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
    @State private var useCanvas = true

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
                Picker("Graph style", selection: $useCanvas) {
                    Text("Canvas").tag(true)
                    Text("List").tag(false)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .padding(.horizontal, 12)
                .padding(.vertical, 4)
                if useCanvas {
                    GraphCanvas(nodes: shown.nodes, edges: shown.edges, selectedID: selectedID) { raw in
                        selectedID = shown.nodes.first { $0.noteID.raw == raw }?.noteID
                        onSelect(raw)
                    }
                    Text(Self.caption(drawn: nodes.count, hidden: shown.hidden))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 12)
                        .padding(.bottom, 4)
                } else {
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

// MARK: - Graph canvas (force-directed)

/// A pannable/zoomable node-link canvas (web GraphCanvas parity): the same
/// nodes/edges the list views show, laid out by a small deterministic force
/// simulation (repulsion + springs + gravity) and drawn in one SwiftUI
/// Canvas. Node taps select via `onSelect`; labels mirror the list's rule
/// (first 12 by degree, center, selected).
struct GraphCanvas: View {
    let nodes: [GraphNode]
    let edges: [GraphEdge]
    let centerID: TrackID?
    let selectedID: TrackID?
    let onSelect: (String) -> Void
    @Environment(\.colorScheme) private var colorScheme
    @State private var scale: CGFloat = 1
    @State private var baseScale: CGFloat = 1
    @State private var offset: CGSize = .zero
    @State private var lastDrag: CGSize = .zero

    /// Force layout stays interactive: past this many nodes only the
    /// best-connected slice is drawn (the list keeps the full ranking).
    static let nodeCap = 300

    init(nodes: [GraphNode], edges: [GraphEdge], centerID: TrackID? = nil, selectedID: TrackID? = nil, onSelect: @escaping (String) -> Void = { _ in }) {
        self.nodes = nodes
        self.edges = edges
        self.centerID = centerID
        self.selectedID = selectedID
        self.onSelect = onSelect
    }

    /// The drawn slice: best-connected first, edges reduced to it.
    var drawn: (nodes: [GraphNode], edges: [GraphEdge]) {
        let ranked = GraphModel.nodesByDegree(nodes, edges: edges)
        let kept = Array(ranked.prefix(Self.nodeCap))
        let ids = Set(kept.map(\.noteID))
        let keptEdges = edges.filter { ids.contains($0.sourceID) && ids.contains($0.targetID) }
        return (kept, keptEdges)
    }

    var body: some View {
        let (kept, keptEdges) = drawn
        let positions = GraphCanvasLayout.layout(nodes: kept, edges: keptEdges, centerID: centerID)
        let degrees = Dictionary(uniqueKeysWithValues: kept.map { ($0.noteID, GraphModel.degree(of: $0.noteID, in: keptEdges)) })
        GeometryReader { proxy in
            let size = proxy.size
            ZStack {
                Canvas { ctx, _ in
                    let t = transform(for: size, positions: positions)
                    for edge in keptEdges {
                        guard let a = positions[edge.sourceID].map({ t.apply($0) }),
                              let b = positions[edge.targetID].map({ t.apply($0) }) else { continue }
                        var path = Path()
                        path.move(to: a)
                        path.addLine(to: b)
                        ctx.stroke(path, with: .color(.secondary.opacity(0.5)), lineWidth: 0.75)
                    }
                    let mark = TrackTheme.palette(for: colorScheme).mark
                    for node in kept {
                        guard let p = positions[node.noteID].map({ t.apply($0) }) else { continue }
                        let degree = degrees[node.noteID] ?? 0
                        let r = GraphCanvasLayout.radius(for: node, degree: degree, isCenter: node.noteID == centerID) * t.scale
                        let rect = CGRect(x: p.x - r, y: p.y - r, width: r * 2, height: r * 2)
                        if node.noteID == centerID {
                            ctx.fill(Path(ellipseIn: rect), with: .color(mark))
                        } else {
                            ctx.fill(Path(ellipseIn: rect), with: .color(.secondary.opacity(0.35)))
                        }
                        if node.noteID == selectedID {
                            ctx.stroke(Path(ellipseIn: rect.insetBy(dx: -3, dy: -3)), with: .color(mark), lineWidth: 2)
                        }
                    }
                }
                .contentShape(Rectangle())
                ForEach(labeledNodes(kept), id: \.noteID) { node in
                    if let p = positions[node.noteID].map({ transform(for: size, positions: positions).apply($0) }) {
                        Text(node.title)
                            .font(.caption2)
                            .lineLimit(1)
                            .padding(.horizontal, 4)
                            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 4))
                            .position(x: p.x, y: p.y - 12)
                            .allowsHitTesting(false)
                    }
                }
            }
            .gesture(pan.simultaneously(with: zoom))
            .onTapGesture(count: 1, coordinateSpace: .local) { location in
                tap(at: location, size: size, positions: positions)
            }
            .overlay(alignment: .topTrailing) {
                HStack(spacing: 8) {
                    if kept.count < nodes.count {
                        Text("\(kept.count) drawn・\(nodes.count - kept.count) hidden")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    Button("Reset view") {
                        scale = 1
                        baseScale = 1
                        offset = .zero
                    }
                    .buttonStyle(.plain)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
                .padding(8)
            }
        }
    }

    private var pan: some Gesture {
        DragGesture()
            .onChanged { value in
                offset = CGSize(width: offset.width + value.translation.width - lastDrag.width,
                                height: offset.height + value.translation.height - lastDrag.height)
                lastDrag = value.translation
            }
            .onEnded { _ in lastDrag = .zero }
    }

    private var zoom: some Gesture {
        MagnifyGesture()
            .onChanged { scale = min(4, max(0.3, baseScale * $0.magnification)) }
            .onEnded { _ in baseScale = scale }
    }

    private func labeledNodes(_ kept: [GraphNode]) -> [GraphNode] {
        kept.enumerated().compactMap { index, node in
            (index < 12 || node.noteID == centerID || node.noteID == selectedID) ? node : nil
        }
    }

    private struct CanvasTransform {
        let scale: CGFloat
        let origin: CGPoint
        func apply(_ p: CGPoint) -> CGPoint {
            CGPoint(x: origin.x + p.x * scale, y: origin.y + p.y * scale)
        }
        func invert(_ p: CGPoint) -> CGPoint {
            CGPoint(x: (p.x - origin.x) / scale, y: (p.y - origin.y) / scale)
        }
    }

    private func transform(for size: CGSize, positions: [TrackID: CGPoint]) -> CanvasTransform {
        let xs = positions.values.map(\.x)
        let ys = positions.values.map(\.y)
        let minX = xs.min() ?? -1, maxX = xs.max() ?? 1
        let minY = ys.min() ?? -1, maxY = ys.max() ?? 1
        let span = max(maxX - minX, maxY - minY, 0.001)
        let base = min(size.width, size.height) * 0.42 / span
        let s = base * scale
        let cx = (minX + maxX) / 2, cy = (minY + maxY) / 2
        let origin = CGPoint(x: size.width / 2 - cx * s + offset.width,
                             y: size.height / 2 - cy * s + offset.height)
        return CanvasTransform(scale: s, origin: origin)
    }

    private func tap(at location: CGPoint, size: CGSize, positions: [TrackID: CGPoint]) {
        let t = transform(for: size, positions: positions)
        let local = t.invert(location)
        var best: GraphNode?
        var bestDist = CGFloat.greatestFiniteMagnitude
        for node in drawn.nodes {
            guard let p = positions[node.noteID] else { continue }
            let d = hypot(p.x - local.x, p.y - local.y)
            if d < bestDist { bestDist = d; best = node }
        }
        guard let node = best else { return }
        let degree = GraphModel.degree(of: node.noteID, in: drawn.edges)
        let r = GraphCanvasLayout.radius(for: node, degree: degree, isCenter: node.noteID == centerID)
        if bestDist <= r + 12 / t.scale {
            onSelect(node.noteID.raw)
        }
    }
}

/// Deterministic force layout for GraphCanvas: circle start (index order),
/// then repulsion + edge springs + weak gravity with damping. The center node
/// of a local graph stays pinned at the origin.
enum GraphCanvasLayout {
    static func layout(nodes: [GraphNode], edges: [GraphEdge], centerID: TrackID?) -> [TrackID: CGPoint] {
        let ordered = nodes.sorted { $0.noteID.raw < $1.noteID.raw }
        let n = ordered.count
        guard n > 0 else { return [:] }
        if n == 1 { return [ordered[0].noteID: .zero] }
        var pos: [TrackID: CGPoint] = [:]
        for (i, node) in ordered.enumerated() {
            let a = 2 * Double.pi * Double(i) / Double(n)
            pos[node.noteID] = CGPoint(x: cos(a), y: sin(a))
        }
        var vel: [TrackID: CGVector] = Dictionary(uniqueKeysWithValues: ordered.map { ($0.noteID, CGVector(dx: 0, dy: 0)) })
        let ticks = n > 250 ? 40 : 100
        let rest = 1.1
        for _ in 0..<ticks {
            var force: [TrackID: CGVector] = Dictionary(uniqueKeysWithValues: ordered.map { ($0.noteID, CGVector(dx: 0, dy: 0)) })
            for i in 0..<n {
                for j in (i + 1)..<n {
                    let a = ordered[i].noteID, b = ordered[j].noteID
                    let pa = pos[a] ?? .zero, pb = pos[b] ?? .zero
                    var dx = pa.x - pb.x, dy = pa.y - pb.y
                    var dist2 = dx * dx + dy * dy
                    if dist2 < 0.0001 {
                        dx = 0.01 * Double(i - j); dy = 0.01 * Double(j - i)
                        dist2 = dx * dx + dy * dy
                    }
                    let dist = sqrt(dist2)
                    let f = min(0.9 / dist2, 2)
                    let fx = f * dx / dist, fy = f * dy / dist
                    force[a]?.dx += fx; force[a]?.dy += fy
                    force[b]?.dx -= fx; force[b]?.dy -= fy
                }
            }
            for e in edges {
                guard pos[e.sourceID] != nil, pos[e.targetID] != nil else { continue }
                let a = e.sourceID, b = e.targetID
                let dx = (pos[b]?.x ?? 0) - (pos[a]?.x ?? 0)
                let dy = (pos[b]?.y ?? 0) - (pos[a]?.y ?? 0)
                let dist = max(sqrt(dx * dx + dy * dy), 0.001)
                let f = 0.03 * (dist - rest)
                let fx = f * dx / dist, fy = f * dy / dist
                force[a]?.dx += fx; force[a]?.dy += fy
                force[b]?.dx -= fx; force[b]?.dy -= fy
            }
            for node in ordered {
                let id = node.noteID
                if id == centerID { pos[id] = .zero; vel[id] = CGVector(dx: 0, dy: 0); continue }
                var v = vel[id] ?? CGVector(dx: 0, dy: 0)
                var f = force[id] ?? CGVector(dx: 0, dy: 0)
                let p = pos[id] ?? .zero
                f.dx += -0.02 * p.x; f.dy += -0.02 * p.y
                v.dx = (v.dx + f.dx) * 0.82; v.dy = (v.dy + f.dy) * 0.82
                vel[id] = v
                pos[id] = CGPoint(x: p.x + v.dx, y: p.y + v.dy)
            }
        }
        return pos
    }

    /// Node radius in layout units, mirroring the list's grade/degree sizing.
    static func radius(for node: GraphNode, degree: Int, isCenter: Bool) -> CGFloat {
        let grade = node.size.flatMap { (1...5).contains($0) ? [0.05, 0.075, 0.105, 0.15, 0.21][$0 - 1] : nil }
        let value = grade ?? (0.07 + min(0.1, sqrt(Double(degree)) * 0.025))
        return isCenter ? max(0.12, value) : value
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
    @State private var useCanvas = true

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
                    Picker("Graph style", selection: $useCanvas) {
                        Text("Canvas").tag(true)
                        Text("List").tag(false)
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    if useCanvas {
                        GraphCanvas(nodes: graph.nodes, edges: graph.edges, centerID: graph.centerID, selectedID: selectedID) { raw in
                            selectedID = graph.nodes.first { $0.noteID.raw == raw }?.noteID
                            onSelect(raw)
                        }
                        .frame(minHeight: 280)
                    }
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
