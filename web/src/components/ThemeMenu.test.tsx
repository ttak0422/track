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

  function field(label: string) {
    return screen.getByLabelText(label) as HTMLInputElement;
  }

  function type(label: string, value: string) {
    fireEvent.change(field(label), { target: { value } });
  }

  function scales() {
    const root = document.documentElement.style;
    return [root.getPropertyValue("--font-scale"), root.getPropertyValue("--preview-font-scale")];
  }

  function openSettings() {
    render(<ThemeMenu />);
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
  }

  it("puts the reader and a preview window on the same scale for the same number", () => {
    openSettings();

    type("Text size", "18");
    type("Preview text size", "18");

    // The sheet's prose is 16px * the scale its surface carries, so an equal scale is equal text —
    // and 18/16 is what 18px has to mean on both.
    const [reader, preview] = scales();
    expect(reader).toBe("1.125");
    expect(preview).toBe(reader);
  });

  it("keeps the two numbers independent", () => {
    openSettings();

    type("Text size", "20");
    type("Preview text size", "14");

    expect(scales()).toEqual(["1.25", "0.875"]);
    expect(localStorage.getItem("track.fontSize")).toBe("20");
    expect(localStorage.getItem("track.previewFontSize")).toBe("14");
  });

  // The default is the absence of the setting, the way the theme's "system" and the content width's
  // "Normal" are: back at the base, both the key and the property go away again.
  it("stores nothing at the default size", () => {
    openSettings();

    type("Text size", "20");
    type("Text size", "16");

    expect(scales()).toEqual(["", ""]);
    expect(localStorage.getItem("track.fontSize")).toBeNull();
    expect(localStorage.getItem("track.previewFontSize")).toBeNull();
  });

  // Typing is a sequence of half-finished numbers, and the page is resizing under the field while it
  // happens. Every draft that is not a whole number in range leaves the size exactly where it was.
  it("holds the size while the field is cleared, mistyped, or out of range", () => {
    openSettings();
    type("Text size", "20");

    for (const draft of ["", "  ", "2", "0", "12", "33", "180", "18.5", "abc"]) {
      // jsdom, like a browser, hands back "" for what a number field cannot parse — either way the
      // draft is not a size, and the page keeps the one it has.
      type("Text size", draft);
      expect(scales()[0]).toBe("1.25");
      expect(localStorage.getItem("track.fontSize")).toBe("20");
    }

    // And the field is not left showing a draft the size never took.
    fireEvent.blur(field("Text size"));
    expect(field("Text size").value).toBe("20");
  });

  it("offers the range to the spinner and the arrow keys", () => {
    openSettings();
    const input = field("Text size");

    expect(input.type).toBe("number");
    expect(input.min).toBe("13");
    expect(input.max).toBe("32");
    expect(input.step).toBe("1");
  });

  it("ignores a stored size from outside the range", () => {
    localStorage.setItem("track.fontSize", "200");
    openSettings();

    expect(field("Text size").value).toBe("16");
    expect(scales()[0]).toBe("");
  });
});
