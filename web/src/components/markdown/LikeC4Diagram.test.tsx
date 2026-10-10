import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LikeC4Diagram } from "./LikeC4Diagram";

const renderLikeC4 = vi.hoisted(() => vi.fn(async (_text: string, _signal: AbortSignal) => [
  { id: "overview", title: "システム全体", svg: '<svg viewBox="0 0 100 40"><text>System</text></svg>' },
  { id: "components", title: "内部構成", svg: '<svg viewBox="0 0 200 80"><text>API</text></svg>' },
]));
vi.mock("./likec4Engine", () => ({ renderLikeC4 }));

describe("LikeC4Diagram", () => {
  afterEach(() => vi.clearAllMocks());

  it("switches views in the shared fitting and pan frame", async () => {
    const { container } = render(<LikeC4Diagram text="source" />);
    expect(screen.getByText("Rendering diagram...")).toBeInTheDocument();
    await screen.findByRole("img", { name: "LikeC4 diagram: システム全体" });
    const overview = screen.getByRole("button", { name: "システム全体" });
    expect(overview).toHaveAttribute("aria-pressed", "true");
    const initialFrame = container.querySelector(".mermaid-diagram");
    fireEvent.click(screen.getByRole("button", { name: "内部構成" }));
    expect(screen.getByRole("img", { name: "LikeC4 diagram: 内部構成" })).toHaveTextContent("API");
    expect(overview).toHaveAttribute("aria-pressed", "false");
    // A view switch starts with a fresh fitting state and cannot retain the other view's pan.
    expect(container.querySelector(".mermaid-diagram")).not.toBe(initialFrame);
    expect(screen.getByRole("button", { name: "Reset diagram view" })).toBeInTheDocument();
  });

  it("shows the source and parser message on failure", async () => {
    renderLikeC4.mockRejectedValueOnce(new Error("Line 2: unknown element"));
    render(<LikeC4Diagram text="broken source" />);
    await screen.findByText("LikeC4 render failed: Line 2: unknown element");
    expect(screen.getByText("broken source")).toBeInTheDocument();
  });

  it("aborts work when the source changes or the diagram unmounts", async () => {
    const { rerender, unmount } = render(<LikeC4Diagram text="first" />);
    await waitFor(() => expect(renderLikeC4).toHaveBeenCalledTimes(1));
    const firstSignal = renderLikeC4.mock.calls[0][1];
    rerender(<LikeC4Diagram text="second" />);
    expect(firstSignal.aborted).toBe(true);
    await waitFor(() => expect(renderLikeC4).toHaveBeenCalledTimes(2));
    const secondSignal = renderLikeC4.mock.calls[1][1];
    unmount();
    expect(secondSignal.aborted).toBe(true);
  });
});
