import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GraphFullView } from "./GraphFullView";

const navigate = vi.hoisted(() => vi.fn());
const useGraphQuery = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("../queries", () => ({ useGraphQuery }));

// The overview canvas is stubbed so the test can drive onSelect and observe the reset token without
// a canvas context; GraphFullView owns nothing else but messages and the reset button.
vi.mock("./GraphOverviewCanvas", () => ({
  GraphOverviewCanvas: ({
    onSelect,
    resetToken,
  }: {
    onSelect: (noteID: string) => void;
    resetToken: number;
  }) => (
    <div>
      <button type="button" onClick={() => onSelect("a")}>
        select-a
      </button>
      <span data-testid="reset-token">{resetToken}</span>
    </div>
  ),
}));

function queryResult(overrides: Record<string, unknown> = {}) {
  return {
    data: { graph: { center_id: "", nodes: [], edges: [] } },
    isPending: false,
    isError: false,
    error: undefined,
    ...overrides,
  };
}

beforeEach(() => {
  navigate.mockClear();
});

describe("GraphFullView", () => {
  it("navigates to the note selected on the overview canvas", () => {
    useGraphQuery.mockReturnValue(queryResult());
    const { container } = render(<GraphFullView />);
    fireEvent.click(
      [...container.querySelectorAll("button")].find((b) => b.textContent === "select-a")!,
    );
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "a" } });
  });

  it("hands the reset control's token to the canvas", () => {
    useGraphQuery.mockReturnValue(queryResult());
    const { container, getByTestId } = render(<GraphFullView />);
    expect(getByTestId("reset-token").textContent).toBe("0");
    fireEvent.click(container.querySelector('button[aria-label="Reset graph view"]')!);
    expect(getByTestId("reset-token").textContent).toBe("1");
  });

  it("shows a loading message while the graph query is pending", () => {
    useGraphQuery.mockReturnValue(queryResult({ isPending: true, data: undefined }));
    const { container } = render(<GraphFullView />);
    expect(container.querySelector(".graph-message")?.textContent).toBe("Loading graph...");
  });

  it("shows the query error when the graph fails to load", () => {
    useGraphQuery.mockReturnValue(queryResult({ isError: true, error: new Error("boom") }));
    const { container } = render(<GraphFullView />);
    expect(container.querySelector(".graph-message")?.textContent).toBe("boom");
  });
});
