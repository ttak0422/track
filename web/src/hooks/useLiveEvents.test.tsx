import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useLiveEvents } from "./useLiveEvents";
import { listNewNotes, renderMetricsDashboard, renderViewSpec } from "../api";
import { useNewNotesQuery } from "../queries";
import { MetricsDashboard } from "../components/markdown/MetricsDashboard";
import { ViewSpecChart } from "../components/markdown/ViewSpecChart";
import { NoteVaultContext } from "../components/markdown/context";
import type { MetricsDashboardResponse } from "../api";
import type { NotesResponse } from "../types";

vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  listNewNotes: vi.fn(async () => ({ notes: [] })),
  renderMetricsDashboard: vi.fn(),
  renderViewSpec: vi.fn(),
}));

vi.mock("../components/markdown/EChartsBlock", () => ({ EChartsBlock: () => null }));

// jsdom has no EventSource; a fake capturing listeners lets the tests emit server events directly.
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  listeners = new Map<string, () => void>();
  closed = false;
  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }
  addEventListener(name: string, fn: () => void) {
    this.listeners.set(name, fn);
  }
  emit(name: string) {
    this.listeners.get(name)?.();
  }
  close() {
    this.closed = true;
  }
}

function renderLiveEvents(withNew = false) {
  const client = new QueryClient();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => {
    useLiveEvents();
    return withNew ? useNewNotesQuery(100, "work") : undefined;
  }, { wrapper });
  const source = FakeEventSource.instances[0];
  expect(source).toBeDefined();
  return { invalidate, source, ...view };
}

