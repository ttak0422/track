import SwiftUI
import TrackAPI

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

    private var graphRequest = UUID()
    let client: TrackClient

    public init(client: TrackClient) {
        self.client = client
    }

    public func loadFull(vault: String = "") async {
        let request = UUID()
        graphRequest = request
        full = nil
        isLoading = true
        defer { if request == graphRequest { isLoading = false } }
        error = nil
        do {
            let response = try await client.getGraph(vault: vault)
            guard request == graphRequest, !Task.isCancelled else { return }
            full = response.graph
        } catch {
            guard request == graphRequest, !Task.isCancelled else { return }
            self.error = error.localizedDescription
        }
    }

    public func loadLocal(id: TrackID) async {
        let request = UUID()
        graphRequest = request
        local = nil
        isLoading = true
        defer { if request == graphRequest { isLoading = false } }
        error = nil
        do {
            let graph = try await client.getLocalGraph(id).graph
            guard request == graphRequest, !Task.isCancelled else { return }
            local = graph
        } catch {
            guard request == graphRequest, !Task.isCancelled else { return }
            self.error = error.localizedDescription
        }
    }

    public static func noteID(for node: GraphNode, vault: String = "") -> TrackID {
        node.noteID.raw.contains("~") ? node.noteID : TrackID.qualify(vault: node.vault ?? vault, id: node.noteID.raw)
    }

    /// Keep the local center/selection reachable even below the degree cut.
    public static func canvasSlice(nodes: [GraphNode], edges: [GraphEdge], centerID: TrackID? = nil, selectedID: TrackID? = nil, cap: Int = 300) -> OverviewGraph {
        let required = nodes.filter { $0.noteID == centerID || $0.noteID == selectedID }
        let requiredIDs = Set(required.map(\.noteID))
        let ranked = nodesByDegree(nodes, edges: edges).filter { !requiredIDs.contains($0.noteID) }
        let kept = Array((required + ranked).prefix(max(required.count, cap)))
        let ids = Set(kept.map(\.noteID))
        return OverviewGraph(nodes: kept, edges: edges.filter { ids.contains($0.sourceID) && ids.contains($0.targetID) }, hidden: nodes.count - kept.count)
    }

    /// Screen-space hit testing is shared by clicks and hover after pan/zoom.
    public static func hitNode(at point: CGPoint, positions: [TrackID: CGPoint], radii: [TrackID: CGFloat]) -> TrackID? {
        positions.compactMap { id, position -> (TrackID, CGFloat)? in
            let distance = hypot(position.x - point.x, position.y - point.y)
            return distance <= max(8, radii[id] ?? 0) + 4 ? (id, distance) : nil
        }.min { $0.1 < $1.1 }?.0
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
    @Environment(VaultScope.self) private var vaultScope: VaultScope?
    @Bindable var model: GraphModel
    let onSelect: (String) -> Void
    @Environment(\.colorScheme) private var colorScheme
    @State private var selectedID: TrackID?
    @State private var centerShown = true
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
                let nodes = GraphModel.nodesByDegree(graph)
                let canvasNodes = shown.nodes + graph.nodes.filter { $0.noteID == selectedID && !shown.nodes.contains(where: { $0.noteID == selectedID }) }
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
                    GraphCanvas(nodes: canvasNodes, edges: graph.edges, centerID: centerShown ? selectedID : nil, selectedID: selectedID, client: model.client, vault: vaultScope?.scope ?? "") { raw in
                        selectedID = graph.nodes.first { $0.noteID.raw == raw }?.noteID
                        if let node = graph.nodes.first(where: { $0.noteID.raw == raw }) { onSelect(GraphModel.noteID(for: node, vault: vaultScope?.scope ?? "").raw) }
                    }
                    Text(Self.caption(drawn: min(shown.nodes.count, GraphCanvas.nodeCap), hidden: graph.nodes.count - min(shown.nodes.count, GraphCanvas.nodeCap)) + " · List includes every note")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 12)
                        .padding(.bottom, 4)
                } else {
                List {
                    Section {
                        ForEach(nodes, id: \.noteID) { node in
                            let isCenter = centerShown && Self.isCenter(node, in: graph)
                            let degree = GraphModel.degree(of: node.noteID, in: graph.edges)
                            Button {
                                selectedID = node.noteID
                                onSelect(GraphModel.noteID(for: node, vault: vaultScope?.scope ?? "").raw)
                            } label: {
                                HStack(spacing: 8) {
                                    Circle()
                                        .fill(isCenter ? mark : Color.clear)
                                        .overlay {
                                            Circle().stroke(isCenter ? mark : Color.secondary, lineWidth: 1)
                                        }
                                        .frame(width: Self.radius(for: node, degree: degree, isCenter: isCenter) * 2,
                                               height: Self.radius(for: node, degree: degree, isCenter: isCenter) * 2)
                                    Text(node.title)
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
                            .notePreview(client: model.client, id: GraphModel.noteID(for: node, vault: vaultScope?.scope ?? "")) { onSelect($0.raw) }
                            .contextMenu {
                                Button("Center in canvas") { selectedID = node.noteID; centerShown = true; useCanvas = true }
                                Button("Pin preview") { NotePreviewWindows.shared.show(client: model.client, id: GraphModel.noteID(for: node, vault: vaultScope?.scope ?? ""), pinned: true) { onSelect($0.raw) } }
                            }
                        }
                    } header: {
                        VStack(alignment: .leading, spacing: 6) {
                            Text("\(nodes.count) notes · includes unlinked notes")
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
        .task(id: vaultScope?.scope) { await model.loadFull(vault: vaultScope?.scope ?? "") }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard !model.isLoading else { return }
            Task { await model.loadFull(vault: vaultScope?.scope ?? "") }
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

    /// How many notes the view is showing and — since the overview draws only
    /// the linked part of the vault up to a cap — how many it left out (web
    /// `graphCountCaption`). One line, because both halves answer the same
    /// question.
    private static func caption(drawn: Int, hidden: Int) -> String {
        hidden > 0 ? "\(drawn)件描画・\(hidden)件未描画" : "\(drawn)件"
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
    let client: TrackClient?
    let vault: String
    @Environment(\.colorScheme) private var colorScheme
    @State private var positions: [TrackID: CGPoint] = [:]
    @State private var hoveredID: TrackID?
    @State private var hoverToken: UUID?
    @State private var scale: CGFloat = 1
    @State private var baseScale: CGFloat = 1
    @State private var offset: CGSize = .zero
    @State private var lastDrag: CGSize = .zero

    /// Force layout stays interactive: past this many nodes only the
    /// best-connected slice is drawn (the list keeps the full ranking).
    static let nodeCap = 300

    init(nodes: [GraphNode], edges: [GraphEdge], centerID: TrackID? = nil, selectedID: TrackID? = nil, client: TrackClient? = nil, vault: String = "", onSelect: @escaping (String) -> Void = { _ in }) {
        self.nodes = nodes
        self.edges = edges
        self.centerID = centerID
        self.selectedID = selectedID
        self.client = client
        self.vault = vault
        self.onSelect = onSelect
    }

    /// The drawn slice: best-connected first, edges reduced to it.
    var drawn: (nodes: [GraphNode], edges: [GraphEdge]) {
        let slice = GraphModel.canvasSlice(nodes: nodes, edges: edges, centerID: centerID, selectedID: selectedID, cap: Self.nodeCap)
        return (slice.nodes, slice.edges)
    }


    var body: some View {
        let (kept, keptEdges) = drawn
        let degrees = Dictionary(uniqueKeysWithValues: kept.map { ($0.noteID, GraphModel.degree(of: $0.noteID, in: keptEdges)) })
        GeometryReader { proxy in
            let size = proxy.size
            ZStack {
                if positions.isEmpty && !kept.isEmpty { ProgressView("Laying out graph") }
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
                .accessibilityLabel("Graph canvas. Switch to List to browse every note with the keyboard.")
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
                if let id = hit(at: location, size: size, positions: positions) { onSelect(id.raw) }
            }
            .onContinuousHover { phase in
                let id: TrackID?
                switch phase {
                case .active(let point): id = hit(at: point, size: size, positions: positions)
                case .ended: id = nil
                }
                guard id != hoveredID else { return }
                NotePreviewWindows.shared.leave(hoverToken)
                hoveredID = id
                if let id, let client, let node = kept.first(where: { $0.noteID == id }) {
                    hoverToken = NotePreviewWindows.shared.show(client: client, id: GraphModel.noteID(for: node, vault: vault)) { onSelect($0.raw) }
                }
            }
            .onDisappear { NotePreviewWindows.shared.leave(hoverToken) }
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
        .task(id: LayoutInput(nodes: kept, edges: keptEdges, centerID: centerID)) {
            let center = centerID
            let task = Task.detached(priority: .userInitiated) { GraphCanvasLayout.layout(nodes: kept, edges: keptEdges, centerID: center) }
            let result = await task.value
            guard !Task.isCancelled else { return }
            positions = result
        }
    }

    private struct LayoutInput: Equatable {
        let nodes: [GraphNode]
        let edges: [GraphEdge]
        let centerID: TrackID?
        static func == (a: Self, b: Self) -> Bool {
            a.centerID == b.centerID && a.nodes.map(\.noteID) == b.nodes.map(\.noteID) &&
            a.edges.map { "\($0.sourceID.raw)>\($0.targetID.raw)" } == b.edges.map { "\($0.sourceID.raw)>\($0.targetID.raw)" }
        }
    }

    private var pan: some Gesture {
        DragGesture()
            .onChanged { value in
                NotePreviewWindows.shared.leave(hoverToken)
                hoveredID = nil
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

    private func hit(at location: CGPoint, size: CGSize, positions: [TrackID: CGPoint]) -> TrackID? {
        let t = transform(for: size, positions: positions)
        let slice = drawn
        let points = positions.mapValues { t.apply($0) }
        let radii = Dictionary(uniqueKeysWithValues: slice.nodes.map { node in
            (node.noteID, GraphCanvasLayout.radius(for: node, degree: GraphModel.degree(of: node.noteID, in: slice.edges), isCenter: node.noteID == centerID) * t.scale)
        })
        return GraphModel.hitNode(at: location, positions: points, radii: radii)
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
                        GraphCanvas(nodes: graph.nodes, edges: graph.edges, centerID: centerShown ? graph.centerID : nil, selectedID: selectedID, client: model.client, vault: centerID.split().vault) { raw in
                            selectedID = graph.nodes.first { $0.noteID.raw == raw }?.noteID
                            if let node = graph.nodes.first(where: { $0.noteID.raw == raw }) { onSelect(GraphModel.noteID(for: node, vault: centerID.split().vault).raw) }
                        }
                        .frame(minHeight: 280)
                    }
                    Text("Center").font(.caption).foregroundStyle(.secondary)
                    Button {
                        selectedID = graph.centerID
                        onSelect(centerID.raw)
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
                    .notePreview(client: model.client, id: centerID) { onSelect($0.raw) }
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
                                onSelect(GraphModel.noteID(for: node, vault: centerID.split().vault).raw)
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
                                .notePreview(client: model.client, id: GraphModel.noteID(for: node, vault: centerID.split().vault)) { onSelect($0.raw) }
                        }
                    }
                }
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task(id: centerID) { await model.loadLocal(id: centerID) }
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
