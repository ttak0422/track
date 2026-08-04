import { afterEach, describe, expect, it } from "vitest";
import { applyDesignPreview, parseDesignPreview } from "./preview";

describe("parseDesignPreview", () => {
  it("reads theme and variant", () => {
    expect(parseDesignPreview("?theme=dark&variant=candidate-a")).toEqual({
      theme: "dark",
      variant: "candidate-a",
    });
  });

  it("drops an unknown theme and keeps the variant", () => {
    expect(parseDesignPreview("?theme=solarized&variant=x")).toEqual({ variant: "x" });
  });

  it("returns nothing for an empty query", () => {
    expect(parseDesignPreview("")).toEqual({});
    expect(parseDesignPreview("?variant=")).toEqual({});
  });
});

describe("applyDesignPreview", () => {
  afterEach(() => {
    delete document.documentElement.dataset.theme;
    delete document.documentElement.dataset.themeVariant;
    localStorage.removeItem("track.theme");
  });

  it("sets the documentElement dataset and persists the theme where ThemeMenu reads it", () => {
    applyDesignPreview({ theme: "dark", variant: "candidate-a" });
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(document.documentElement.dataset.themeVariant).toBe("candidate-a");
    expect(localStorage.getItem("track.theme")).toBe("dark");
  });

  it("leaves the dataset alone when nothing is selected", () => {
    applyDesignPreview({});
    expect(document.documentElement.dataset.theme).toBeUndefined();
    expect(document.documentElement.dataset.themeVariant).toBeUndefined();
    expect(localStorage.getItem("track.theme")).toBeNull();
  });
});
