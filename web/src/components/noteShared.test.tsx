import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { NoteAside } from "./noteShared";

const navigate = vi.hoisted(() => vi.fn());
const localGraph = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  useLocation: () => "",
  Link: ({ children }: { children?: ReactNode }) => <a>{children}</a>,
}));

vi.mock("../queries", () => ({
  useAgendaQuery: () => ({ isPending: false, data: { notes: [] } }),
  useLocalGraphQuery: (noteID: unknown) => localGraph(noteID),
}));

vi.mock("./preview/WikiLink", () => ({ WikiLink: () => null }));

// Stub the canvas (a lazy, client-only component) so the test can observe resetToken and drive
// onSelect directly.
vi.mock("./GraphCanvasLazy", () => ({
  GraphCanvas: ({ onSelect, resetToken }: { onSelect: (id: string) => void; resetToken: number }) => (
    <button type="button" data-reset={resetToken} onClick={() => onSelect("2")}>
      select-2
    </button>
  ),
}));

const linkedGraph = {
  graph: {
    center_id: "1",
    nodes: [
      { note_id: "1", file_kind: "note", title: "Center", center: true },
      { note_id: "2", file_kind: "note", title: "Neighbor" },
    ],
    edges: [{ source_id: "1", target_id: "2" }],
  },
};

describe("NoteAside graph section", () => {
  it("shows the always-on local graph, resets its view, and navigates on node select", () => {
    localGraph.mockReturnValue({ data: linkedGraph });
    render(<NoteAside backlinks={[]} noteID="1" journalDate="" />);

    expect(screen.getByRole("heading", { name: "Graph" })).toBeTruthy();

    const canvas = screen.getByText("select-2");
    expect(canvas.getAttribute("data-reset")).toBe("0");
    fireEvent.click(screen.getByRole("button", { name: "Reset graph view" }));
    expect(canvas.getAttribute("data-reset")).toBe("1");

    fireEvent.click(canvas);
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "2" } });
  });

  it("omits the graph while it is loading or when the note links nowhere", () => {
    localGraph.mockReturnValue({
      data: {
        graph: {
          center_id: "1",
          nodes: [{ note_id: "1", file_kind: "note", title: "Center", center: true }],
          edges: [],
        },
      },
    });
    render(<NoteAside backlinks={[]} noteID="1" journalDate="" />);
    expect(screen.queryByRole("heading", { name: "Graph" })).toBeNull();
  });
});
