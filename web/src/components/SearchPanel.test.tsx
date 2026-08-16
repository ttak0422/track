import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SearchPanel } from "./SearchPanel";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children, ...rest }: { children?: React.ReactNode }) => <a {...rest}>{children}</a>,
}));
vi.mock("../hooks/useDebouncedValue", () => ({ useDebouncedValue: (value: string) => value }));
const setQuery = vi.hoisted(() => vi.fn());
vi.mock("../searchState", () => ({ useSearchState: () => ({ query: "1785024006000", setQuery }) }));

const results = [
  { note_id: 1, file_kind: "note", path: "", title: "Titled", match: "title", tags: ["daily"] },
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

    // Reading order is the grouping: the named note comes last, not folded into Titles. A NEW badge
    // rides each title (nothing was ever opened in this test's fresh storage), so titles are matched
    // by prefix.
    const shown = [...container.querySelectorAll("a")].map((link) => link.textContent);
    expect(shown).toEqual(["TitledNEW", "BodiedNEW", "NamedNEW"]);
  });

  // The query is shared state, so a search that stayed set kept its hits on screen behind the host
  // that just closed — reopening search showed the previous search instead of an empty field.
  it("clears the query once a result is chosen", () => {
    setQuery.mockReset();
    const onNavigate = vi.fn();
    const { container } = render(<SearchPanel onNavigate={onNavigate} />);

    fireEvent.click(container.querySelector("a")!);
    expect(setQuery).toHaveBeenCalledWith("");
    expect(onNavigate).toHaveBeenCalled();
  });

  // #tag is a tag filter the engine already understands; the panel just had no way to reach it
  // without typing the sigil by hand.
  it("adds a tag term to the query when a result's tag is clicked", () => {
    setQuery.mockReset();
    render(<SearchPanel />);

    fireEvent.click(screen.getByRole("button", { name: "#daily" }));
    expect(setQuery).toHaveBeenCalledWith("1785024006000 #daily");
  });

  it("clears the query when Enter takes the active result", () => {
    setQuery.mockReset();
    render(<SearchPanel />);

    fireEvent.keyDown(screen.getByRole("searchbox"), { key: "Enter" });
    expect(setQuery).toHaveBeenCalledWith("");
  });
});
