import { act, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EChartsBlock } from "./EChartsBlock";

const charts = vi.hoisted(() => new Map<HTMLElement, {resize: ReturnType<typeof vi.fn>; setOption: ReturnType<typeof vi.fn>; dispose: ReturnType<typeof vi.fn>}>());
vi.mock("echarts", () => ({
  getInstanceByDom: (el: HTMLElement) => charts.get(el),
  init: (el: HTMLElement) => {
    const chart = {resize:vi.fn(),setOption:vi.fn(),dispose:vi.fn(() => charts.delete(el)),off:vi.fn(),on:vi.fn()};
    charts.set(el,chart); return chart;
  },
}));
vi.mock("@tanstack/react-router", () => ({useNavigate: () => undefined}));

class Observer {
  static all: Observer[] = [];
  disconnected = false;
  constructor(public callback: () => void) { Observer.all.push(this); }
  observe() {}
  disconnect() { this.disconnected = true; }
}

afterEach(() => { vi.unstubAllGlobals(); charts.clear(); Observer.all = []; });
describe("dashboard chart lifecycle", () => {
  it("reuses charts on refresh, handles narrow/wide resize, and disposes on repeated tab close", async () => {
    vi.stubGlobal("ResizeObserver",Observer);
    for (let navigation = 0; navigation < 3; navigation++) {
      const view = render(<EChartsBlock option={{title:{text:"Level"},series:[]}} />);
      await waitFor(() => expect(charts.size).toBe(1));
      const chart = [...charts.values()][0];
      const firstCalls = chart.setOption.mock.calls.length;
      view.rerender(<EChartsBlock option={{title:{text:"Updated level"},series:[]}} />);
      await waitFor(() => expect(chart.setOption.mock.calls.length).toBeGreaterThan(firstCalls));
      expect(charts.size).toBe(1);
      for (const width of [1,80,800]) {
        Object.defineProperty(view.container.querySelector(".viewspec-chart"),"clientWidth",{value:width,configurable:true});
        await act(async () => { Observer.all.filter((o) => !o.disconnected).forEach((o) => o.callback()); });
      }
      expect(chart.resize).toHaveBeenCalled();
      view.unmount();
      await waitFor(() => expect(charts.size).toBe(0));
      expect(chart.dispose).toHaveBeenCalledTimes(1);
      expect(Observer.all.every((o) => o.disconnected)).toBe(true);
    }
  });
});
