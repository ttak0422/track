import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { MarkdownView } from "./MarkdownView";
import { FloatingProvider } from "./preview/floatingStore";

// FloatingProvider (needed by the include embed's WikiLink header) reads the current route, so stub
// the router the same way WikiLink.test.tsx does.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
  useNavigate: () => vi.fn(),
}));

// EChartsBlock lazy-imports echarts; stub it so a chart fence doesn't pull the real (heavy) library
// into this suite and starve the KaTeX lazy-load test of its waitFor budget.
vi.mock("echarts", () => ({
  init: vi.fn(() => ({ setOption: vi.fn(), resize: vi.fn(), dispose: vi.fn() })),
  getInstanceByDom: vi.fn(() => undefined),
}));

// GraphvizDiagram lazy-imports the Graphviz WASM engine; stub it for the same reason.
vi.mock("@hpcc-js/wasm-graphviz", () => ({
  Graphviz: { load: async () => ({ dot: () => '<svg viewBox="0 0 10 10"><text>G</text></svg>' }) },
}));

// A QueryClient is only needed for markdown that produces links (ExternalLink/WikiLink) or viewspec
// charts (ViewSpecChart), which call useQuery. Pure block content (tables, task lists, code) renders
// without it.
function renderWithQuery(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe("MarkdownView", () => {
  it("shows a placeholder for empty input", () => {
    render(<MarkdownView markdown="   " />);
    expect(screen.getByText("Empty note.")).toBeInTheDocument();
  });

  it("renders a GFM table", () => {
    render(<MarkdownView markdown={"| a | b |\n| --- | --- |\n| 1 | 2 |"} />);
    const table = screen.getByRole("table");
    expect(within(table).getByText("a")).toBeInTheDocument();
    expect(within(table).getByText("2")).toBeInTheDocument();
  });

  it("renders a GFM task list with checkbox state", () => {
    const { container } = render(<MarkdownView markdown={"- [ ] todo\n- [x] done"} />);
    const items = container.querySelectorAll(".task-list-item");
    expect(items).toHaveLength(2);
    const boxes = container.querySelectorAll<HTMLInputElement>("input[type='checkbox']");
    expect(boxes).toHaveLength(2);
    expect(boxes[0]).not.toBeChecked();
    expect(boxes[1]).toBeChecked();
  });

  it("renders a fenced code block through CodeBlock", () => {
    const { container } = render(<MarkdownView markdown={"```js\nconst x = 1\n```"} />);
    expect(container.querySelector(".code-block")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy code" })).toBeInTheDocument();
  });

  it("renders Mermaid fences through the diagram component", () => {
    const { container } = render(<MarkdownView markdown={"```mermaid\ngraph TD\nA-->B\n```"} />);
    expect(container.querySelector(".mermaid-diagram")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders dot fences through the Graphviz diagram component", async () => {
    const { container } = render(<MarkdownView markdown={"```dot\ndigraph { a -> b }\n```"} />);
    expect(container.querySelector(".graphviz-diagram")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "Graphviz diagram" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders a heading-based mindmap fence as an SVG tree", () => {
    const { container } = render(<MarkdownView markdown={"```mindmap\n# Root\n## Leaf\n```"} />);
    const svg = container.querySelector(".mindmap-diagram svg");
    expect(svg).toBeInTheDocument();
    expect(svg?.textContent).toContain("Root");
    expect(svg?.textContent).toContain("Leaf");
  });

  it("maps the note's heading tree for an empty mindmap fence", () => {
    const markdown = "# Note\n\n## Alpha\n\n## Beta\n\n```mindmap\n```";
    const { container } = render(<MarkdownView markdown={markdown} />);
    const svg = container.querySelector(".mindmap-diagram svg");
    expect(svg?.textContent).toContain("Note");
    expect(svg?.textContent).toContain("Alpha");
    expect(svg?.textContent).toContain("Beta");
  });

  it("renders GFM footnotes with linked reference and back-link", () => {
    const { container } = render(
      <MarkdownView markdown={"A claim.[^1]\n\n[^1]: The source of the claim."} />,
    );
    const ref = container.querySelector("sup a.footnote-ref") as HTMLAnchorElement;
    expect(ref).toBeInTheDocument();
    expect(ref.getAttribute("href")).toBe("#user-content-fn-1");
    expect(ref.id).toBe("user-content-fnref-1");

    const section = container.querySelector("section.footnotes");
    expect(section).toBeInTheDocument();
    expect(section?.textContent).toContain("The source of the claim.");
    const backref = section?.querySelector("a.footnote-backref") as HTMLAnchorElement;
    expect(backref.getAttribute("href")).toBe("#user-content-fnref-1");
    expect(backref).toHaveAttribute("title", "Back to reference 1");
    expect(container.querySelector("#user-content-fn-1")).toBeInTheDocument();
  });

  it("renders one back-link for each reference to the same footnote", () => {
    const { container } = render(
      <MarkdownView markdown={"First claim.[^source] Second claim.[^source]\n\n[^source]: Shared source."} />,
    );

    const refs = container.querySelectorAll("a.footnote-ref");
    const backrefs = container.querySelectorAll("a.footnote-backref");
    expect(refs).toHaveLength(2);
    expect(backrefs).toHaveLength(2);
    for (const backref of backrefs) {
      const target = backref.getAttribute("href")?.slice(1);
      expect(target).toBeTruthy();
      expect(container.querySelector(`#${target}`)).toBeInTheDocument();
    }
  });

  it("renders viewspec fences through the chart component", () => {
    const { container } = renderWithQuery(<MarkdownView markdown={'```viewspec\n{"version":2}\n```'} />);
    expect(container.querySelector(".viewspec-chart")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders echarts fences as a chart container, and bad JSON as a code block", () => {
    const { container } = render(<MarkdownView markdown={'```echarts\n{"series":[]}\n```'} />);
    expect(container.querySelector(".viewspec-chart")).toBeInTheDocument();

    const { container: bad } = render(<MarkdownView markdown={"```echarts\nnot json\n```"} />);
    expect(bad.querySelector(".viewspec-chart")).not.toBeInTheDocument();
    expect(bad.querySelector(".code-block")).toBeInTheDocument();
  });

  it("renders an external link that opens in a new tab", () => {
    renderWithQuery(<MarkdownView markdown="[example](https://example.com)" />);
    const link = screen.getByRole("link", { name: "example" });
    expect(link).toHaveAttribute("href", "https://example.com");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("renders inline and block math with KaTeX (loaded lazily)", async () => {
    const { container } = render(<MarkdownView markdown={"inline $a^2+b^2$\n\n$$\n\\int_0^1 x\\,dx\n$$"} />);
    // KaTeX is imported on demand, so the .katex spans appear once the chunk resolves. A block
    // ($$…$$) is wrapped in .katex-display. Resolving the chunk competes with every other test file
    // under a parallel run, so give it well past waitFor's default 1s — this was a recurring flake.
    await waitFor(() => expect(container.querySelectorAll(".katex").length).toBeGreaterThanOrEqual(2), {
      timeout: 10_000,
    });
    expect(container.querySelector(".katex-display")).toBeInTheDocument();
  });

  it("renders a [!NOTE] blockquote as a titled callout and leaves plain quotes alone", () => {
    const { container } = render(<MarkdownView markdown={"> [!NOTE]\n> body text"} />);
    const alert = container.querySelector(".md-alert.md-alert-note");
    expect(alert).not.toBeNull();
    expect(within(alert as HTMLElement).getByText("Note")).toBeInTheDocument();
    expect(alert?.textContent).toContain("body text");
    // The marker itself is stripped from the body.
    expect(container.textContent).not.toContain("[!NOTE]");

    const { container: quote } = render(<MarkdownView markdown={"> just a quote"} />);
    expect(quote.querySelector(".md-alert")).toBeNull();
    expect(quote.querySelector("blockquote")).not.toBeNull();
  });

  it("applies a :height embed option to an HTML embed and strips the option tail", () => {
    const { container } = render(<MarkdownView markdown={"![Widget](assets/x.html) :height 240"} />);
    const frame = container.querySelector(".embed-html iframe") as HTMLIFrameElement | null;
    expect(frame).not.toBeNull();
    expect(frame?.style.height).toBe("240px");
    // The option tail is consumed, not rendered as text.
    expect(container.textContent).not.toContain(":height");

    // A percentage is treated as viewport height (vh), since a normal-flow iframe has no % basis.
    const { container: pct } = render(<MarkdownView markdown={"![Widget](assets/x.html) :height 90%"} />);
    expect((pct.querySelector(".embed-html iframe") as HTMLIFrameElement).style.height).toBe("90vh");
  });

  it("renders a resolved include as an embed card in place of its directive line", () => {
    // The embed header's WikiLink needs the floating-window store (same as WikiLink.test.tsx).
    const { container } = renderWithQuery(
      <FloatingProvider>
        <MarkdownView
          markdown={"before\n\n![[Design##API]] :only-contents\n\nafter"}
          includes={[{ line: 2, title: "Design", caption: "Design##API", lines: ["embedded line"] }]}
        />
      </FloatingProvider>,
    );
    const card = container.querySelector(".note-include");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText("embedded line")).toBeInTheDocument();
    // The caption header links back to the source note; the raw directive text is gone.
    expect(within(card as HTMLElement).getByText("Design##API")).toBeInTheDocument();
    expect(container.textContent).not.toContain(":only-contents");
  });

  it("renders an include error as a warning card instead of dropping the line", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={"![[Nope]]"}
        includes={[{ line: 0, caption: "Nope", lines: [], error: 'unresolved note "Nope"' }]}
      />,
    );
    expect(container.querySelector(".note-include-error")?.textContent).toContain(
      'unresolved note "Nope"',
    );
  });
});