describe("useLiveEvents", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    vi.mocked(listNewNotes).mockReset().mockResolvedValue({ notes: [] });
    vi.mocked(renderMetricsDashboard).mockReset();
    vi.mocked(renderViewSpec).mockReset();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("invalidates note queries on a change event", () => {
    const { invalidate, source } = renderLiveEvents();
    source.emit("change");
    vi.advanceTimersByTime(150);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["note"] });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ["viewspec"] });
  });

  it("refreshes the task listings too, since a checkbox can be written from anywhere", () => {
    const { invalidate, source } = renderLiveEvents();
    source.emit("change");
    vi.advanceTimersByTime(150);
    // The prefix covers both the dated listing (calendar, day page) and the open one (tasks page).
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["tasks"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["calendar-events"] });
  });

  it("replaces a pre-event initial New request after a burst of indexed changes", async () => {
    let resolveStale!: (data: NotesResponse) => void;
    vi.mocked(listNewNotes).mockReturnValueOnce(new Promise((resolve) => { resolveStale = resolve; }));
    const { source, result } = renderLiveEvents(true);
    const fresh: NotesResponse = { notes: [
      { note_id: "work~3", title: "Newest", file_kind: "note", path: "note/3.md" },
      { note_id: "work~2", title: "New", file_kind: "note", path: "note/2.md" },
    ] };
    vi.mocked(listNewNotes).mockResolvedValue(fresh);
    source.emit("change");
    source.emit("change");
    await act(async () => { await vi.advanceTimersByTimeAsync(200); });
    expect(listNewNotes).toHaveBeenCalledTimes(2);
    expect(listNewNotes).toHaveBeenLastCalledWith(100, "work");
    expect(result.current?.data).toEqual(fresh);
    await act(async () => { resolveStale({ notes: [] }); });
    await act(async () => { await vi.advanceTimersByTimeAsync(50); });
    expect(result.current?.data).toEqual(fresh);
  });

  it("refreshes New on connection and reconnect because missed events are not replayed", async () => {
    const { source } = renderLiveEvents(true);
    await act(async () => { await vi.advanceTimersByTimeAsync(50); });
    expect(listNewNotes).toHaveBeenCalledTimes(1);
    for (const calls of [2, 3]) {
      source.emit("open");
      await act(async () => { await vi.advanceTimersByTimeAsync(200); });
      expect(listNewNotes).toHaveBeenCalledTimes(calls);
    }
  });

  it("clears queued change and reconnect refreshes when the stream unmounts", async () => {
    const { source, invalidate, unmount } = renderLiveEvents();
    source.emit("open");
    source.emit("change");
    unmount();
    await vi.advanceTimersByTimeAsync(200);
    expect(source.closed).toBe(true);
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("refreshes charts and dashboards after reconnect", async () => {
    const { invalidate, source } = renderLiveEvents();
    source.emit("open");
    await act(async () => { await vi.advanceTimersByTimeAsync(150); });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["viewspec"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["metrics-dashboard"] });
  });

  it("invalidates chart and dashboard queries on a data event, debouncing bursts", async () => {
    const { invalidate, source, unmount } = renderLiveEvents();
    // A burst of writes under data/ coalesces into a single chart refresh.
    source.emit("data");
    source.emit("data");
    expect(invalidate).not.toHaveBeenCalled();
    await act(async () => { await vi.advanceTimersByTimeAsync(150); });
    expect(invalidate).toHaveBeenCalledTimes(2);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["viewspec"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["metrics-dashboard"] });

    unmount();
    expect(source.closed).toBe(true);
  });
  it.each(["data", "open"])("replaces both pending initial chart responses after %s", async (event) => {
    let resolveOldDashboard!: (data: MetricsDashboardResponse) => void;
    let resolveOldChart!: (data: { echarts: Record<string, unknown> }) => void;
    vi.mocked(renderMetricsDashboard).mockReturnValueOnce(new Promise((resolve) => { resolveOldDashboard = resolve; }));
    vi.mocked(renderViewSpec).mockReturnValueOnce(new Promise((resolve) => { resolveOldChart = resolve; }));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const dashboardText = JSON.stringify({title:"Metrics",panels:[{type:"timeseries",datasource:{type:"track",uid:"m.jsonl"},targets:[{expr:"m"}]}]});
    const specText = JSON.stringify({version:2,mark:"line",data:{source:"m.jsonl",kind:"metric"},encoding:{x:{field:"time"},y:[{field:"value"}]}});
    function Fixture() {
      useLiveEvents();
      return <NoteVaultContext value="research"><MetricsDashboard text={dashboardText} /><ViewSpecChart text={specText} /></NoteVaultContext>;
    }
    render(<QueryClientProvider client={client}><Fixture /></QueryClientProvider>);
    expect(renderMetricsDashboard).toHaveBeenCalledTimes(1);
    expect(renderViewSpec).toHaveBeenCalledTimes(1);
    const freshDashboard: MetricsDashboardResponse = {title:"Fresh",panels:[]};
    const freshChart = {echarts:{title:{text:"Fresh"}}};
    vi.mocked(renderMetricsDashboard).mockResolvedValue(freshDashboard);
    vi.mocked(renderViewSpec).mockResolvedValue(freshChart);
    const source = FakeEventSource.instances[0];
    source.emit(event);
    source.emit(event);
    await act(async () => { await vi.advanceTimersByTimeAsync(200); });
    expect(renderMetricsDashboard).toHaveBeenCalledTimes(2);
    expect(renderMetricsDashboard).toHaveBeenLastCalledWith(dashboardText,0,"research");
    expect(renderViewSpec).toHaveBeenCalledTimes(2);
    expect(renderViewSpec).toHaveBeenLastCalledWith(specText,"research");
    expect(client.getQueriesData({queryKey:["metrics-dashboard"]})[0][1]).toEqual(freshDashboard);
    expect(client.getQueriesData({queryKey:["viewspec"]})[0][1]).toEqual(freshChart);
    await act(async () => {
      resolveOldDashboard({title:"Stale",panels:[]});
      resolveOldChart({echarts:{title:{text:"Stale"}}});
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(client.getQueriesData({queryKey:["metrics-dashboard"]})[0][1]).toEqual(freshDashboard);
    expect(client.getQueriesData({queryKey:["viewspec"]})[0][1]).toEqual(freshChart);
  });

});
