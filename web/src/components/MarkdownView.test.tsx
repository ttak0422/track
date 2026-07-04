import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { MarkdownView } from "./MarkdownView";

// A QueryClient is only needed for markdown that produces links (ExternalLink/WikiLink call useQuery).
// Pure block content (tables, task lists, code) renders without it.
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

  it("renders an external link that opens in a new tab", () => {
    renderWithQuery(<MarkdownView markdown="[example](https://example.com)" />);
    const link = screen.getByRole("link", { name: "example" });
    expect(link).toHaveAttribute("href", "https://example.com");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("renders inline and block math with KaTeX", () => {
    const { container } = render(<MarkdownView markdown={"inline $a^2+b^2$\n\n$$\n\\int_0^1 x\\,dx\n$$"} />);
    // KaTeX emits .katex spans; a block ($$…$$) is wrapped in .katex-display.
    expect(container.querySelectorAll(".katex").length).toBeGreaterThanOrEqual(2);
    expect(container.querySelector(".katex-display")).toBeInTheDocument();
  });
});
