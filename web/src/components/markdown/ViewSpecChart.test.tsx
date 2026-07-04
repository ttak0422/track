import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderViewSpec } from "../../api";
import { ViewSpecChart } from "./ViewSpecChart";

vi.mock("../../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../api")>()),
  renderViewSpec: vi.fn(),
}));

const mockRender = vi.mocked(renderViewSpec);

const spec = '{"version":2,"mark":"line"}';

// The chart fetch lives in react-query (useViewSpecQuery), so every render needs a client.
function renderWithQuery(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return { client, ...render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>) };
}

describe("ViewSpecChart", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("inlines the server-rendered SVG, dropping the XML prolog", async () => {
    mockRender.mockResolvedValue({ svg: '<?xml version="1.0" encoding="UTF-8"?>\n<svg><text>PI</text></svg>' });
    const { container } = renderWithQuery(<ViewSpecChart text={spec} />);
    expect(screen.getByText("Rendering chart...")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(mockRender).toHaveBeenCalledWith(spec);
    const chart = screen.getByRole("img", { name: "Chart" });
    expect(chart.innerHTML).not.toContain("<?xml");
  });

  it("shows the server error and the source at the block position", async () => {
    mockRender.mockRejectedValue(new Error("view spec: unsupported mark \"pie\""));
    renderWithQuery(<ViewSpecChart text={spec} />);
    await waitFor(() =>
      expect(screen.getByText(/View Spec error: view spec: unsupported mark/)).toBeInTheDocument(),
    );
    // The original source stays readable (and copyable) under the error.
    expect(screen.getByRole("button", { name: "Copy code" })).toBeInTheDocument();
  });

  it("refetches and swaps the chart when the viewspec queries are invalidated", async () => {
    // This is the live-update path: useLiveEvents invalidates ["viewspec"] when the server emits a
    // `data` event for a change under the vault's data/ directory.
    mockRender.mockResolvedValueOnce({ svg: "<svg><text>before</text></svg>" });
    const { client } = renderWithQuery(<ViewSpecChart text={spec} />);
    await waitFor(() => expect(screen.getByText("before")).toBeInTheDocument());

    mockRender.mockResolvedValueOnce({ svg: "<svg><text>after</text></svg>" });
    await client.invalidateQueries({ queryKey: ["viewspec"] });
    await waitFor(() => expect(screen.getByText("after")).toBeInTheDocument());
    expect(mockRender).toHaveBeenCalledTimes(2);
  });
});
