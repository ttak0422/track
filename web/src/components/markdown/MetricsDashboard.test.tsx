import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderMetricsDashboard, type MetricsDashboardResponse } from "../../api";
import { MetricsDashboard, MetricsSnapshot } from "./MetricsDashboard";
import { NoteVaultContext } from "./context";

vi.mock("../../api", () => ({ renderMetricsDashboard: vi.fn() }));
vi.mock("./EChartsBlock", () => ({ EChartsBlock: ({option}: {option: {title?: {text: string}}}) => <div role="img" aria-label={option.title?.text} /> }));
const view: MetricsDashboardResponse = {title:"Sectors",panels:[{title:"Level",x:0,width:12,source:"sectors.jsonl",unit:"pct",latest_observation:"2026-10-08",points:3,missing:1,gaps:[{time:"2026-10-08",entity:"A",reason:"not published"}],series:[{entity:"A",frequency:"weekly",scope:"sector"}],echarts:{title:{text:"Level"}}}]};
function mount() {
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}});
  const result = render(<QueryClientProvider client={client}><NoteVaultContext value="research"><MetricsDashboard text='{"title":"Sectors"}' /></NoteVaultContext></QueryClientProvider>);
  return {...result,client};
}

describe("saved metrics dashboard", () => {
 it("keeps the saved definition while changing periods and refreshing data", async () => {
  vi.mocked(renderMetricsDashboard).mockResolvedValue(view);
  const first = mount();
  await screen.findByRole("heading",{name:"Sectors"});
  expect(renderMetricsDashboard).toHaveBeenLastCalledWith('{"title":"Sectors"}',0,"research");
  expect(screen.getByText("weekly", {selector:"dd"})).toBeInTheDocument();
  expect(screen.getByText("sector", {selector:"dd"})).toBeInTheDocument();
  expect(screen.getByText(/Latest observation:/)).toHaveTextContent("1 missing");
  expect(first.container.querySelector(".metrics-dashboard-panel")).toHaveStyle({gridColumn:"1 / span 12"});
  fireEvent.change(screen.getByRole("combobox"),{target:{value:"7"}});
  await waitFor(() => expect(renderMetricsDashboard).toHaveBeenLastCalledWith('{"title":"Sectors"}',7,"research"));
  const calls = vi.mocked(renderMetricsDashboard).mock.calls.length;
  await first.client.invalidateQueries({queryKey:["metrics-dashboard"]});
  expect(vi.mocked(renderMetricsDashboard).mock.calls.length).toBeGreaterThan(calls);
  first.unmount();
  const second = mount();
  await screen.findByRole("img",{name:"Level"});
  second.unmount();
 });
 it("shows deterministic failures and empty periods explicitly", async () => {
  vi.mocked(renderMetricsDashboard).mockRejectedValueOnce(new Error("unknown metric"));
  const first = mount();
  expect(await screen.findByRole("alert")).toHaveTextContent("unknown metric");
  first.unmount();
  vi.mocked(renderMetricsDashboard).mockResolvedValue({...view,panels:[{...view.panels[0],points:0}]});
  mount();
  await screen.findByText("No observations in this period.");
  expect(screen.queryByRole("img")).toBeNull();
 });
 it("renders published snapshots without a data request", () => {
  vi.mocked(renderMetricsDashboard).mockClear();
  render(<MetricsSnapshot text={JSON.stringify(view)} />);
  expect(screen.getByText(/Published snapshot/)).toBeInTheDocument();
  expect(screen.getByRole("img",{name:"Level"})).toBeInTheDocument();
  expect(renderMetricsDashboard).not.toHaveBeenCalled();
 });
});

it("falls back to source for malformed snapshot metadata", () => {
  const malformed = {...view,panels:[{...view.panels[0],unit:{unexpected:"object"}}]};
  const result = render(<MetricsSnapshot text={JSON.stringify(malformed)} />);
  expect(screen.queryByRole("img")).toBeNull();
  expect(result.container.querySelector(".code-block")).not.toBeNull();
});
