import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { MarkdownView } from "../MarkdownView";
import { FloatingProvider } from "../preview/floatingStore";

// WikiLink (card titles) reads the router; stub it the same way MarkdownView.test.tsx does.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
  useNavigate: () => vi.fn(),
}));

// Card titles are WikiLinks, which need react-query (resolution) and the floating-window store
// (hover previews), the same providers WikiLink.test.tsx uses.
function renderWithQuery(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <FloatingProvider>{ui}</FloatingProvider>
    </QueryClientProvider>,
  );
}

function fence(payload: unknown): string {
  return "```track-view\n" + JSON.stringify(payload) + "\n```";
}

describe("QueryView", () => {
  it("renders a list payload without table chrome", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={fence({
          layout: "list",
          columns: ["title"],
          groups: [{ rows: [{ title: "Alpha", cells: ["Alpha"] }, { title: "Beta", cells: ["Beta"] }] }],
        })}
      />
    );
    const list = container.querySelector(".query-list");
    expect(list).not.toBeNull();
    expect(list?.querySelectorAll(".query-list-item")).toHaveLength(2);
    expect(list?.textContent).toContain("Alpha");
    expect(container.querySelector("table")).toBeNull();
  });

  it("renders a board payload as lanes of cards", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={fence({
          layout: "board",
          key: "state",
          columns: ["title", "state", "due"],
          groups: [
            { name: "doing", rows: [{ title: "Alpha", cells: ["Alpha", "doing", "2026-07-02"] }] },
            { name: "todo", rows: [{ title: "Beta", cells: ["Beta", "todo", ""] }] },
          ],
        })}
      />,
    );
    const lanes = container.querySelectorAll(".query-lane");
    expect(lanes).toHaveLength(2);
    expect(lanes[0].querySelector(".query-lane-name")?.textContent).toBe("doing");
    expect(lanes[0].querySelector(".query-lane-count")?.textContent).toBe("1");
    const card = lanes[0].querySelector(".query-card");
    expect(card?.textContent).toContain("Alpha");
    // The due cell shows as a meta line; the grouping column does not repeat on the card.
    expect(card?.textContent).toContain("due");
    expect(card?.textContent).toContain("2026-07-02");
    expect(card?.textContent).not.toContain("state");
  });

  it("keeps the card link while hiding a card title", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={fence({
          layout: "board",
          showTitle: false,
          key: "state",
          columns: ["title", "state"],
          groups: [{ name: "todo", rows: [{ title: "Alpha", cells: ["Alpha", "todo"] }] }],
        })}
      />
    );
    const title = container.querySelector(".query-card-title-hidden");
    expect(title).not.toBeNull();
    expect(title?.querySelector(".wiki-link")).not.toBeNull();
  });

  it("renders a gallery payload with covers served as assets", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={fence({
          layout: "gallery",
          columns: ["title"],
          groups: [
            {
              rows: [
                { title: "Alpha", cells: ["Alpha"], cover: "assets/a.png" },
                { title: "Beta", cells: ["Beta"], icon: "📄" },
                { title: "Gamma", cells: ["Gamma"] },
              ],
            },
          ],
        })}
      />,
    );
    const cards = container.querySelectorAll(".query-gallery-card");
    expect(cards).toHaveLength(3);
    const img = cards[0].querySelector("img");
    expect(img?.getAttribute("src")).toContain("/api/asset?");
    expect(img?.getAttribute("src")).toContain("a.png");
    // A coverless note keeps its card, with its icon as the default card face.
    expect(cards[1].querySelector("img")).toBeNull();
    expect(cards[1].querySelector(".query-card-cover-empty")?.textContent).toBe("📄");
    // With neither cover nor icon, track's built-in no-image face fills the slot.
    expect(cards[2].querySelector(".query-card-noimage")).not.toBeNull();
  });

  it("renders a calendar payload as month grids with rows on their days", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={fence({
          layout: "calendar",
          key: "due",
          columns: ["title", "due"],
          groups: [
            { name: "2026-07-01", rows: [{ title: "Beta", cells: ["Beta", "2026-07-01"] }] },
            { name: "2026-08-15", rows: [{ title: "Gamma", cells: ["Gamma", "2026-08-15"] }] },
          ],
        })}
      />,
    );
    const months = container.querySelectorAll(".query-month");
    expect(months).toHaveLength(2);
    expect(months[0].querySelector(".calendar-title")?.textContent).toBe("2026 / 07");
    // July 2026 starts on a Wednesday: 3 leading blanks, 31 day cells.
    expect(months[0].querySelectorAll(".calendar-day-blank")).toHaveLength(3);
    expect(months[0].querySelectorAll(".calendar-day:not(.calendar-day-blank)")).toHaveLength(31);
    const active = months[0].querySelector(".calendar-day-active");
    expect(active?.textContent).toContain("Beta");
    expect(months[1].textContent).toContain("Gamma");
  });

  it("falls back to a code block for a payload it cannot parse", () => {
    const { container } = render(<MarkdownView markdown={"```track-view\nnot json\n```"} />);
    expect(container.querySelector(".query-view")).toBeNull();
    expect(container.querySelector(".code-block")).not.toBeNull();
  });
});
