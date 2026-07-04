import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useLiveEvents } from "./useLiveEvents";

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

function renderLiveEvents() {
  const client = new QueryClient();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => useLiveEvents(), { wrapper });
  const source = FakeEventSource.instances[0];
  expect(source).toBeDefined();
  return { invalidate, source, ...view };
}

describe("useLiveEvents", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
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

  it("invalidates only viewspec queries on a data event, debouncing bursts", () => {
    const { invalidate, source, unmount } = renderLiveEvents();
    // A burst of writes under data/ coalesces into a single chart refresh.
    source.emit("data");
    source.emit("data");
    expect(invalidate).not.toHaveBeenCalled();
    vi.advanceTimersByTime(150);
    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["viewspec"] });

    unmount();
    expect(source.closed).toBe(true);
  });
});
