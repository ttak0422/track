import { describe, expect, it } from "vitest";
import type { Graph, GraphEdge, GraphNode } from "../types";
import { layoutStatic } from "./graphStaticLayout";

function makeGraph(nodeIds: string[], edges: Array<[string, string]>): Graph {
  const nodes: GraphNode[] = nodeIds.map((note_id) => ({ note_id, file_kind: "note", title: note_id }));
  const graphEdges: GraphEdge[] = edges.map(([source_id, target_id]) => ({ source_id, target_id }));
  return { center_id: nodeIds[0] ?? "", nodes, edges: graphEdges };
}

function distance(a: { x: number; y: number }, b: { x: number; y: number }): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

describe("layoutStatic", () => {
  it("returns an empty layout for an empty graph", () => {
    const layout = layoutStatic({ center_id: "", nodes: [], edges: [] });
    expect(layout.positions.size).toBe(0);
    expect(layout.width).toBe(0);
    expect(layout.height).toBe(0);
  });

  it("places every node inside the reported bounds, edges or not", () => {
    const graph = makeGraph(
      ["a", "b", "c", "d", "e", "f"],
      [["a", "b"], ["b", "c"], ["d", "e"]],
    );
    const layout = layoutStatic(graph);
    expect(layout.positions.size).toBe(6);
    for (const point of layout.positions.values()) {
      expect(point.x).toBeGreaterThanOrEqual(0);
      expect(point.x).toBeLessThanOrEqual(layout.width);
      expect(point.y).toBeGreaterThanOrEqual(0);
      expect(point.y).toBeLessThanOrEqual(layout.height);
    }
  });

  it("ignores edges naming unknown nodes instead of crashing on them", () => {
    const graph = makeGraph(["a", "b"], [["a", "ghost"], ["a", "b"]]);
    const layout = layoutStatic(graph);
    expect(layout.positions.size).toBe(2);
    expect(distance(layout.positions.get("a")!, layout.positions.get("b")!)).toBeGreaterThan(0);
  });

  it("is deterministic: the same graph lays out identically however its arrays are ordered", () => {
    const edges: Array<[string, string]> = [
      ["a", "b"], ["b", "c"], ["c", "a"], // triangle
      ["a", "d"], // pendant
      ["e", "f"], ["f", "g"], ["g", "h"], ["h", "e"], // cycle
    ];
    const first = layoutStatic(makeGraph(["a", "b", "c", "d", "e", "f", "g", "h"], edges));
    const shuffled = layoutStatic(
      makeGraph(["h", "c", "a", "g", "d", "b", "f", "e"], [...edges].reverse()),
    );
    expect(shuffled.positions).toEqual(first.positions);
    expect(shuffled.width).toBe(first.width);
    expect(shuffled.height).toBe(first.height);
  });

  it("separates components onto disjoint shelves", () => {
    const graph = makeGraph(
      ["a", "b", "c", "d", "e"],
      [["a", "b"], ["b", "c"], ["c", "a"], ["d", "e"]],
    );
    const layout = layoutStatic(graph);

    const boxes = new Map<string, { minX: number; maxX: number; minY: number; maxY: number }>();
    for (const group of [["a", "b", "c"], ["d", "e"]] as const) {
      let box = { minX: Infinity, maxX: -Infinity, minY: Infinity, maxY: -Infinity };
      for (const id of group) {
        const point = layout.positions.get(id)!;
        box = {
          minX: Math.min(box.minX, point.x),
          maxX: Math.max(box.maxX, point.x),
          minY: Math.min(box.minY, point.y),
          maxY: Math.max(box.maxY, point.y),
        };
      }
      boxes.set(group[0], box);
    }
    const triangle = boxes.get("a")!;
    const pair = boxes.get("d")!;
    // No shared area: one box ends before the other begins on some axis.
    const separated =
      pair.minX >= triangle.maxX ||
      triangle.minX >= pair.maxX ||
      pair.minY >= triangle.maxY ||
      triangle.minY >= pair.maxY;
    expect(separated).toBe(true);
  });

  it("rings a star around its best-connected node", () => {
    const graph = makeGraph(
      ["hub", "l1", "l2", "l3", "l4", "l5"],
      [["hub", "l1"], ["hub", "l2"], ["hub", "l3"], ["hub", "l4"], ["hub", "l5"]],
    );
    const layout = layoutStatic(graph);
    const hub = layout.positions.get("hub")!;
    const leaves = ["l1", "l2", "l3", "l4", "l5"].map((id) => layout.positions.get(id)!);
    // Evenly spaced leaves average to their circle's centre — which is where the hub sits.
    const meanX = leaves.reduce((sum, p) => sum + p.x, 0) / leaves.length;
    const meanY = leaves.reduce((sum, p) => sum + p.y, 0) / leaves.length;
    expect(hub.x).toBeCloseTo(meanX, 6);
    expect(hub.y).toBeCloseTo(meanY, 6);
    const radii = leaves.map((leaf) => distance(hub, leaf));
    for (const radius of radii) expect(radius).toBeCloseTo(radii[0], 6);
  });

  it("breaks a degree tie toward the smallest id, putting it on the innermost ring", () => {
    // Path p1-p2-p3-p4: p2 and p3 both carry degree 2; p2 wins the tie and anchors the rings.
    const graph = makeGraph(["p1", "p2", "p3", "p4"], [["p1", "p2"], ["p2", "p3"], ["p3", "p4"]]);
    const layout = layoutStatic(graph);
    const p1 = layout.positions.get("p1")!;
    const p2 = layout.positions.get("p2")!;
    const p3 = layout.positions.get("p3")!;
    const p4 = layout.positions.get("p4")!;
    // With p2 at the centre, the far end p4 hangs off the farthest ring from it — measurably
    // farther from p2 than from p3. The reverse choice would flip the comparison.
    expect(distance(p2, p4)).toBeLessThan(distance(p3, p4));
    expect(distance(p2, p4)).toBeGreaterThan(distance(p2, p1));
  });

  it("lays out a vault-sized graph without blowing up", () => {
    // Main-vault scale: ~3,500 notes, a few hundred links. Pure traversal, no simulation passes.
    const nodeIds: string[] = [];
    const edges: Array<[string, string]> = [];
    for (let i = 0; i < 3500; i++) nodeIds.push(`n${String(i).padStart(4, "0")}`);
    for (let i = 0; i < 400; i++) edges.push([nodeIds[i], nodeIds[(i * 7 + 1) % 3500]]);
    const started = performance.now();
    const layout = layoutStatic(makeGraph(nodeIds, edges));
    const elapsed = performance.now() - started;
    expect(layout.positions.size).toBe(3500);
    expect(elapsed).toBeLessThan(1000);
  });
});
