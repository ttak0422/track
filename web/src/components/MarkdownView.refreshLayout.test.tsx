import { act, render, screen } from "@testing-library/react";
import { useEffect, useLayoutEffect, useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MarkdownView } from "./MarkdownView";

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn(), useRouterState: () => "/" }));

const diagram = vi.hoisted(() => ({
  mounts: 0, unmounts: 0, height: 24, scrollTop: 0,
  pending: [] as (() => void)[],
}));

// Use real Markdown parsing, component reconciliation, and FeatureBoundary. Only the async
// diagram engine is replaced: its first frame is small, its completed frame is tall. jsdom has no
// layout/scroll clamping, so the explicit model below represents a 400px viewport with 600px of
// other content. This proves the remount/geometry cause, not real WebKit scroll behavior.
function setDiagramHeight(height: number) {
  diagram.height = height;
  diagram.scrollTop = Math.min(diagram.scrollTop, Math.max(0, 600 + height - 400));
}
vi.mock("./markdown/MermaidDiagram", () => ({ MermaidDiagram: ({ text }: { text: string }) => {
  const [rendered, setRendered] = useState("");
  useEffect(() => {
    diagram.mounts++;
    return () => { diagram.unmounts++; setDiagramHeight(24); };
  }, []);
  useEffect(() => {
    let cancelled = false;
    diagram.pending.push(() => { if (!cancelled) setRendered(text); });
    return () => { cancelled = true; };
  }, [text]);
  useLayoutEffect(() => { setDiagramHeight(rendered ? 520 : 24); }, [rendered]);
  if (text.includes("broken")) throw new Error("diagram renderer failed");
  return <div data-testid="async-diagram" data-height={rendered ? "520" : "24"}
    data-source={text} style={{ height: rendered ? 520 : 24 }}>{rendered || "Loading diagram…"}</div>;
} }));

const source = (diagramText = "graph TD; A-->B", suffix = "") =>
  `# Reading position\n\n\`\`\`mermaid\n${diagramText}\n\`\`\`\n\nParagraph below the diagram.${suffix}`;
async function finishDiagram() {
  await act(async () => { diagram.pending.splice(0).forEach((finish) => finish()); });
}

beforeEach(() => {
  diagram.mounts = 0;
  diagram.unmounts = 0;
  diagram.height = 24;
  diagram.scrollTop = 0;
  diagram.pending = [];
});

describe("Markdown source refresh layout", () => {
  it("does not remount or collapse an unchanged diagram when unrelated source is appended", async () => {
    const view = render(<MarkdownView markdown={source()} noteId="a" />);
    await finishDiagram();
    const frame = screen.getByTestId("async-diagram");
    expect(frame).toHaveAttribute("data-height", "520");
    diagram.scrollTop = 650;

    view.rerender(<MarkdownView markdown={source(undefined, "\n\nNew unrelated paragraph.")} noteId="a" />);
    expect.soft(screen.getByTestId("async-diagram")).toBe(frame);
    expect.soft(diagram.mounts).toBe(1);
    expect.soft(diagram.unmounts).toBe(0);
    expect.soft(screen.getByTestId("async-diagram")).toHaveAttribute("data-height", "520");
    expect.soft(diagram.height).toBe(520);
    expect.soft(diagram.scrollTop).toBe(650);
    expect(screen.getByText("New unrelated paragraph.")).toBeInTheDocument();
  });

  it("updates an edited diagram's source without resetting its component identity", async () => {
    const view = render(<MarkdownView markdown={source()} noteId="a" />);
    await finishDiagram();
    const frame = screen.getByTestId("async-diagram");
    view.rerender(<MarkdownView markdown={source("graph TD; A-->C")} noteId="a" />);
    expect(screen.getByTestId("async-diagram")).toBe(frame);
    expect(frame).toHaveAttribute("data-source", "graph TD; A-->C");
    await finishDiagram();
    expect(frame).toHaveTextContent("graph TD; A-->C");
    expect(diagram.mounts).toBe(1);
  });

  it("resets stateful renderers when the note identity changes", async () => {
    const view = render(<MarkdownView markdown={source()} noteId="a" />);
    await finishDiagram();
    const frame = screen.getByTestId("async-diagram");
    view.rerender(<MarkdownView markdown={source()} noteId="b" />);
    expect(screen.getByTestId("async-diagram")).not.toBe(frame);
    expect(screen.getByTestId("async-diagram")).toHaveAttribute("data-height", "24");
    expect(diagram.unmounts).toBe(1);
    expect(diagram.mounts).toBe(2);
    await finishDiagram();
    expect(screen.getByTestId("async-diagram")).toHaveAttribute("data-height", "520");
  });

  it("retries a failed preview boundary when the source changes", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const view = render(<MarkdownView markdown={source("broken")} noteId="a" />);
      expect(screen.getByText("Preview is unavailable. Showing the note source.")).toBeInTheDocument();
      expect(screen.queryByTestId("async-diagram")).toBeNull();
      view.rerender(<MarkdownView markdown={source()} noteId="a" />);
      await finishDiagram();
      expect(screen.getByTestId("async-diagram")).toHaveAttribute("data-height", "520");
      expect(screen.queryByText("Preview is unavailable. Showing the note source.")).toBeNull();
    } finally { error.mockRestore(); }
  });
});
