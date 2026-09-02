import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { graphCountCaption } from "./graphCaption";
import { GraphFullView } from "./GraphFullView";
import { GraphPanel } from "./GraphPanel";

// A mutable graph the useGraphQuery mock hands back by reference, so each test sets the vault's
// shape without re-mocking the module. GraphFullView and GraphPanel share the whole-vault graph
// query, so the caption belongs to both surfaces.
type MockNode = { note_id: string; file_kind: string; title: string };
type MockEdge = { source_id: string; target_id: string };
const nodes = vi.hoisted(() => [] as MockNode[]);
const edges = vi.hoisted(() => [] as MockEdge[]);
const navigate = vi.hoisted(() => vi.fn());
const floatingOpen = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("../queries", () => ({
  useGraphQuery: () => ({ data: { graph: { center_id: "c", nodes, edges } } }),
}));
vi.mock("./GraphCanvasLazy", () => ({
  GraphCanvas: () => <div data-testid="graph-canvas" />,
}));
vi.mock("./preview/floatingStore", () => ({ useFloating: () => ({ open: floatingOpen }) }));
vi.mock("./preview/NoteWindow", () => ({ NoteWindow: () => <div data-testid="note-window" /> }));

// The overview draws only the linked part of a vault, so the sample is a chain: every node carries
// an edge and survives into the picture the caption counts.
function setChain(count: number) {
  nodes.splice(
    0,
    nodes.length,
    ...Array.from({ length: count }, (_, i) => ({
      note_id: `n${i}`,
      file_kind: "note",
      title: `Note ${i}`,
    })),
  );
  edges.splice(
    0,
    edges.length,
    ...Array.from({ length: Math.max(0, count - 1) }, (_, i) => ({
      source_id: `n${i}`,
      target_id: `n${i + 1}`,
    })),
  );
}

describe("graphCountCaption", () => {
  it("formats the count, singular and plural", () => {
    expect(graphCountCaption(612)).toBe("612 notes");
    expect(graphCountCaption(1)).toBe("1 note");
  });

  it("carries what the overview left out on the same line", () => {
    expect(graphCountCaption(1000, 1234)).toBe("1,000 notes · 1,234 not drawn");
  });
});

describe("whole-vault graph caption", () => {
  it("counts what GraphFullView drew", () => {
    setChain(120);
    render(<GraphFullView />);
    expect(screen.getByTestId("graph-canvas")).toBeInTheDocument();
    expect(screen.getByText("120 notes")).toBeInTheDocument();
  });

  it("says nothing beside an empty graph", () => {
    setChain(0);
    render(<GraphFullView />);
    expect(screen.getByTestId("graph-canvas")).toBeInTheDocument();
    expect(screen.queryByText(/notes?$/)).not.toBeInTheDocument();
  });

  it("counts what GraphPanel drew", () => {
    setChain(25);
    render(<GraphPanel />);
    fireEvent.click(screen.getByLabelText("Show graph"));
    expect(screen.getByTestId("graph-canvas")).toBeInTheDocument();
    expect(screen.getByText("25 notes")).toBeInTheDocument();
  });

  // An unlinked note is not in a picture of the link graph, so the caption has to account for it —
  // otherwise the corner claims a count the vault does not recognise.
  it("names the notes the overview left out of the picture", () => {
    setChain(10);
    nodes.push(
      ...Array.from({ length: 5 }, (_, i) => ({
        note_id: `orphan${i}`,
        file_kind: "note",
        title: `Orphan ${i}`,
      })),
    );
    render(<GraphFullView />);
    expect(screen.getByText("10 notes · 5 not drawn")).toBeInTheDocument();
  });
});
