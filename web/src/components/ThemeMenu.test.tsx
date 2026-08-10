import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ThemeMenu } from "./ThemeMenu";

describe("ThemeMenu", () => {
  it("mounts its menu outside the rail stacking context", () => {
    render(
      <aside className="sidebar">
        <ThemeMenu />
      </aside>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Settings" }));

    const menu = document.querySelector(".rail-menu-panel");
    expect(menu).not.toBeNull();
    expect(menu?.closest(".sidebar")).toBeNull();

    fireEvent.mouseDown(menu!);
    expect(screen.getByRole("group", { name: "Theme" })).toBeInTheDocument();
  });
});
