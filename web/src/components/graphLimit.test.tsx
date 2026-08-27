import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { graphCountCaption, graphTooManyMessage, MAX_GRAPH_NODES } from "./graphLimit";
import { GraphFullView } from "./GraphFullView";
import { GraphPanel } from "./GraphPanel";

// A mutable node list the useGraphQuery mock hands back by reference, so each test sets the vault's
// size without re-mocking the module. GraphFullView and GraphPanel share the whole-vault graph query,
// so the limit applies to both surfaces.
type MockNode = { note_id: string; file_kind: string; title: string };
const nodes = vi.hoisted(() => [] as MockNode[]);
const navigate = vi.hoisted(() => vi.fn());
const floatingOpen = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("../queries", () => ({
  useGraphQuery: () => ({ data: { graph: { center_id: "c", nodes, edges: [] } } }),
}));
vi.mock("./GraphCanvasLazy", () => ({
  GraphCanvas: () => <div data-testid="graph-canvas" />,
}));
vi.mock("./preview/floatingStore", () => ({ useFloating: () => ({ open: floatingOpen }) }));
vi.mock("./preview/NoteWindow", () => ({ NoteWindow: () => <div data-testid="note-window" /> }));

function makeNodes(count: number): MockNode[] {
  return Array.from({ length: count }, (_, i) => ({
    note_id: `n${i}`,
    file_kind: "note",
    title: `Note ${i}`,
  }));
}

function setNodeCount(count: number) {
  nodes.splice(0, nodes.length, ...makeNodes(count));
}

describe("graph node limit helpers", () => {
  it("caps the whole-vault graph at 500 nodes", () => {
    expect(MAX_GRAPH_NODES).toBe(500);
  });

  it("formats the count caption, singular and plural", () => {
    expect(graphCountCaption(612)).toBe("612 notes");
    expect(graphCountCaption(1)).toBe("1 note");
  });

  it("formats the overflow message with the actual count", () => {
    expect(graphTooManyMessage(612)).toBe("Too many notes to display (612 notes).");
  });
});

describe("GraphFullView whole-vault graph node limit", () => {
  it("renders an English overflow message with the count instead of the canvas past the limit", () => {
    setNodeCount(MAX_GRAPH_NODES + 112); // 612
    render(<GraphFullView />);
    expect(screen.getByText("Too many notes to display (612 notes).")).toBeInTheDocument();
    expect(screen.queryByTestId("graph-canvas")).not.toBeInTheDocument();
  });

  it("renders the canvas right at the limit", () => {
    setNodeCount(MAX_GRAPH_NODES); // 500
    render(<GraphFullView />);
    expect(screen.getByTestId("graph-canvas")).toBeInTheDocument();
    expect(screen.queryByText(/Too many notes/)).not.toBeInTheDocument();
  });

  it("shows the count caption beside the canvas under the limit", () => {
    setNodeCount(120);
    render(<GraphFullView />);
    expect(screen.getByTestId("graph-canvas")).toBeInTheDocument();
    expect(screen.getByText("120 notes")).toBeInTheDocument();
  });

  it("shows no count caption for an empty graph", () => {
    setNodeCount(0);
    render(<GraphFullView />);
    expect(screen.getByTestId("graph-canvas")).toBeInTheDocument();
    expect(screen.queryByText(/notes?$/)).not.toBeInTheDocument();
  });
});

describe("GraphPanel whole-vault graph node limit", () => {
  function openPanel() {
    const view = render(<GraphPanel />);
    fireEvent.click(screen.getByLabelText("Show graph"));
    return view;
  }

  it("refuses the canvas past the limit, naming the count", () => {
    setNodeCount(MAX_GRAPH_NODES + 1); // 501
    openPanel();
    expect(screen.getByText("Too many notes to display (501 notes).")).toBeInTheDocument();
    expect(screen.queryByTestId("graph-canvas")).not.toBeInTheDocument();
  });

  it("shows the count caption beside the canvas under the limit", () => {
    setNodeCount(25);
    openPanel();
    expect(screen.getByTestId("graph-canvas")).toBeInTheDocument();
    expect(screen.getByText("25 notes")).toBeInTheDocument();
  });
});