import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { GraphFullView } from "./GraphFullView";

const navigate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("../queries", () => ({
  useGraphQuery: () => ({
    isPending: false,
    isError: false,
    data: {
      graph: {
        center_id: 0,
        nodes: [{ note_id: "7", file_kind: "note", title: "Seven", x: 10, y: 20 }],
        edges: [],
      },
    },
  }),
}));

// GraphFullView consumes the static overview; the stub exposes its onSelect so the test can drive
// navigation without drawing SVG.
vi.mock("./GraphOverviewStatic", () => ({
  GraphOverviewStatic: ({ onSelect }: { onSelect: (id: string) => void }) => (
    <button type="button" onClick={() => onSelect("7")}>
      select-7
    </button>
  ),
}));

describe("GraphFullView", () => {
  beforeEach(() => {
    navigate.mockClear();
  });

  it("navigates to a note selected in the overview", () => {
    const { container } = render(<GraphFullView />);
    fireEvent.click(
      [...container.querySelectorAll("button")].find((b) => b.textContent?.trim() === "select-7")!,
    );
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "7" } });
  });

  it("carries no reset control: the static overview is fitted by construction", () => {
    const { container } = render(<GraphFullView />);
    expect(container.textContent).not.toContain("Reset");
    // The only interactive element is the overview itself.
    expect(container.querySelectorAll("button")).toHaveLength(1);
  });
});
