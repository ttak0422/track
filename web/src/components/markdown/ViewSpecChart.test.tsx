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

// The component lazy-imports echarts; the mock records setOption calls so tests can assert the
// server-resolved option reached the chart instance. on/off back the provenance click handler.
const setOption = vi.fn();
const dispose = vi.fn();
vi.mock("echarts", () => ({
  init: vi.fn(() => ({ setOption, resize: vi.fn(), dispose, on: vi.fn(), off: vi.fn() })),
  getInstanceByDom: vi.fn(() => undefined),
}));

// EChartsBlock reads the router for provenance clicks (navigate to a referenced note).
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));

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

  it("hands the server-resolved option to an ECharts instance", async () => {
    mockRender.mockResolvedValue({ echarts: { series: [{ type: "line" }] } });
    renderWithQuery(<ViewSpecChart text={spec} />);
    expect(screen.getByText("Rendering chart...")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("img", { name: "Chart" })).toBeInTheDocument());
    expect(mockRender).toHaveBeenCalledWith(spec);
    // The option arrives themed (applyChartTheme), so match on the semantic core.
    await waitFor(() =>
      expect(setOption).toHaveBeenCalledWith(
        expect.objectContaining({ series: [{ type: "line" }] }),
        { notMerge: true },
      ),
    );
  });

  it("shows the server error and the source at the block position", async () => {
    mockRender.mockRejectedValue(new Error('view spec: unsupported mark "pie"'));
    renderWithQuery(<ViewSpecChart text={spec} />);
    await waitFor(() =>
      expect(screen.getByText(/View Spec error: view spec: unsupported mark/)).toBeInTheDocument(),
    );
    // The original source stays readable (and copyable) under the error.
    expect(screen.getByRole("button", { name: "Copy code" })).toBeInTheDocument();
  });

  it("refetches and reapplies the option when the viewspec queries are invalidated", async () => {
    // This is the live-update path: useLiveEvents invalidates ["viewspec"] when the server emits a
    // `data` event for a change under the vault's data/ directory.
    mockRender.mockResolvedValueOnce({ echarts: { title: { text: "before" } } });
    const { client } = renderWithQuery(<ViewSpecChart text={spec} />);
    await waitFor(() =>
      expect(setOption).toHaveBeenCalledWith(
        expect.objectContaining({ title: expect.objectContaining({ text: "before" }) }),
        { notMerge: true },
      ),
    );

    mockRender.mockResolvedValueOnce({ echarts: { title: { text: "after" } } });
    await client.invalidateQueries({ queryKey: ["viewspec"] });
    await waitFor(() =>
      expect(setOption).toHaveBeenCalledWith(
        expect.objectContaining({ title: expect.objectContaining({ text: "after" }) }),
        { notMerge: true },
      ),
    );
    expect(mockRender).toHaveBeenCalledTimes(2);
  });
});
