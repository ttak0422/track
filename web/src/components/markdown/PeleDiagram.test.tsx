import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MermaidDiagram } from "./MermaidDiagram";
import { peleFallbackReason } from "./PeleDiagram";
import { renderPele } from "./peleEngine";

const mermaidImport = vi.hoisted(() => vi.fn());
vi.mock("mermaid", () => { mermaidImport(); return { default: {
  initialize: vi.fn(), registerIconPacks: vi.fn(),
  render: vi.fn(async () => ({ svg: '<svg viewBox="0 0 200 80"><text>Mermaid output</text></svg>' })),
} }; });
vi.mock("./peleEngine", () => ({ renderPele: vi.fn(async () => ({
  svg: '<svg class="pele" viewBox="0 0 200 80"><text>Pele output</text></svg>',
  popupSvg: '<svg class="pele" viewBox="0 0 200 80"><text>Pele popup</text></svg>',
})) }));

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute("data-theme");
  document.documentElement.removeAttribute("style");
});

describe("Pele default rendering", () => {
  it("uses Pele by default without importing Mermaid and copies identical source", async () => {
    const source = 'flowchart LR\nA["日本語のラベル"] --> B[Done]\n';
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    render(<MermaidDiagram text={source} />);
    expect(await screen.findByText("Pele output")).toBeInTheDocument();
    expect(renderPele).toHaveBeenCalledWith(source, expect.any(String), expect.any(String));
    fireEvent.click(screen.getByRole("button", { name: "Copy source" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(source));
    expect(mermaidImport).not.toHaveBeenCalled();
    expect(screen.queryByRole("group", { name: "Diagram renderer" })).not.toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Pele diagram" })).toBeInTheDocument();
  });

  it.each([
    ['flowchart LR\nA["$$x^2$$"]', "Math labels"],
    ['architecture-beta\nservice api(logos:aws-lambda)[API]', "Custom icons"],
    ['flowchart LR\nA["<b>HTML</b>"]', "HTML labels"],
    ['---\nconfig:\n  theme: dark\n---\nflowchart LR\nA-->B', "frontmatter"],
    ['%%{init: {"theme":"dark"}}%%\nflowchart LR\nA-->B', "directives"],
  ])("explicitly retains the existing renderer for %s", async (source, reason) => {
    render(<MermaidDiagram text={source} />);
    expect(screen.getByRole("status")).toHaveTextContent(reason);
    expect(await screen.findByText("Mermaid output")).toBeInTheDocument();
    expect(renderPele).not.toHaveBeenCalled();
    expect(peleFallbackReason(source)).not.toBeNull();
  });

  it("falls back visibly on failure and retries with changed source", async () => {
    vi.mocked(renderPele).mockRejectedValueOnce(new Error("Unsupported layout"));
    const { rerender } = render(<MermaidDiagram text="flowchart LR\nA-->B" />);
    expect(await screen.findByRole("status")).toHaveTextContent("Unsupported layout");
    expect(await screen.findByText("Mermaid output")).toBeInTheDocument();
    rerender(<MermaidDiagram text="flowchart LR\nA-->C" />);
    expect(await screen.findByText("Pele output")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("keeps the same SVG on color changes and measures again when the font changes", async () => {
    const { container } = render(<MermaidDiagram text="flowchart LR\nA-->B" />);
    await screen.findByText("Pele output");
    const svg = container.querySelector("svg.pele");
    await act(async () => { document.documentElement.setAttribute("data-theme", "dark"); });
    expect(renderPele).toHaveBeenCalledTimes(1);
    expect(container.querySelector("svg.pele")).toBe(svg);
    await act(async () => { document.documentElement.style.setProperty("--font-sans", "serif"); });
    await waitFor(() => expect(renderPele).toHaveBeenCalledTimes(2));
    expect(renderPele).toHaveBeenLastCalledWith(expect.any(String), "serif", expect.any(String));
    await act(async () => { (container.querySelector(".pele-diagram") as HTMLElement).style.setProperty("--pele-font", "monospace"); });
    await waitFor(() => expect(renderPele).toHaveBeenCalledTimes(3));
    expect(renderPele).toHaveBeenLastCalledWith(expect.any(String), "monospace", expect.any(String));
  });

  it("retains the shared zoom, reset and popup controls", async () => {
    const { container } = render(<MermaidDiagram text="flowchart LR\nA-->B" />);
    await screen.findByText("Pele output");
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(container.querySelector(".mermaid-pan")?.getAttribute("style")).toContain("scale(1.3)");
    fireEvent.click(screen.getByRole("button", { name: "Reset diagram view" }));
    expect(container.querySelector(".mermaid-pan")?.getAttribute("style")).toContain("scale(1)");
    fireEvent.click(screen.getByRole("button", { name: "Open diagram in popup" }));
    expect(await screen.findByText("Pele popup")).toBeInTheDocument();
    expect(screen.getAllByRole("img", { name: "Pele diagram" })).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Close diagram popup" }));
    expect(screen.queryByText("Pele popup")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open diagram in popup" }));
    expect(await screen.findByText("Pele popup")).toBeInTheDocument();
  });
  it("retries an earlier failed source after leaving it and closes a previous popup", async () => {
    const source = "flowchart LR\nA-->B";
    vi.mocked(renderPele).mockRejectedValueOnce(new Error("Transient failure"));
    const { rerender } = render(<MermaidDiagram text={source} />);
    await screen.findByText("Mermaid output");
    rerender(<MermaidDiagram text="flowchart LR\nA-->C" />);
    await screen.findByText("Pele output");
    fireEvent.click(screen.getByRole("button", { name: "Open diagram in popup" }));
    await screen.findByText("Pele popup");
    rerender(<MermaidDiagram text={source} />);
    await screen.findByText("Pele output");
    expect(screen.queryByText("Pele popup")).not.toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(renderPele).toHaveBeenCalledTimes(3);
  });

  it.each(["success", "failure"])("ignores a stale Pele %s after the source changes", async (outcome) => {
    let resolve!: (value: { svg: string; popupSvg: string }) => void;
    let reject!: (reason: Error) => void;
    vi.mocked(renderPele).mockImplementationOnce(() => new Promise((yes, no) => { resolve = yes; reject = no; }));
    const { rerender } = render(<MermaidDiagram text="flowchart LR\nA-->B" />);
    await waitFor(() => expect(renderPele).toHaveBeenCalledTimes(1));
    rerender(<MermaidDiagram text="flowchart LR\nA-->C" />);
    await screen.findByText("Pele output");
    await act(async () => {
      if (outcome === "failure") reject(new Error("Stale failure"));
      else resolve({ svg: "<svg><text>Stale output</text></svg>", popupSvg: "" });
    });
    expect(screen.getByText("Pele output")).toBeInTheDocument();
    expect(screen.queryByText("Stale output")).not.toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("keeps source and error visible when both engines fail", async () => {
    const { default: mermaid } = await import("mermaid");
    vi.mocked(renderPele).mockRejectedValueOnce(new Error("Unsupported syntax"));
    vi.mocked(mermaid.render).mockRejectedValueOnce(new Error("Parse error"));
    const source = "flowchart LR\ninvalid";
    const { container } = render(<MermaidDiagram text={source} />);
    expect(await screen.findByText("Mermaid render failed: Parse error")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Unsupported syntax");
    expect(container.querySelector("code")).toHaveTextContent("invalid");
  });
  it("waits for the viewport before loading the default engine", async () => {
    let enter!: IntersectionObserverCallback;
    vi.stubGlobal("IntersectionObserver", class {
      constructor(callback: IntersectionObserverCallback) { enter = callback; }
      observe() {}
      disconnect() {}
    });
    render(<MermaidDiagram text="flowchart LR\nA-->B" />);
    await act(async () => {});
    expect(renderPele).not.toHaveBeenCalled();
    await act(async () => enter([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver));
    await screen.findByText("Pele output");
    expect(renderPele).toHaveBeenCalledTimes(1);
  });

  it("measures again when a webfont finishes loading", async () => {
    const original = Object.getOwnPropertyDescriptor(document, "fonts");
    const fonts = Object.assign(new EventTarget(), { ready: Promise.resolve() });
    Object.defineProperty(document, "fonts", { configurable: true, value: fonts });
    try {
      render(<MermaidDiagram text="flowchart LR\nA-->B" />);
      await screen.findByText("Pele output");
      expect(renderPele).toHaveBeenCalledTimes(1);
      await act(async () => fonts.dispatchEvent(new Event("loadingdone")));
      await waitFor(() => expect(renderPele).toHaveBeenCalledTimes(2));
    } finally {
      if (original) Object.defineProperty(document, "fonts", original);
      else Reflect.deleteProperty(document, "fonts");
    }
  });
});
