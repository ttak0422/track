// @ts-expect-error node builtin — no @types/node installed on purpose (tests run in Node)
import { readFileSync } from "node:fs";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { GalleryView, candidateVariants } from "./GalleryView";

afterEach(() => {
  delete document.documentElement.dataset.theme;
  delete document.documentElement.dataset.themeVariant;
});

describe("GalleryView", () => {
  it("matches the [data-theme-variant] blocks declared in candidates.css", () => {
    // candidates.css is the source of truth; the playground's list and the design-shots default
    // (which parses the same file) must not drift from it.
    // cwd-relative: vitest always runs from web/ (jsdom's import.meta.url is not a file: URL here).
    const css = readFileSync("src/dev/candidates.css", "utf8");
    // [\w-]+ keeps the header comment's "…" placeholder example out of the declared set.
    const declared = [
      ...new Set([...css.matchAll(/\[data-theme-variant="([\w-]+)"\]/g)].map((m) => m[1])),
    ];
    expect(candidateVariants).toEqual(declared);
    expect(candidateVariants.length).toBeGreaterThan(0);
  });

  it("renders one section per design.md variant and a card per candidate", () => {
    render(<GalleryView />);
    for (const heading of [
      "1 · Text control",
      "2 · Quiet chip",
      "3 · Floating layer",
      "4 · Filled action",
      "5 · Underline input",
      "6 · Section label",
    ]) {
      expect(screen.getByText(heading)).toBeInTheDocument();
    }
    for (const name of candidateVariants) {
      expect(screen.getByRole("button", { name })).toBeInTheDocument();
    }
  });

  it("switches the candidate and theme on the document element", () => {
    render(<GalleryView />);
    // The playground forces an explicit theme (candidates cannot override system-follow).
    expect(document.documentElement.dataset.theme).toBe("light");

    fireEvent.click(screen.getByRole("button", { name: candidateVariants[0] }));
    expect(document.documentElement.dataset.themeVariant).toBe(candidateVariants[0]);

    fireEvent.click(screen.getByRole("button", { name: "dark" }));
    expect(document.documentElement.dataset.theme).toBe("dark");

    fireEvent.click(screen.getByRole("button", { name: "base" }));
    expect(document.documentElement.dataset.themeVariant).toBeUndefined();
  });

  it("restores the pre-playground selection on unmount", () => {
    document.documentElement.dataset.theme = "dark";
    const { unmount } = render(<GalleryView />);
    fireEvent.click(screen.getByRole("button", { name: candidateVariants[1] }));
    fireEvent.click(screen.getByRole("button", { name: "light" }));

    unmount();
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(document.documentElement.dataset.themeVariant).toBeUndefined();
  });
});
