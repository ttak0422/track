import { describe, expect, it } from "vitest";
import type { Graph } from "../types";
import { OVERVIEW_NODE_CAP, overviewGraph } from "./overviewGraph";

function makeGraph(ids: string[], links: Array<[string, string]>): Graph {
  return {
    center_id: "",
    nodes: ids.map((note_id) => ({ note_id, file_kind: "note" as const, title: note_id })),
    edges: links.map(([source_id, target_id]) => ({ source_id, target_id })),
  };
}

describe("overviewGraph", () => {
  it("drops notes with no link and counts them as not drawn", () => {
    const graph = makeGraph(["a", "b", "c", "d"], [["a", "b"]]);
    const overview = overviewGraph(graph);
    expect(overview.graph.nodes.map((n) => n.note_id)).toEqual(["a", "b"]);
    expect(overview.hidden).toBe(2);
  });

  it("ignores a self link and a link naming a note the payload never delivered", () => {
    const graph = makeGraph(["a", "b"], [["a", "a"], ["b", "ghost"]]);
    const overview = overviewGraph(graph);
    // Neither link puts anything on screen, so neither keeps its note in the picture.
    expect(overview.graph.nodes).toEqual([]);
    expect(overview.graph.edges).toEqual([]);
    expect(overview.hidden).toBe(2);
  });

  it("leaves a graph under the cap alone", () => {
    const graph = makeGraph(["a", "b", "c"], [["a", "b"], ["b", "c"]]);
    const overview = overviewGraph(graph);
    expect(overview.graph.nodes).toHaveLength(3);
    expect(overview.graph.edges).toHaveLength(2);
    expect(overview.hidden).toBe(0);
  });

  it("keeps the best-connected notes when the graph runs past the cap", () => {
    // A hub wired to everything, plus pairs that only link to each other: with a cap of 3 the hub
    // and the two notes it is drawn with must win over any pair further down.
    const ids = ["hub", ...Array.from({ length: 20 }, (_, i) => `n${String(i).padStart(2, "0")}`)];
    const links: Array<[string, string]> = ids.slice(1).map((id) => ["hub", id] as [string, string]);
    links.push(["n18", "n19"]); // a pair with one extra link each, ranking them above their peers
    const overview = overviewGraph(makeGraph(ids, links), 3);
    expect(overview.graph.nodes.map((n) => n.note_id)).toEqual(["hub", "n18", "n19"]);
    expect(overview.hidden).toBe(18);
  });

  it("drops a node the cut stranded rather than drawing a loose dot", () => {
    // Cap 3 keeps the triangle's members by degree; "lonely" would rank in on a link to "outside",
    // which the cut removes — so it must not survive as an unconnected dot.
    const graph = makeGraph(
      ["x", "y", "z", "lonely", "outside", "far"],
      [["x", "y"], ["y", "z"], ["z", "x"], ["lonely", "outside"], ["outside", "far"]],
    );
    const overview = overviewGraph(graph, 4);
    expect(overview.graph.nodes.map((n) => n.note_id)).toEqual(["x", "y", "z"]);
    for (const node of overview.graph.nodes) {
      expect(
        overview.graph.edges.some(
          (e) => e.source_id === node.note_id || e.target_id === node.note_id,
        ),
      ).toBe(true);
    }
  });

  it("cuts the same way however the payload is ordered", () => {
    const ids = Array.from({ length: 12 }, (_, i) => `n${String(i).padStart(2, "0")}`);
    const links = ids.slice(1).map((id) => ["n00", id] as [string, string]);
    const first = overviewGraph(makeGraph(ids, links), 5);
    const shuffled = overviewGraph(makeGraph([...ids].reverse(), [...links].reverse()), 5);
    expect(shuffled.graph.nodes.map((n) => n.note_id)).toEqual(
      first.graph.nodes.map((n) => n.note_id),
    );
  });

  it("caps at a size a screen can still show", () => {
    // The bound is a readability budget, not a performance one: a 1920x1080 canvas holds about
    // 2,300 dots at a 30px pitch before the edges have nowhere to go.
    expect(OVERVIEW_NODE_CAP).toBeLessThanOrEqual(2300);
  });

  it("survives a payload with no nodes or edges at all", () => {
    const overview = overviewGraph({ center_id: "", nodes: [], edges: [] });
    expect(overview.graph.nodes).toEqual([]);
    expect(overview.hidden).toBe(0);
  });
});
