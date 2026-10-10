import { afterEach, describe, expect, it, vi } from "vitest";
import { renderLikeC4 } from "./likec4Engine";

class TestWorker {
  static latest: TestWorker;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: ErrorEvent) => void) | null = null;
  postMessage = vi.fn();
  terminate = vi.fn();
  constructor() { TestWorker.latest = this; }
}

describe("LikeC4 worker lifecycle", () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });

  it("terminates after rendering and sanitizes every returned view", async () => {
    vi.stubGlobal("Worker", TestWorker);
    const result = renderLikeC4("model", new AbortController().signal);
    const worker = TestWorker.latest;
    expect(worker.postMessage).toHaveBeenCalledWith("model");
    worker.onmessage!({ data: { status: "ready", views: [
      { id: "a", title: "A", svg: '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><text>A</text></svg>' },
    ] } } as MessageEvent);
    expect((await result)[0].svg).not.toContain("script");
    expect(worker.terminate).toHaveBeenCalledOnce();
  });

  it("terminates a stuck worker after the deadline", async () => {
    vi.stubGlobal("Worker", TestWorker);
    vi.useFakeTimers();
    const result = renderLikeC4("model", new AbortController().signal);
    const rejected = expect(result).rejects.toThrow("30 seconds");
    await vi.advanceTimersByTimeAsync(30_000);
    await rejected;
    expect(TestWorker.latest.terminate).toHaveBeenCalledOnce();
  });

  it("terminates when the source is replaced", async () => {
    vi.stubGlobal("Worker", TestWorker);
    const controller = new AbortController();
    const result = renderLikeC4("model", controller.signal);
    controller.abort();
    await expect(result).rejects.toMatchObject({ name: "AbortError" });
    expect(TestWorker.latest.terminate).toHaveBeenCalledOnce();
  });

  it("rejects oversized input without starting a worker", async () => {
    const constructor = vi.fn();
    vi.stubGlobal("Worker", constructor);
    await expect(renderLikeC4(" ".repeat(100_001), new AbortController().signal)).rejects.toThrow("100,000 characters");
    expect(constructor).not.toHaveBeenCalled();
  });
});
