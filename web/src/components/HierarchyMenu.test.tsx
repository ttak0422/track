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

  it("opens with the roots and their children, and descends where asked", () => {
    hierarchy.mockReturnValue({ data: tree });
    render(<HierarchyMenu />);
    openMenu();

    expect(screen.getByRole("menu", { name: "Hierarchy" })).toBeInTheDocument();
    // A root is always open, so it has no fold control at all — only what is under it does.
    expect(screen.getByRole("menuitem", { name: "Root" })).toHaveAttribute("href", "/notes/1");
    expect(screen.queryByRole("menuitem", { name: "Collapse Root" })).not.toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Child" })).toHaveAttribute("href", "/notes/2");
    // One level at a time: the branch under Child stays folded until it is asked for.
    expect(screen.queryByRole("menuitem", { name: "Grandchild" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitem", { name: "Expand Child" }));
    expect(screen.getByRole("menuitem", { name: "Grandchild" })).toHaveAttribute("href", "/notes/3");
  });

  it("says so rather than opening an empty panel when nothing is placed by a hierarchy", () => {
    hierarchy.mockReturnValue({ data: { hierarchy: [] } });
    render(<HierarchyMenu />);
    openMenu();

    expect(screen.getByText("No hierarchy")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem")).not.toBeInTheDocument();
  });

  it("remembers what was unfolded across a reload, and folding it again too", () => {
    hierarchy.mockReturnValue({ data: tree });
    const first = render(<HierarchyMenu />);
    openMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Expand Child" }));

    first.unmount();
    const second = render(<HierarchyMenu />);
    openMenu();
    expect(screen.getByRole("menuitem", { name: "Grandchild" })).toBeInTheDocument();

    // Folding it back leaves the row that folded it, and is remembered the same way.
    fireEvent.click(screen.getByRole("menuitem", { name: "Collapse Child" }));
    expect(screen.queryByRole("menuitem", { name: "Grandchild" })).not.toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Expand Child" })).toHaveAttribute("aria-expanded", "false");
    expect(localStorage.getItem("track.hierarchyExpanded")).toBe("[]");

    second.unmount();
    render(<HierarchyMenu />);
    openMenu();
    expect(screen.queryByRole("menuitem", { name: "Grandchild" })).not.toBeInTheDocument();
  });

  it("gives a leaf no fold control", () => {
    hierarchy.mockReturnValue({ data: tree });
    render(<HierarchyMenu />);
    openMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Expand Child" }));

    expect(screen.getByRole("menuitem", { name: "Grandchild" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Expand Grandchild" })).not.toBeInTheDocument();
  });
});
