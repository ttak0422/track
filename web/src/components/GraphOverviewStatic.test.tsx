import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Graph } from "../types";
import { GraphOverviewStatic } from "./GraphOverviewStatic";

const graph: Graph = {
  center_id: "0",
  nodes: [
    { note_id: "1", file_kind: "note", title: "Alpha", x: 10, y: 20, size: 3 },
    { note_id: "2", file_kind: "note", title: "Beta", x: 110, y: 20, size: 2 },
    // A node without server coordinates (a local-graph shape) is not part of the overview picture.
    { note_id: "3", file_kind: "note", title: "Gamma", size: 1 },
  ],
  edges: [
    { source_id: "1", target_id: "2" },
    { source_id: "1", target_id: "3" }, // one end unplaced: no line can be drawn
  ],
};

describe("GraphOverviewStatic", () => {
  it("draws one circle per placed node and one line per fully-placed edge", () => {
    const { container } = render(<GraphOverviewStatic graph={graph} onSelect={vi.fn()} />);
    expect(container.querySelectorAll("circle")).toHaveLength(2);
    expect(container.querySelectorAll("line")).toHaveLength(1);
  });

  it("fits its viewBox to the laid-out nodes with padding", () => {
    const { container } = render(<GraphOverviewStatic graph={graph} onSelect={vi.fn()} />);
    const svg = container.querySelector("svg");
    expect(svg?.getAttribute("viewBox")).toBe("-38 -28 196 97");
  });

  it("names each node with its title tooltip", () => {
    const { container } = render(<GraphOverviewStatic graph={graph} onSelect={vi.fn()} />);
    const titles = [...container.querySelectorAll("circle > title")].map((t) => t.textContent);
    expect(titles).toEqual(["Alpha", "Beta"]);
  });

  it("reports the clicked node", () => {
    const onSelect = vi.fn();
    const { container } = render(<GraphOverviewStatic graph={graph} onSelect={onSelect} />);
    fireEvent.click(container.querySelectorAll("circle")[1]);
    expect(onSelect).toHaveBeenCalledWith("2");
  });

  it("renders nothing when no node carries coordinates", () => {
    const bare: Graph = {
      center_id: "0",
      nodes: [{ note_id: "1", file_kind: "note", title: "Alpha" }],
      edges: [],
    };
    const { container } = render(<GraphOverviewStatic graph={bare} onSelect={vi.fn()} />);
    expect(container.querySelector("svg")).toBeNull();
  });
});
