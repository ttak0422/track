import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  attachDetailTooltip,
  detailTooltipFormatter,
  EChartsBlock,
  suppressBoxLabels,
} from "./EChartsBlock";

// The mock chart records the handlers EChartsBlock registers per event, so tests can drive a datum
// click (or a zoom) without a real canvas. convertToPixel spreads anchors so the rail lays out.
const handlers = vi.hoisted(
  () => ({}) as Record<string, ((params: unknown) => void) | undefined>,
);
const setOption = vi.hoisted(() => vi.fn());
vi.mock("echarts", () => ({
  init: vi.fn(() => ({
    setOption,
    resize: vi.fn(),
    dispose: vi.fn(),
    off: vi.fn((event: string) => {
      handlers[event] = undefined;
    }),
    on: vi.fn((event: string, fn: (params: unknown) => void) => {
      handlers[event] = fn;
    }),
    convertToPixel: vi.fn((_finder: unknown, at: unknown) => 100 + String(at).charCodeAt(0)),
    containPixel: vi.fn(() => true),
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

  it("keeps today's exact tree for options without box markers", async () => {
    const { container } = await renderChart({ series: [] });
    expect(container.querySelector(".viewspec-chart-wrap")).toBeNull();
    expect(container.querySelector(".viewspec-chart")).not.toBeNull();
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

// A ready-to-draw option with two box-mode markers, as the engine emits them (ADR 0028).
function boxOption(): Record<string, unknown> {
  return {
    xAxis: { type: "category", data: ["a", "b", "c"] },
    series: [
      {
        type: "line",
        data: [1, 2, 3],
        markLine: {
          data: [
            {
              xAxis: "a",
              label: { formatter: "First event" },
              href: "https://example.com/1",
              note: "123",
              box: { date: "2026-01-02", host: "example.com" },
            },
            { xAxis: "c", label: { formatter: "Second event" }, box: { date: "c" } },
          ],
        },
      },
    ],
  };
}

describe("annotation rail", () => {
  afterEach(() => {
    vi.clearAllMocks();
    handlers.click = undefined;
  });

  it("renders box-mode markers as an evidence rail beside the chart host", async () => {
    const { container, getByText } = await renderChart(boxOption());
    expect(container.querySelector(".viewspec-chart-wrap")).not.toBeNull();
    expect(container.querySelectorAll(".chart-annotation")).toHaveLength(2);
    expect(getByText("2026-01-02")).toBeTruthy();
    expect(getByText("First event")).toBeTruthy();
    const source = getByText("example.com ↗") as HTMLAnchorElement;
    expect(source.getAttribute("href")).toBe("https://example.com/1");
    expect(source.getAttribute("rel")).toContain("noopener");
  });

  it("suppresses the canvas label on the drawn clone, never the shared option", async () => {
    const option = boxOption();
    await renderChart(option);
    const drawn = setOption.mock.calls[0][0] as {
      series: { markLine: { data: { label?: unknown }[] } }[];
    };
    expect(drawn.series[0].markLine.data[0].label).toEqual({ show: false });
    const original = option.series as { markLine: { data: { label?: unknown }[] } }[];
    expect(original[0].markLine.data[0].label).toEqual({ formatter: "First event" });
  });

  it("navigates to the referenced note from a box's note chip", async () => {
    const { getAllByRole } = await renderChart(boxOption());
    getAllByRole("button", { name: "note" })[0].click();
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "123" } });
  });
});

describe("suppressBoxLabels", () => {
  it("touches only box-mode items", () => {
    const option = boxOption();
    const plain = { xAxis: "b", label: { formatter: "classic" } };
    (
      (option.series as { markLine: { data: unknown[] } }[])[0].markLine.data
    ).push(plain);
    suppressBoxLabels(option);
    const data = (option.series as { markLine: { data: { label?: unknown }[] } }[])[0].markLine
      .data;
    expect(data[0].label).toEqual({ show: false });
    expect(data[2].label).toEqual({ formatter: "classic" });
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
