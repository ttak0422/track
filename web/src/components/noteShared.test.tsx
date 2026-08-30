import { fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { NoteAside, NoteFlagBadges, NoteProperties, NoteStamps } from "./noteShared";

const navigate = vi.hoisted(() => vi.fn());
const localGraph = vi.hoisted(() => vi.fn());
// A default empty agenda is set in beforeEach; tests for a journal's On-this-day list override it.
const agenda = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  useLocation: () => "",
  Link: ({ children }: { children?: ReactNode }) => <a>{children}</a>,
}));

vi.mock("../queries", () => ({
  useAgendaQuery: (date: unknown, vault: unknown) => agenda(date, vault),
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

beforeEach(() => {
  // Every list test starts from an empty agenda and no graph; a test overrides either for its own
  // note.
  agenda.mockReturnValue({ isPending: false, data: { notes: [] } });
  localGraph.mockReset();
  localGraph.mockReturnValue({ data: undefined });
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

describe("note flags", () => {
  it("stamps each flag over the article and stacks two flags vertically", () => {
    const { container } = render(<NoteStamps flags={["DEPRECATED", "CONFIDENTIAL"]} />);

    const stamps = container.querySelectorAll(".stamp");
    expect(stamps).toHaveLength(2);
    expect(stamps[0]).toHaveClass("stamp-deprecated");
    expect(stamps[0]).toHaveTextContent("DEPRECATED");
    expect(stamps[1]).toHaveClass("stamp-confidential");
    expect(stamps[1]).toHaveTextContent("CONFIDENTIAL");
    // Each stamp carries its own top, so the second lands below the first.
    const tops = [...stamps].map((stamp) =>
      Number((stamp as HTMLElement).style.top.replace("px", "")),
    );
    expect(tops[1]).toBeGreaterThan(tops[0]);
  });

  it("stamps nothing when the note carries no flags", () => {
    const { container } = render(<NoteStamps flags={undefined} />);
    expect(container.querySelector(".note-stamps")).toBeNull();
  });

  it("badges a flagged note beside its title", () => {
    const { container } = render(<NoteFlagBadges flags={["CONFIDENTIAL", "DEPRECATED"]} />);

    const badges = container.querySelectorAll(".note-flag-badge");
    expect(badges).toHaveLength(2);
    expect(badges[0]).toHaveClass("note-flag-badge-confidential");
    expect(badges[0]).toHaveTextContent("CONFIDENTIAL");
    expect(badges[1]).toHaveClass("note-flag-badge-deprecated");
  });

  it("badges a flagged backlink in the aside list", () => {
    render(
      <NoteAside
        backlinks={[{ note_id: "2", file_kind: "note", title: "Flagged", flags: ["DEPRECATED"] }]}
        noteID="1"
        journalDate=""
      />,
    );

    const badge = screen.getByText("DEPRECATED");
    expect(badge).toHaveClass("note-flag-badge-deprecated");
  });

  it("badges a flagged note in the On-this-day list", () => {
    agenda.mockReturnValue({
      isPending: false,
      data: {
        notes: [{ note_id: "3", file_kind: "note", title: "That day", flags: ["CONFIDENTIAL"] }],
      },
    });
    render(<NoteAside backlinks={[]} noteID="1" journalDate="2026-07-12" />);

    expect(screen.getByText("That day")).toBeTruthy();
    expect(screen.getByText("CONFIDENTIAL")).toHaveClass("note-flag-badge-confidential");
  });
});
