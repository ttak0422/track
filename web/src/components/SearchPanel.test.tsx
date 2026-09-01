import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FloatingProvider, useFloating } from "./preview/floatingStore";
import { SearchPanel } from "./SearchPanel";

// Where the reader is. The panel asks so a result already on screen offers no way to open it again;
// the floating layer asks so it can drop its unpinned windows when the route changes.
const routerMock = vi.hoisted(() => ({ pathname: "/" }));

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  useRouterState: () => routerMock.pathname,
  Link: ({ children, ...rest }: { children?: React.ReactNode }) => <a {...rest}>{children}</a>,
}));
vi.mock("../hooks/useDebouncedValue", () => ({ useDebouncedValue: (value: string) => value }));
const setQuery = vi.hoisted(() => vi.fn());
vi.mock("../searchState", () => ({ useSearchState: () => ({ query: "1785024006000", setQuery }) }));
// The panel's result rows share the aside's flag badges, which would drag the graph canvas and the
// wiki-link preview windows (and through them d3-force and pdf.js) into this test; neither is under
// test here.
vi.mock("./GraphCanvasLazy", () => ({ GraphCanvas: () => null }));
vi.mock("./preview/WikiLink", () => ({ WikiLink: () => null }));

// A mutable holder so a test can swap in a flagged result and restore the default afterwards. The
// defaults live here (not in a module const) because vi.mock's factory is hoisted above it.
type MockResult = {
  note_id: string;
  file_kind: string;
  path: string;
  title: string;
  match: string;
  tags?: string[];
  flags?: string[];
};

const searchState = vi.hoisted(() => ({
  results: [
    { note_id: "1", file_kind: "note", path: "", title: "Titled", match: "title", tags: ["daily"] },
    { note_id: "2", file_kind: "note", path: "", title: "Bodied", match: "body" },
    { note_id: "3", file_kind: "note", path: "", title: "Named", match: "path" },
  ] as MockResult[],
}));

vi.mock("../queries", () => ({
  useSearchQuery: () => ({
    data: { results: searchState.results, unavailable: [] },
    isPending: false,
    isError: false,
  }),
}));

// Every result row carries a float button, which asks the layer for a window — so the panel only
// renders inside the provider that owns them.
function renderPanel(panel: React.ReactElement) {
  return render(
    <FloatingProvider>
      {panel}
      <FloatingCount />
    </FloatingProvider>,
  );
}

// Reports how many windows the floating layer holds, without rendering the layer itself.
function FloatingCount() {
  const { windows } = useFloating();
  return <output data-testid="floating-count">{windows.length}</output>;
}

describe("SearchPanel groups", () => {
  afterEach(() => {
    routerMock.pathname = "/";
  });

  // A file-name hit used to fall into Titles, because that group was everything that was not a body
  // hit. It is its own group, last: naming a file is the coarsest way to ask for a note.
  it("puts a file-name hit in its own group, below full text", () => {
    const { container } = renderPanel(<SearchPanel />);

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
    const { container } = renderPanel(<SearchPanel onNavigate={onNavigate} />);

    fireEvent.click(container.querySelector("a")!);
    expect(setQuery).toHaveBeenCalledWith("");
    expect(onNavigate).toHaveBeenCalled();
  });

  // #tag is a tag filter the engine already understands; the panel just had no way to reach it
  // without typing the sigil by hand.
  it("adds a tag term to the query when a result's tag is clicked", () => {
    setQuery.mockReset();
    renderPanel(<SearchPanel />);

    fireEvent.click(screen.getByRole("button", { name: "#daily" }));
    expect(setQuery).toHaveBeenCalledWith("1785024006000 #daily");
  });

  it("clears the query when Enter takes the active result", () => {
    setQuery.mockReset();
    renderPanel(<SearchPanel />);

    fireEvent.keyDown(screen.getByRole("searchbox"), { key: "Enter" });
    expect(setQuery).toHaveBeenCalledWith("");
  });

  // The row's own control, since a button cannot live inside the result's link: it pops the note into
  // the floating layer rather than navigating, so a hit can be read without spending the search.
  it("floats a result from the button beside it", () => {
    renderPanel(<SearchPanel />);

    expect(screen.getByTestId("floating-count")).toHaveTextContent("0");
    fireEvent.click(screen.getByRole("button", { name: "Float Titled" }));
    expect(screen.getByTestId("floating-count")).toHaveTextContent("1");
    // The note is on screen now, so the row stops offering to put it there: clicking again would only
    // raise the window already standing in front of the reader.
    expect(screen.queryByRole("button", { name: "Float Titled" })).toBeNull();
  });

  // The note in the reader is displayed already — the loudest case of the same rule.
  it("offers no float control for the note being read", () => {
    routerMock.pathname = "/notes/1";
    renderPanel(<SearchPanel />);

    expect(screen.queryByRole("button", { name: "Float Titled" })).toBeNull();
    expect(screen.getByRole("button", { name: "Float Bodied" })).toBeTruthy();
  });

  it("badges a flagged result beside its title", () => {
    searchState.results = [
      { note_id: "9", file_kind: "note", path: "", title: "Flagged", match: "title", flags: ["DEPRECATED"] },
    ];
    const { container } = renderPanel(<SearchPanel />);

    expect(screen.getByText("Flagged")).toBeTruthy();
    const badge = container.querySelector(".note-flag-badge-deprecated");
    expect(badge).not.toBeNull();
    expect(badge).toHaveTextContent("DEPRECATED");
  });
});
