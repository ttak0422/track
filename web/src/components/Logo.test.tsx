import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { BrandMark } from "./Logo";

describe("BrandMark", () => {
  it("renders the published site icon under the Vite base", () => {
    const { container } = render(<BrandMark icon="icon.png" className="rail-mark" />);
    const img = container.querySelector("img");
    expect(img).toBeTruthy();
    expect(img?.getAttribute("src")).toBe(`${import.meta.env.BASE_URL}icon.png`);
    expect(img).toHaveClass("rail-mark");
    // Decorative: the wrapping link carries the accessible label.
    expect(img?.getAttribute("alt")).toBe("");
  });

  it("falls back to the built-in k mark without an icon", () => {
    const { container } = render(<BrandMark className="rail-mark" />);
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("svg")).toHaveClass("rail-mark");
  });
});
