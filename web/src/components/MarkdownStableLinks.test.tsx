import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { MarkdownView } from "./MarkdownView";
import { FloatingProvider } from "./preview/floatingStore";

vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
  useNavigate: () => vi.fn(),
  Link: ({ children, params, hash }: { children: ReactNode; params: { noteId: string }; hash?: string }) => (
    <a href={`/notes/${params.noteId}${hash ? `#${hash}` : ""}`}>{children}</a>
  ),
}));

vi.mock("../queries", () => ({
  useResolveQuery: (term: string) => ({
    data: { found: term === "id:200", note: { note_id: "200", title: "Renamed title" } },
    isPending: false,
  }),
}));

describe("stable Markdown links", () => {
  it("renders an ID-only GFM table cell with the current title and preserves explicit aliases", () => {
    const { container } = render(
      <FloatingProvider>
        <MarkdownView markdown={"| title |\n| --- |\n| [[id:200]] |\n\n[[id:200#Section|Old title]] [[id:200|id:200]] [[id:404|Missing]]"} />
      </FloatingProvider>,
    );
    expect(container.querySelectorAll("tbody td")).toHaveLength(1);
    expect(container.querySelector("tbody td")?.textContent).toBe("Renamed title");
    expect([...container.querySelectorAll("a")].map((link) => [link.textContent, link.getAttribute("href")])).toEqual([
      ["Renamed title", "/notes/200"],
      ["Old title", "/notes/200#h-section"],
      ["id:200", "/notes/200"],
    ]);
    expect(container.querySelector(".wiki-link.unresolved")?.textContent).toBe("Missing");
  });
});
