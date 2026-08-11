import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SearchPanel } from "./SearchPanel";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children, ...rest }: { children?: React.ReactNode }) => <a {...rest}>{children}</a>,
}));
vi.mock("../hooks/useDebouncedValue", () => ({ useDebouncedValue: (value: string) => value }));
vi.mock("../searchState", () => ({ useSearchState: () => ({ query: "1785024006000", setQuery: vi.fn() }) }));

const results = [
  { note_id: 1, file_kind: "note", path: "", title: "Titled", match: "title" },
  { note_id: 2, file_kind: "note", path: "", title: "Bodied", match: "body" },
  { note_id: 3, file_kind: "note", path: "", title: "Named", match: "path" },
];

vi.mock("../queries", () => ({
  useSearchQuery: () => ({ data: { results, unavailable: [] }, isPending: false, isError: false }),
}));

describe("SearchPanel groups", () => {
  // A file-name hit used to fall into Titles, because that group was everything that was not a body
  // hit. It is its own group, last: naming a file is the coarsest way to ask for a note.
  it("puts a file-name hit in its own group, below full text", () => {
    const { container } = render(<SearchPanel />);

    const headings = screen.getAllByRole("heading").map((h) => h.textContent);
    expect(headings).toEqual(["Titles", "Full text", "File name"]);

    // Reading order is the grouping: the named note comes last, not folded into Titles.
    const shown = [...container.querySelectorAll("a")].map((link) => link.textContent);
    expect(shown).toEqual(["Titled", "Bodied", "Named"]);
  });
});
