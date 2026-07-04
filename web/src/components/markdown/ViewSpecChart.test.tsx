import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderViewSpec } from "../../api";
import { ViewSpecChart } from "./ViewSpecChart";

vi.mock("../../api", () => ({
  renderViewSpec: vi.fn(),
}));

const mockRender = vi.mocked(renderViewSpec);

const spec = '{"version":2,"mark":"line"}';

describe("ViewSpecChart", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("inlines the server-rendered SVG, dropping the XML prolog", async () => {
    mockRender.mockResolvedValue({ svg: '<?xml version="1.0" encoding="UTF-8"?>\n<svg><text>PI</text></svg>' });
    const { container } = render(<ViewSpecChart text={spec} />);
    expect(screen.getByText("Rendering chart...")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(mockRender).toHaveBeenCalledWith(spec);
    const chart = screen.getByRole("img", { name: "Chart" });
    expect(chart.innerHTML).not.toContain("<?xml");
  });

  it("shows the server error and the source at the block position", async () => {
    mockRender.mockRejectedValue(new Error("view spec: unsupported mark \"pie\""));
    render(<ViewSpecChart text={spec} />);
    await waitFor(() =>
      expect(screen.getByText(/View Spec error: view spec: unsupported mark/)).toBeInTheDocument(),
    );
    // The original source stays readable (and copyable) under the error.
    expect(screen.getByRole("button", { name: "Copy code" })).toBeInTheDocument();
  });
});
