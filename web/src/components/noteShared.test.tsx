import { fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { NoteAside, NoteProperties } from "./noteShared";

const navigate = vi.hoisted(() => vi.fn());
const localGraph = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  useLocation: () => "",
  Link: ({ children }: { children?: ReactNode }) => <a>{children}</a>,
}));

vi.mock("../queries", () => ({
  useAgendaQuery: () => ({ isPending: false, data: { notes: [] } }),
  useLocalGraphQuery: (noteID: unknown) => localGraph(noteID),
}));

vi.mock("./preview/WikiLink", () => ({ WikiLink: () => null }));

// Stub the canvas (a lazy, client-only component) so the test can observe resetToken and drive
// onSelect directly.
vi.mock("./GraphCanvasLazy", () => ({
  GraphCanvas: ({ onSelect, resetToken }: { onSelect: (id: string) => void; resetToken: number }) => (
    <button type="button" data-reset={resetToken} onClick={() => onSelect("2")}>
      select-2
    </button>
  ),
}));

const linkedGraph = {
  graph: {
    center_id: "1",
    nodes: [
      { note_id: "1", file_kind: "note", title: "Center", center: true },
      { note_id: "2", file_kind: "note", title: "Neighbor" },
    ],
    edges: [{ source_id: "1", target_id: "2" }],
  },
};

describe("NoteAside Contents outline", () => {
  it("lists the leading h1 and links each entry to its rendered heading id", () => {
    localGraph.mockReturnValue({ data: undefined });
    render(
      <NoteAside backlinks={[]} noteID="1" journalDate="" markdown={"# Title\n\n## Status"} />,
    );
    const contents = screen.getByRole("region", { name: "Contents" });
    const links = within(contents).getAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual(["Title", "Status"]);
    expect(links[0]).toHaveAttribute("href", "#h-title");
    expect(links[1]).toHaveAttribute("href", "#h-status");
  });

  it("omits the Contents section while a body has a single heading", () => {
    localGraph.mockReturnValue({ data: undefined });
    render(<NoteAside backlinks={[]} noteID="1" journalDate="" markdown={"# Only"} />);
    expect(screen.queryByRole("region", { name: "Contents" })).toBeNull();
  });
});

