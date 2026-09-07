import SwiftUI
import TrackAPI

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
        graph.edges.filter { $0.sourceID == id || $0.targetID == id }.count
    }

    /// Nodes ordered by degree descending, ties broken by title ascending.
    public static func nodesByDegree(_ graph: Graph) -> [GraphNode] {
        graph.nodes.sorted { lhs, rhs in
            let a = degree(of: lhs.noteID, in: graph)
            let b = degree(of: rhs.noteID, in: graph)
            if a != b { return a > b }
            return lhs.title < rhs.title
        }
    }
}

// MARK: - Full graph view

public struct GraphFullView: View {
    @Bindable var model: GraphModel
    let onSelect: (String) -> Void

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
                List(GraphModel.nodesByDegree(graph), id: \.noteID) { node in
                    Button {
                        onSelect(node.noteID.raw)
                    } label: {
                        HStack {
                            Text(node.title)
                            Spacer()
                            Text("\(GraphModel.degree(of: node.noteID, in: graph))")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .task { await model.loadFull() }
    }
}

// MARK: - Local graph view

public struct LocalGraphView: View {
    @Bindable var model: GraphModel
    let centerID: TrackID
    let onSelect: (String) -> Void

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
                VStack(alignment: .leading, spacing: 4) {
                    Text("Center").font(.caption).foregroundStyle(.secondary)
                    Button(Self.title(for: graph.centerID, in: graph)) {
                        onSelect(graph.centerID.raw)
                    }
                    .buttonStyle(.plain).fontWeight(.medium)
                    if !neighbors.isEmpty {
                        Divider()
                        Text("Linked").font(.caption).foregroundStyle(.secondary)
                        ForEach(neighbors, id: \.noteID) { node in
                            Button(node.title) { onSelect(node.noteID.raw) }
                                .buttonStyle(.plain)
                        }
                    }
                }
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task { await model.loadLocal(id: centerID) }
    }

    private static func neighbors(of graph: Graph) -> [GraphNode] {
        graph.nodes.filter { $0.noteID != graph.centerID && $0.center != true }
    }

    private static func title(for id: TrackID, in graph: Graph) -> String {
        graph.nodes.first { $0.noteID == id }?.title ?? id.raw
    }
}
