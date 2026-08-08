import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { HierarchyMenu } from "./HierarchyMenu";
import type { HierarchyResponse } from "../types";

const hierarchy = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, params, ...rest }: { children: unknown; params: { noteId: string } }) => (
    <a href={`/notes/${params.noteId}`} {...rest}>
      {children as never}
    </a>
  ),
}));
vi.mock("../queries", () => ({ useHierarchyQuery: (enabled: boolean) => hierarchy(enabled) }));

const tree: HierarchyResponse = {
  hierarchy: [
    {
      note_id: "1",
      file_kind: "note",
      title: "Root",
      children: [{ note_id: "2", file_kind: "note", title: "Child", children: [{ note_id: "3", file_kind: "note", title: "Grandchild" }] }],
    },
  ],
};

function openMenu() {
  fireEvent.pointerEnter(screen.getByRole("button", { name: "Hierarchy" }));
}

describe("HierarchyMenu", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("asks for the tree only once the menu is opened", () => {
    hierarchy.mockReturnValue({ data: undefined });
    render(<HierarchyMenu />);
    expect(hierarchy).toHaveBeenCalledWith(false);

    hierarchy.mockReturnValue({ data: tree });
    fireEvent.pointerEnter(screen.getByRole("button", { name: "Hierarchy" }));
    expect(hierarchy).toHaveBeenLastCalledWith(true);
  });

  it("draws the whole tree, every level linking to its note", () => {
    hierarchy.mockReturnValue({ data: tree });
    render(<HierarchyMenu />);
    fireEvent.pointerEnter(screen.getByRole("button", { name: "Hierarchy" }));

    expect(screen.getByRole("menu", { name: "Hierarchy" })).toBeInTheDocument();
    for (const [title, id] of [
      ["Root", "1"],
      ["Child", "2"],
      ["Grandchild", "3"],
    ]) {
      expect(screen.getByRole("menuitem", { name: title })).toHaveAttribute("href", `/notes/${id}`);
    }
  });

  it("says so rather than opening an empty panel when nothing is placed by a hierarchy", () => {
    hierarchy.mockReturnValue({ data: { hierarchy: [] } });
    render(<HierarchyMenu />);
    openMenu();

    expect(screen.getByText("No hierarchy")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem")).not.toBeInTheDocument();
  });

  it("folds a branch away and remembers the fold across a reload", () => {
    hierarchy.mockReturnValue({ data: tree });
    const first = render(<HierarchyMenu />);
    openMenu();

    fireEvent.click(screen.getByRole("menuitem", { name: "Collapse Child" }));
    // Only the folded branch goes: its parent's row stays, and so does the row that folded it.
    expect(screen.queryByRole("menuitem", { name: "Grandchild" })).not.toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Child" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Expand Child" })).toHaveAttribute("aria-expanded", "false");

    first.unmount();
    render(<HierarchyMenu />);
    openMenu();
    expect(screen.queryByRole("menuitem", { name: "Grandchild" })).not.toBeInTheDocument();

    // And unfolding is remembered the same way, rather than only the folds being sticky.
    fireEvent.click(screen.getByRole("menuitem", { name: "Expand Child" }));
    expect(screen.getByRole("menuitem", { name: "Grandchild" })).toBeInTheDocument();
    expect(localStorage.getItem("track.hierarchyCollapsed")).toBe("[]");
  });

  it("gives a leaf no fold control", () => {
    hierarchy.mockReturnValue({ data: tree });
    render(<HierarchyMenu />);
    openMenu();

    expect(screen.queryByRole("menuitem", { name: /Grandchild$/ })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Collapse Grandchild" })).not.toBeInTheDocument();
  });
});