describe("NoteAside graph section", () => {
  it("shows the always-on local graph, resets its view, and navigates on node select", () => {
    localGraph.mockReturnValue({ data: linkedGraph });
    render(<NoteAside backlinks={[]} noteID="1" journalDate="" />);

    // Labelled like the lists above it, so the aside reads as one stack of sections.
    const region = screen.getByRole("region", { name: "Graph" });
    const heading = within(region).getByRole("heading", { name: "Graph" });
    expect(heading.parentElement).toHaveClass("aside-graph-heading");

    const canvas = screen.getByText("select-2");
    expect(canvas.getAttribute("data-reset")).toBe("0");
    fireEvent.click(screen.getByRole("button", { name: "Reset graph view" }));
    expect(canvas.getAttribute("data-reset")).toBe("1");

    fireEvent.click(canvas);
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "2" } });
  });

  it("uses equally sized vector icons for the graph controls", () => {
    localGraph.mockReturnValue({ data: linkedGraph });
    render(<NoteAside backlinks={[]} noteID="1" journalDate="" />);

    const region = screen.getByRole("region", { name: "Graph" });
    const reset = within(region).getByRole("button", { name: "Reset graph view" });
    const enlarge = within(region).getByRole("button", { name: "Enlarge graph" });
    const resetIcon = reset.querySelector("svg");
    const enlargeIcon = enlarge.querySelector("svg");

    expect(resetIcon).not.toBeNull();
    expect(enlargeIcon).not.toBeNull();
    expect(resetIcon).toHaveAttribute("width", "15");
    expect(resetIcon).toHaveAttribute("height", "15");
    expect(enlargeIcon).toHaveAttribute("width", "15");
    expect(enlargeIcon).toHaveAttribute("height", "15");
  });

  it("opens the enlarged graph in a centered dialog and closes on backdrop click", () => {
    localGraph.mockReturnValue({ data: linkedGraph });
    const { container } = render(<NoteAside backlinks={[]} noteID="1" journalDate="" />);

    fireEvent.click(screen.getByRole("button", { name: "Enlarge graph" }));
    const dialog = container.querySelector("dialog.graph-lightbox");
    expect(dialog).not.toBeNull();
    // The lightbox carries its own canvas, independent of the aside's.
    expect(within(dialog! as HTMLElement).getByText("select-2")).toBeTruthy();

    // A backdrop click lands on the dialog element itself and closes (unmounts) the lightbox.
    fireEvent.click(dialog!);
    expect(container.querySelector("dialog.graph-lightbox")).toBeNull();
  });

  // The lightbox fills the window, so what is left to click past it is a few pixels of backdrop —
  // nothing a thumb can aim at, and there is no Esc key on a phone either.
  it("closes the enlarged graph from a button of its own", () => {
    localGraph.mockReturnValue({ data: linkedGraph });
    const { container } = render(<NoteAside backlinks={[]} noteID="1" journalDate="" />);

    fireEvent.click(screen.getByRole("button", { name: "Enlarge graph" }));
    const dialog = container.querySelector("dialog.graph-lightbox")!;
    fireEvent.click(within(dialog as HTMLElement).getByRole("button", { name: "Close enlarged graph" }));

    expect(container.querySelector("dialog.graph-lightbox")).toBeNull();
  });

  it("navigates from a node selected in the enlarged graph and drops the dialog", () => {
    localGraph.mockReturnValue({ data: linkedGraph });
    const { container } = render(<NoteAside backlinks={[]} noteID="1" journalDate="" />);

    fireEvent.click(screen.getByRole("button", { name: "Enlarge graph" }));
    const dialog = container.querySelector("dialog.graph-lightbox")!;
    fireEvent.click(within(dialog as HTMLElement).getByText("select-2"));
    expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "2" } });
    expect(container.querySelector("dialog.graph-lightbox")).toBeNull();
  });

  it("omits the graph while it is loading or when the note links nowhere", () => {
    localGraph.mockReturnValue({
      data: {
        graph: {
          center_id: "1",
          nodes: [{ note_id: "1", file_kind: "note", title: "Center", center: true }],
          edges: [],
        },
      },
    });
    render(<NoteAside backlinks={[]} noteID="1" journalDate="" />);
    expect(screen.queryByRole("region", { name: "Graph" })).toBeNull();
  });
});

describe("NoteProperties dates", () => {
  const props = [{ key: "status", value: "draft", type: "string", line: 0 }];

  it("closes the strip with the created and updated rows", () => {
    // Built from a local date so the day the mtime formats to is the same in any timezone.
    const updated = new Date(2026, 5, 20, 12, 0, 0).getTime() / 1000;
    render(<NoteProperties props={props} created="2026-06-14" updated={updated} />);

    expect(screen.getAllByRole("term").map((dt) => dt.textContent)).toEqual([
      "status",
      "created",
      "updated",
    ]);
    // created shows verbatim; updated is the mtime at the same day precision.
    expect(screen.getByText("2026-06-14")).toBeTruthy();
    expect(screen.getByText("2026-06-20")).toBeTruthy();
  });

  it("omits a date row the note has no value for", () => {
    render(<NoteProperties props={props} created="2026-06-14" />);
    expect(screen.getAllByRole("term").map((dt) => dt.textContent)).toEqual(["status", "created"]);
  });

  // The common note has no properties of its own but does have a created date, so the strip has to
  // open for the dates alone — it is only empty when there is nothing at all to show.
  it("shows the dates on a note with no properties, and nothing at all without either", () => {
    const { unmount } = render(<NoteProperties props={[]} created="2026-06-14" />);
    expect(screen.getAllByRole("term").map((dt) => dt.textContent)).toEqual(["created"]);
    unmount();

    render(<NoteProperties props={[]} />);
    expect(screen.queryByRole("list")).toBeNull();
    expect(screen.queryAllByRole("term")).toEqual([]);
  });
});
