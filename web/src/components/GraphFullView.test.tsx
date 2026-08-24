import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GraphFullView } from "./GraphFullView";

const navigate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

// A small fixed graph: one hub the test clicks through to.
vi.mock("../queries", () => ({
  useGraphQuery: () => ({
    data: {
      graph: {
        center_id: "a",
        nodes: [
          { note_id: "a", file_kind: "note", title: "Hub", size: 5 },
          { note_id: "b", file_kind: "note", title: "Stub", size: 1 },
        ],
        edges: [{ source_id: "b", target_id: "a" }],
      },
    },
  }),
}));

// GraphOverviewSigma needs WebGL, which jsdom does not ship; mock it down to the events the view
// wires up so the test can drive onSelect directly.
vi.mock("./GraphOverviewSigma", () => ({
  GraphOverviewSigma: ({ onSelect }: { onSelect: (id: string) => void }) => (
    <button type="button" onClick={() => onSelect("a")}>
      select-a
    </button>
  ),
}));

describe("GraphFullView", () => {
  beforeEach(() => {
    navigate.mockClear();
  });

  it("navigates to the note behind a clicked node", () => {
    const { container } = render(<GraphFullView />);
    fireEvent.click(
      [...container.querySelectorAll("button")].find((b) => b.textContent?.trim() === "select-a")!,
    );
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "a" } });
  });
});
