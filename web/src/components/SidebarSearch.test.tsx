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

    fireEvent.click(screen.getByRole("button", { name: "Search notes" }));

    const popup = document.querySelector(".search-popup");
    expect(popup).not.toBeNull();
    expect(popup?.closest(".sidebar")).toBeNull();

    fireEvent.mouseDown(popup!);
    expect(screen.getByRole("dialog", { name: "Search notes" })).toBeInTheDocument();
  });
});
