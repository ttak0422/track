import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MermaidDiagram } from "./MermaidDiagram";
import { peleFallbackReason } from "./PeleDiagram";
import { renderPele } from "./peleEngine";

vi.mock("mermaid", () => ({ default: {
  initialize: vi.fn(), registerIconPacks: vi.fn(),
  render: vi.fn(async () => ({ svg: '<svg viewBox="0 0 200 80"><text>Mermaid output</text></svg>' })),
} }));
vi.mock("./peleEngine", () => ({ renderPele: vi.fn(async () => ({
  svg: '<svg class="pele" viewBox="0 0 200 80"><text>Pele output</text></svg>',
  popupSvg: '<svg class="pele" viewBox="0 0 200 80"><text>Pele popup</text></svg>',
})) }));

afterEach(() => {
  vi.clearAllMocks();
  document.documentElement.removeAttribute("data-theme");
  document.documentElement.removeAttribute("style");
});

describe("Pele comparison", () => {
  it("keeps Mermaid as default and copies identical source after switching both ways", async () => {
    const source = 'flowchart LR\nA["日本語のラベル"] --> B[Done]\n';
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    render(<MermaidDiagram text={source} />);
    expect(await screen.findByText("Mermaid output")).toBeInTheDocument();
    expect(renderPele).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Pele (comparison)" }));
    expect(await screen.findByText("Pele output")).toBeInTheDocument();
    expect(renderPele).toHaveBeenCalledWith(source, expect.any(String), expect.any(String));
    fireEvent.click(screen.getByRole("button", { name: "Copy source" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(source));
    fireEvent.click(screen.getByRole("button", { name: "Mermaid" }));
    expect(await screen.findByText("Mermaid output")).toBeInTheDocument();
  });

  it.each([
    ['flowchart LR\nA["$$x^2$$"]', "Math labels"],
    ['architecture-beta\nservice api(logos:aws-lambda)[API]', "Custom icons"],
    ['flowchart LR\nA["<b>HTML</b>"]', "HTML labels"],
    ['---\nconfig:\n  theme: dark\n---\nflowchart LR\nA-->B', "frontmatter"],
    ['%%{init: {"theme":"dark"}}%%\nflowchart LR\nA-->B', "directives"],
  ])("explicitly retains the existing renderer for %s", async (source, reason) => {
    render(<MermaidDiagram text={source} />);
    fireEvent.click(screen.getByRole("button", { name: "Pele (comparison)" }));
    expect(screen.getByRole("status")).toHaveTextContent(reason);
    expect(await screen.findByText("Mermaid output")).toBeInTheDocument();
    expect(renderPele).not.toHaveBeenCalled();
    expect(peleFallbackReason(source)).not.toBeNull();
  });

  it("falls back visibly on failure and retries with changed source", async () => {
    vi.mocked(renderPele).mockRejectedValueOnce(new Error("Unsupported layout"));
    const { rerender } = render(<MermaidDiagram text="flowchart LR\nA-->B" />);
    fireEvent.click(screen.getByRole("button", { name: "Pele (comparison)" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Unsupported layout");
    expect(await screen.findByText("Mermaid output")).toBeInTheDocument();
    rerender(<MermaidDiagram text="flowchart LR\nA-->C" />);
    expect(await screen.findByText("Pele output")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("keeps the same SVG on color changes and measures again when the font changes", async () => {
    const { container } = render(<MermaidDiagram text="flowchart LR\nA-->B" />);
    fireEvent.click(screen.getByRole("button", { name: "Pele (comparison)" }));
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
    fireEvent.click(screen.getByRole("button", { name: "Pele (comparison)" }));
    await screen.findByText("Pele output");
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(container.querySelector(".mermaid-pan")?.getAttribute("style")).toContain("scale(1.3)");
    fireEvent.click(screen.getByRole("button", { name: "Reset diagram view" }));
    expect(container.querySelector(".mermaid-pan")?.getAttribute("style")).toContain("scale(1)");
    fireEvent.click(screen.getByRole("button", { name: "Open diagram in popup" }));
    expect(await screen.findByText("Pele popup")).toBeInTheDocument();
  });
});
