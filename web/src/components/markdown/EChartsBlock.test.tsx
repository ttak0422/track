import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { attachDetailTooltip, detailTooltipFormatter, EChartsBlock } from "./EChartsBlock";

// The mock chart records the click handler EChartsBlock registers, so tests can drive a datum click
// without a real canvas.
const handlers = vi.hoisted(() => ({ click: undefined as ((params: unknown) => void) | undefined }));
const setOption = vi.hoisted(() => vi.fn());
vi.mock("echarts", () => ({
  init: vi.fn(() => ({
    setOption,
    resize: vi.fn(),
    dispose: vi.fn(),
    off: vi.fn(() => {
      handlers.click = undefined;
    }),
    on: vi.fn((_event: string, fn: (params: unknown) => void) => {
      handlers.click = fn;
    }),
  })),
  getInstanceByDom: vi.fn(() => undefined),
}));

const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

async function renderChart(option: Record<string, unknown>) {
  const result = render(<EChartsBlock option={option} />);
  await waitFor(() => expect(handlers.click).toBeDefined());
  return result;
}

describe("EChartsBlock provenance clicks", () => {
  afterEach(() => {
    vi.clearAllMocks();
    handlers.click = undefined;
  });

  it("opens a datum's source URL in a new tab (http(s) only)", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    await renderChart({ series: [] });

    handlers.click?.({ data: { href: "https://example.com/report" } });
    expect(open).toHaveBeenCalledWith("https://example.com/report", "_blank", "noopener,noreferrer");

    open.mockClear();
    handlers.click?.({ data: { href: "javascript:alert(1)" } });
    expect(open).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it("navigates to a datum's referenced note when it carries no URL", async () => {
    await renderChart({ series: [] });
    handlers.click?.({ data: { note: "1783269749000" } });
    expect(navigate).toHaveBeenCalledWith({
      to: "/notes/$noteId",
      params: { noteId: "1783269749000" },
    });
  });

  it("prefers the source URL when a datum carries both", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    await renderChart({ series: [] });
    handlers.click?.({ data: { href: "https://example.com/a", note: "123" } });
    expect(open).toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it("ignores clicks on plain datums", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    await renderChart({ series: [] });
    handlers.click?.({ data: 3 });
    handlers.click?.({});
    expect(open).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
    open.mockRestore();
  });
});

describe("detail tooltip", () => {
  it("installs the formatter only when a datum carries detail rows", () => {
    const plain: Record<string, unknown> = { series: [{ data: [1, 2] }], tooltip: { trigger: "axis" } };
    attachDetailTooltip(plain);
    expect((plain.tooltip as Record<string, unknown>).formatter).toBeUndefined();

    const detailed: Record<string, unknown> = {
      series: [{ data: [{ value: 1, detail: [{ label: "who", value: "A" }] }] }],
      tooltip: { trigger: "axis" },
    };
    attachDetailTooltip(detailed);
    expect((detailed.tooltip as Record<string, unknown>).formatter).toBe(detailTooltipFormatter);
  });

  it("renders header, series lines, and escaped detail rows without duplicates", () => {
    const param = (series: string) => ({
      axisValueLabel: "2026-02-02",
      seriesName: series,
      marker: "<span>*</span>",
      value: 5,
      data: { detail: [{ label: "note<b>", value: 'says "hi"' }] },
    });
    const html = detailTooltipFormatter([param("amount"), param("index")]);
    expect(html).toContain("2026-02-02");
    expect(html).toContain("<span>*</span>amount: 5");
    expect(html).toContain("index: 5");
    // Detail values are escaped and deduped across series sharing the record.
    expect(html).toContain("note&lt;b&gt;: says &quot;hi&quot;");
    expect(html.match(/note&lt;b&gt;/g)).toHaveLength(1);
  });
});
