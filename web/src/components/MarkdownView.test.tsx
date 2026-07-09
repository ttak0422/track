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
