import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderMetricsDashboard } from "../../api";
import type { MetricsDashboardResponse } from "../../types";
import { MetricsDashboard } from "./MetricsDashboard";
import { NoteVaultContext } from "./context";

vi.mock("../../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../api")>()), renderMetricsDashboard: vi.fn(),
}));
vi.mock("./EChartsBlock", () => ({
  EChartsBlock: ({ option }: { option: Record<string, unknown> }) => <div role="img" aria-label="Resolved chart">{JSON.stringify(option)}</div>,
}));
const mockRender = vi.mocked(renderMetricsDashboard);
const spec = '{"title":"Service health","panels":[]}';
const value = { name: "cpu", entity: "worker-a", value: 82, display: "82%", time: "2026-09-18T12:00:00Z", threshold: 0.8, thresholdDisplay: "80%" };
const dashboard: MetricsDashboardResponse = {
  title: "Service health", metrics: ["cpu", "latency"], entities: ["worker-a", "worker-b"], asof: value.time,
  panels: [
    { id: "0", title: "CPU", type: "stat", grid: { x: 12, y: 0, w: 12, h: 4 }, unit: "%", values: [value] },
    { id: "1", title: "History", type: "timeseries", grid: { x: 0, y: 0, w: 12, h: 8 }, unit: "%", values: [value], echarts: { series: [{ type: "line" }] } },
    { id: "2", title: "Hosts", type: "table", grid: { x: 12, y: 4, w: 12, h: 4 }, unit: "%", values: [value] },
    { id: "3", title: "Missing source", type: "stat", grid: { x: 0, y: 8, w: 12, h: 4 }, unit: "", values: [], error: "metrics.jsonl: not found" },
    { id: "4", title: "Empty", type: "stat", grid: { x: 12, y: 8, w: 12, h: 4 }, unit: "", values: [] },
  ],
};
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return { client, ...render(<QueryClientProvider client={client}><NoteVaultContext.Provider value="ops"><MetricsDashboard text={spec} /></NoteVaultContext.Provider></QueryClientProvider>) };
}
afterEach(() => vi.resetAllMocks());

describe("MetricsDashboard", () => {
  it("renders resolved values, chart, table, freshness and isolated panel failures in grid order", async () => {
    mockRender.mockResolvedValue(dashboard);
    const { container } = mount();
    expect(screen.getByRole("status")).toHaveTextContent("Loading dashboard");
    const stat = await screen.findByRole("region", { name: "CPU" });
    expect(within(stat).getByText("82%")).toBeInTheDocument();
    expect(within(stat).getByText("At or above 80%")).toBeInTheDocument();
    expect(stat).toHaveStyle({ gridColumn: "13 / span 12", gridRow: "1 / span 4" });
    expect(container.querySelector(".metrics-dashboard-grid")?.firstElementChild).toHaveAttribute("aria-label", "History");
    expect(screen.getByRole("img", { name: "Resolved chart" })).toHaveTextContent('"type":"line"');
    expect(within(screen.getByRole("table")).getByRole("cell", { name: "worker-a" })).toBeInTheDocument();
    expect(screen.getByText(/Latest sample:/)).toHaveTextContent(value.time);
    expect(screen.getByRole("alert")).toHaveTextContent("metrics.jsonl: not found");
    expect(within(screen.getByRole("region", { name: "Empty" })).getByText("No data for this selection.")).toBeInTheDocument();
    expect(mockRender).toHaveBeenCalledWith(spec, { from: "", to: "", entity: "", metric: "" }, "ops");
  });

  it("shares filters across panels, retains target choices and hides old values while selecting", async () => {
    mockRender.mockResolvedValue(dashboard);
    mount();
    await screen.findByRole("region", { name: "CPU" });
    mockRender.mockImplementation(() => new Promise(() => {}));
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-09-01" } });
    await waitFor(() => expect(mockRender).toHaveBeenLastCalledWith(spec, { from: "2026-09-01", to: "", entity: "", metric: "" }, "ops"));
    expect(screen.queryByRole("region", { name: "CPU" })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "worker-b" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-09-19" } });
    fireEvent.change(screen.getByLabelText("Target"), { target: { value: "worker-b" } });
    fireEvent.change(screen.getByLabelText("Metric"), { target: { value: "cpu" } });
    await waitFor(() => expect(mockRender).toHaveBeenLastCalledWith(spec, { from: "2026-09-01", to: "2026-09-19", entity: "worker-b", metric: "cpu" }, "ops"));
    mockRender.mockResolvedValue(dashboard);
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    await screen.findByRole("region", { name: "CPU" });
    expect(screen.getByLabelText("From")).toHaveValue("");
    expect(screen.getByLabelText("Target")).toHaveValue("");
    expect(screen.getByLabelText("Metric")).toHaveValue("");
  });

  it("refreshes on demand and invalidation, replacing stale values with a fetch error", async () => {
    mockRender.mockResolvedValue(dashboard);
    const { client } = mount();
    await screen.findByRole("region", { name: "CPU" });
    mockRender.mockResolvedValue({ ...dashboard, asof: "2026-09-19T12:00:00Z" });
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(screen.getByText(/Latest sample:/)).toHaveTextContent("2026-09-19"));
    mockRender.mockRejectedValue(new Error("source unavailable"));
    await client.invalidateQueries({ queryKey: ["metrics-dashboard"] });
    await screen.findByText("Dashboard error: source unavailable");
    expect(screen.queryByRole("region", { name: "CPU" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy code" })).toBeInTheDocument();
    expect(mockRender).toHaveBeenCalledTimes(3);
  });
});
