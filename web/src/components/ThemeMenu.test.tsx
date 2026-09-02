import { fireEvent, render, screen, within } from "@testing-library/react";
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

describe("ThemeMenu text size", () => {
  afterEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute("style");
  });

  function pick(group: string, label: string) {
    fireEvent.click(within(screen.getByRole("group", { name: group })).getByRole("button", { name: label }));
  }

  function scales() {
    const root = document.documentElement.style;
    return [root.getPropertyValue("--font-scale"), root.getPropertyValue("--preview-font-scale")];
  }

  it("puts the reader and a preview window on the same scale for the same number", () => {
    render(<ThemeMenu />);
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));

    pick("Text size", "18px");
    pick("Preview text size", "18px");

    // The sheet's prose is 16px * the scale its surface carries, so an equal scale is equal text —
    // and 18/16 is what 18px has to mean on both.
    const [reader, preview] = scales();
    expect(reader).toBe("1.125");
    expect(preview).toBe(reader);
  });

  it("keeps the two numbers independent", () => {
    render(<ThemeMenu />);
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));

    pick("Text size", "20px");
    pick("Preview text size", "14px");

    expect(scales()).toEqual(["1.25", "0.875"]);
    expect(localStorage.getItem("track.fontSize")).toBe("20");
    expect(localStorage.getItem("track.previewFontSize")).toBe("14");
  });

  // The default is the absence of the setting, the way the theme's "system" and the content width's
  // "Normal" are: back at the base, both the key and the property go away again.
  it("stores nothing at the default size", () => {
    render(<ThemeMenu />);
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));

    pick("Text size", "20px");
    pick("Text size", "16px");

    expect(scales()).toEqual(["", ""]);
    expect(localStorage.getItem("track.fontSize")).toBeNull();
    expect(localStorage.getItem("track.previewFontSize")).toBeNull();
  });
});
