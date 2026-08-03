import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { NoteAside, NoteProperties } from "./noteShared";

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

    // The section carries no caption — a graph is recognisably a graph — so it is found by its
    // accessible name instead.
    expect(screen.getByRole("region", { name: "Local graph" })).toBeTruthy();

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
    expect(screen.queryByRole("region", { name: "Local graph" })).toBeNull();
  });
});

describe("NoteProperties dates", () => {
  const props = [{ key: "status", value: "draft", type: "string", line: 0 }];

  it("closes the strip with the created and updated rows", () => {
    // Built from a local date so the day the mtime formats to is the same in any timezone.
    const updated = new Date(2026, 5, 20, 12, 0, 0).getTime() / 1000;
    render(<NoteProperties props={props} created="2026-06-14" updated={updated} />);

    expect(screen.getAllByRole("term").map((dt) => dt.textContent)).toEqual([
      "status",
      "created",
      "updated",
    ]);
    // created shows verbatim; updated is the mtime at the same day precision.
    expect(screen.getByText("2026-06-14")).toBeTruthy();
    expect(screen.getByText("2026-06-20")).toBeTruthy();
  });

  it("omits a date row the note has no value for", () => {
    render(<NoteProperties props={props} created="2026-06-14" />);
    expect(screen.getAllByRole("term").map((dt) => dt.textContent)).toEqual(["status", "created"]);
  });

  // The common note has no properties of its own but does have a created date, so the strip has to
  // open for the dates alone — it is only empty when there is nothing at all to show.
  it("shows the dates on a note with no properties, and nothing at all without either", () => {
    const { unmount } = render(<NoteProperties props={[]} created="2026-06-14" />);
    expect(screen.getAllByRole("term").map((dt) => dt.textContent)).toEqual(["created"]);
    unmount();

    render(<NoteProperties props={[]} />);
    expect(screen.queryByRole("list")).toBeNull();
    expect(screen.queryAllByRole("term")).toEqual([]);
  });
});
