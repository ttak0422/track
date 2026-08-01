import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The loader memoizes its injection promise in module scope, so each test imports a fresh module.
// jsdom never fetches script src, so the tests fire load/error on the injected tag themselves.
async function freshLoader() {
  return (await import("./drawioViewer")).loadDrawioViewer;
}

function injected(): HTMLScriptElement[] {
  return [...document.head.querySelectorAll("script")];
}

function fire(script: HTMLScriptElement, type: "load" | "error") {
  script.dispatchEvent(new Event(type));
}

const graphViewer = { createViewerForElement: () => {} };

describe("loadDrawioViewer", () => {
  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    for (const script of injected()) script.remove();
    delete window.GraphViewer;
    delete window.MathJax;
    delete window.Editor;
    delete window.PROXY_URL;
    delete window.STYLE_PATH;
    delete window.SHAPES_PATH;
    delete window.STENCIL_PATH;
    delete window.DRAW_MATH_URL;
  });

  it("injects the vendored script and neutralizes the diagrams.net fallbacks first", async () => {
    const loadDrawioViewer = await freshLoader();
    const pending = loadDrawioViewer();

    expect(injected()).toHaveLength(1);
    expect(injected()[0].getAttribute("src")).toBe(
      `${import.meta.env.BASE_URL}drawio-viewer-static.min.js`,
    );
    // Set before the script can run: the viewer resolves these as `window.X || "https://…"`, so a
    // falsy or late value phones viewer.diagrams.net (ADR 0065). A predefined MathJax keeps
    // Editor.initMath() a no-op.
    const dead = `${import.meta.env.BASE_URL}drawio-absent`;
    expect([
      window.PROXY_URL,
      window.STYLE_PATH,
      window.SHAPES_PATH,
      window.STENCIL_PATH,
      window.DRAW_MATH_URL,
    ]).toEqual([dead, dead, dead, dead, dead]);
    expect(window.MathJax).toBeDefined();

    window.GraphViewer = graphViewer;
    window.Editor = {};
    fire(injected()[0], "load");
    await expect(pending).resolves.toBe(graphViewer);
    // One export path calls Editor.MathJaxRender unguarded; with initMath skipped nothing defines it.
    expect(typeof window.Editor.MathJaxRender).toBe("function");
  });

  it("injects once and shares the promise across callers", async () => {
    const loadDrawioViewer = await freshLoader();
    const first = loadDrawioViewer();
    const second = loadDrawioViewer();
    expect(injected()).toHaveLength(1);

    window.GraphViewer = graphViewer;
    fire(injected()[0], "load");
    expect(await first).toBe(graphViewer);
    expect(await second).toBe(graphViewer);
    expect(await loadDrawioViewer()).toBe(graphViewer);
    expect(injected()).toHaveLength(1);
  });

  it("resolves from an already-loaded global without injecting anything", async () => {
    window.GraphViewer = graphViewer;
    const loadDrawioViewer = await freshLoader();
    await expect(loadDrawioViewer()).resolves.toBe(graphViewer);
    expect(injected()).toHaveLength(0);
    expect(window.PROXY_URL).toBeUndefined();
  });

  it("retries on the next call after a failed fetch", async () => {
    const loadDrawioViewer = await freshLoader();
    const failed = loadDrawioViewer();
    fire(injected()[0], "error");
    await expect(failed).rejects.toThrow("failed to load the draw.io viewer");

    // The memo is cleared, so a later diagram gets a fresh script instead of the poisoned promise.
    const retry = loadDrawioViewer();
    expect(injected()).toHaveLength(2);
    window.GraphViewer = graphViewer;
    fire(injected()[1], "load");
    await expect(retry).resolves.toBe(graphViewer);
  });

  it("rejects when the script loads without defining GraphViewer, and stays rejected", async () => {
    const loadDrawioViewer = await freshLoader();
    const pending = loadDrawioViewer();
    fire(injected()[0], "load");
    await expect(pending).rejects.toThrow("draw.io viewer loaded without GraphViewer");
    // A script that loaded but defined no global is a broken build, not a transient failure:
    // re-fetching the same URL would fail the same way, so the memo stays.
    await expect(loadDrawioViewer()).rejects.toThrow("draw.io viewer loaded without GraphViewer");
    expect(injected()).toHaveLength(1);
  });
});
