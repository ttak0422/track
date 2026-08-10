import { fireEvent, render, screen } from "@testing-library/react";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ThemeMenu } from "./ThemeMenu";

describe("ThemeMenu", () => {
  afterEach(() => localStorage.clear());

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

describe("ThemeMenu on hover", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.useRealTimers();
    localStorage.clear();
  });

  // The panel is portalled, so the container the pointer handlers sit on is the trigger's parent.
  function menuContainer() {
    return screen.getByRole("button", { name: "Settings" }).parentElement!;
  }

  it("opens on hover and closes when the pointer leaves", () => {
    render(<ThemeMenu />);
    expect(screen.queryByRole("group", { name: "Theme" })).toBeNull();

    fireEvent.pointerEnter(menuContainer());
    act(() => vi.advanceTimersByTime(500));
    expect(screen.getByRole("group", { name: "Theme" })).toBeInTheDocument();

    fireEvent.pointerLeave(menuContainer());
    act(() => vi.advanceTimersByTime(500));
    expect(screen.queryByRole("group", { name: "Theme" })).toBeNull();
  });

  it("does not open for a pointer that only passes over", () => {
    render(<ThemeMenu />);

    fireEvent.pointerEnter(menuContainer());
    act(() => vi.advanceTimersByTime(60));
    fireEvent.pointerLeave(menuContainer());
    act(() => vi.advanceTimersByTime(500));

    expect(screen.queryByRole("group", { name: "Theme" })).toBeNull();
  });

  it("survives the gap between the rail and the panel", () => {
    render(<ThemeMenu />);
    fireEvent.pointerEnter(menuContainer());
    act(() => vi.advanceTimersByTime(500));

    // Crossing the gap fires a leave, then an enter before the close lands.
    fireEvent.pointerLeave(menuContainer());
    act(() => vi.advanceTimersByTime(80));
    fireEvent.pointerEnter(menuContainer());
    act(() => vi.advanceTimersByTime(500));

    expect(screen.getByRole("group", { name: "Theme" })).toBeInTheDocument();
  });

  it("still opens on click, for a pointer that cannot hover", () => {
    render(<ThemeMenu />);

    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByRole("group", { name: "Theme" })).toBeInTheDocument();
  });
});
