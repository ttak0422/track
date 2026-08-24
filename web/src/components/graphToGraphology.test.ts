import { describe, expect, it } from "vitest";
import type { Graph as TrackGraph, GraphEdge, GraphNode } from "../types";
import { graphToGraphology } from "./graphToGraphology";

function node(partial: Partial<GraphNode> & { note_id: string }): GraphNode {
  return { file_kind: "note", title: `Note ${partial.note_id}`, ...partial };
}

const sample: TrackGraph = {
  center_id: "a",
  nodes: [
    node({ note_id: "a", title: "Hub", size: 5 }),
    node({ note_id: "b", size: 1 }),
    node({ note_id: "c" }), // no grade at all → degree fallback
    node({ note_id: "dangling-target", title: "" }), // titleless → #id label
  ],
  edges: [
    { source_id: "b", target_id: "a" },
    { source_id: "c", target_id: "a" },
    // Both endpoints missing from the node list; dropped rather than throwing.
    { source_id: "ghost", target_id: "phantom" } as GraphEdge,
    // Self loop and duplicate pair; collapsed by the simple-undirected graph.
    { source_id: "b", target_id: "b" },
    { source_id: "a", target_id: "b" },
  ],
};

describe("graphToGraphology", () => {
  it("adds every node once and keeps only edges whose endpoints exist", () => {
    const rendered = graphToGraphology(sample);

    expect(rendered.order).toBe(4);
    for (const n of sample.nodes) expect(rendered.hasNode(n.note_id)).toBe(true);
    // ghost→phantom dropped (no endpoints), b→b dropped (self loop), a→b collapsed into b→a.
    expect(rendered.size).toBe(2);
    expect(rendered.hasEdge("b", "a")).toBe(true);
    expect(rendered.hasEdge("c", "a")).toBe(true);
  });

  it("is deterministic: the same payload converts to identical attributes", () => {
    const first = graphToGraphology(sample);
    const second = graphToGraphology(sample);

    first.forEachNode((n, attrs) => {
      expect(second.getNodeAttributes(n)).toEqual(attrs);
    });
  });

  it("labels nodes by title with a #id fallback, keyed by note id", () => {
    const rendered = graphToGraphology(sample);

    expect(rendered.getNodeAttribute("a", "label")).toBe("Hub");
    expect(rendered.getNodeAttribute("dangling-target", "label")).toBe("#dangling-target");
  });

  it("sizes nodes by their precomputed grade and keeps the centre focal", () => {
    const rendered = graphToGraphology(sample);

    const hub = rendered.getNodeAttributes("a");
    const stub = rendered.getNodeAttributes("b");
    expect(hub.center).toBe(true);
    expect(stub.center).toBe(false);
    // Grade 5 vs grade 1 — the same five-level radii nodeRadius.ts draws.
    expect(stub.size).toBeLessThan(hub.size);
    expect(hub.size).toBeGreaterThanOrEqual(10); // focal floor for the centre
    // No grade at all falls back to incident-edge degree sizing instead of collapsing to zero.
    expect(rendered.getNodeAttribute("c", "size")).toBeGreaterThan(0);
  });

  it("seeds positions on a deterministic ring, centring a single node", () => {
    const rendered = graphToGraphology(sample);
    expect(rendered.getNodeAttribute("a", "x")).toBeCloseTo(160);
    expect(rendered.getNodeAttribute("a", "y")).toBeCloseTo(0);

    const lone = graphToGraphology({
      center_id: "solo",
      nodes: [node({ note_id: "solo" })],
      edges: [],
    });
    expect(lone.getNodeAttribute("solo", "x")).toBe(0);
    expect(lone.getNodeAttribute("solo", "y")).toBe(0);
  });

  it("tolerates missing edge/node lists like the API's optional shapes", () => {
    const empty = graphToGraphology({ center_id: "", nodes: [], edges: [] });
    expect(empty.order).toBe(0);
    expect(empty.size).toBe(0);
  });
});
