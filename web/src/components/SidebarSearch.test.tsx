import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SearchProvider } from "../searchState";
import { SidebarSearch } from "./SidebarSearch";

vi.mock("./SearchPanel", () => ({
  SearchPanel: () => <input aria-label="Search notes" />,
}));

describe("SidebarSearch", () => {
  it("mounts the palette outside the rail stacking context", () => {
    render(
      <aside className="sidebar">
        <SearchProvider>
          <SidebarSearch />
        </SearchProvider>
      </aside>,
    );

    const trigger = screen.getByRole("button", { name: "Search notes" });
    // The palette names itself; a native tooltip would arrive late and on top of it.
    expect(trigger).not.toHaveAttribute("title");
    fireEvent.click(trigger);

    const popup = document.querySelector(".search-popup");
    expect(popup).not.toBeNull();
    expect(popup?.closest(".sidebar")).toBeNull();

    fireEvent.mouseDown(popup!);
    expect(screen.getByRole("dialog", { name: "Search notes" })).toBeInTheDocument();
  });
});
