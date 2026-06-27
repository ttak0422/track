import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MermaidDiagram } from "./MermaidDiagram";

vi.mock("mermaid", () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn(async () => ({ svg: "<svg><text>Diagram</text></svg>" })),
  },
}));

describe("MermaidDiagram", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders generated SVG", async () => {
    const { container } = render(<MermaidDiagram text={"graph TD\nA-->B"} />);
    expect(screen.getByText("Rendering diagram...")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "Mermaid diagram" })).toBeInTheDocument();
  });
});
