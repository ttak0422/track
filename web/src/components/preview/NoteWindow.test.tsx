import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { NoteWindow } from "./NoteWindow";

// A note and its sanitized render arrive in two steps. FloatingWindow auto-fits its height to whatever
// is inside it, so drawing an empty body between the two makes the window shrink and jump back — these
// tests pin the switch that keeps the spinner up until there is real content to fit to.

const { noteQuery, renderQuery } = vi.hoisted(() => ({ noteQuery: vi.fn(), renderQuery: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("../../queries", () => ({
  useNoteQuery: () => noteQuery(),
  useRenderQuery: () => renderQuery(),
}));
// Only the loading/body switch is under test, so the window chrome and the Markdown renderer are stubs.
vi.mock("./FloatingWindow", () => ({
  FloatingWindow: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("../MarkdownView", () => ({
  MarkdownView: ({ markdown, title, showTitle }: { markdown: string; title?: string; showTitle?: boolean }) => (
    <div data-testid="body" data-title={title} data-show-title={String(showTitle)}>
      {markdown}
    </div>
  ),
}));

const controls = {
  initialBounds: { left: 0, top: 0, width: 320, height: 240 },
  pinned: false,
  depth: 0,
  stackOrder: 0,
  onActivate: () => {},
  onClose: () => {},
  onPinToggle: () => {},
};

function noteLoaded(body: string) {
  return { data: { note: { title: "T", body, file_kind: "note" } }, isPending: false, isError: false };
}

function show() {
  render(<NoteWindow noteID="1" {...controls} />);
}

describe("NoteWindow content switch", () => {
  it("holds the spinner while the render is still in flight", () => {
    noteQuery.mockReturnValue(noteLoaded("# hi"));
    renderQuery.mockReturnValue({ data: undefined, isError: false });
    show();
    expect(screen.getByRole("status")).toBeTruthy();
    expect(screen.queryByTestId("body")).toBeNull();
  });

  it("shows the body once the render lands", () => {
    noteQuery.mockReturnValue(noteLoaded("# hi"));
    renderQuery.mockReturnValue({ data: { markdown: "# hi" }, isError: false });
    show();
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByTestId("body").textContent).toBe("# hi");
  });

  it("passes the note title for duplicate removal without rendering it in the popup body", () => {
    noteQuery.mockReturnValue(noteLoaded("# T"));
    renderQuery.mockReturnValue({ data: { markdown: "# T" }, isError: false });
    show();
    expect(screen.getByTestId("body")).toHaveAttribute("data-title", "T");
    expect(screen.getByTestId("body")).toHaveAttribute("data-show-title", "false");
  });

  it("does not wait on an empty note, whose render never runs", () => {
    noteQuery.mockReturnValue(noteLoaded("   "));
    renderQuery.mockReturnValue({ data: undefined, isError: false });
    show();
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByTestId("body")).toBeTruthy();
  });

  it("falls through to the empty body when the render fails", () => {
    noteQuery.mockReturnValue(noteLoaded("# hi"));
    renderQuery.mockReturnValue({ data: undefined, isError: true });
    show();
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByTestId("body")).toBeTruthy();
  });
});
