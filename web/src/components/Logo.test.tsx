import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { BrandMark, TrackLogo } from "./Logo";

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

  it("falls back to the refreshed themed square mark without an icon", () => {
    const { container } = render(<BrandMark className="rail-mark" />);
    const images = container.querySelectorAll("img");
    expect(images).toHaveLength(2);
    expect(images[0]).toHaveAttribute("src", `${import.meta.env.BASE_URL}track-icon.svg`);
    expect(images[1]).toHaveAttribute("src", `${import.meta.env.BASE_URL}track-icon-dark.svg`);
    expect(images[0]).toHaveClass("rail-mark", "theme-asset-light");
    expect(images[1]).toHaveClass("rail-mark", "theme-asset-dark");
  });

  it("uses the refreshed themed lockup for the home wordmark", () => {
    const { container } = render(<TrackLogo className="home-logo" />);
    const images = container.querySelectorAll("img");
    expect(images).toHaveLength(2);
    expect(images[0]).toHaveAttribute("src", `${import.meta.env.BASE_URL}track-lockup.svg`);
    expect(images[1]).toHaveAttribute("src", `${import.meta.env.BASE_URL}track-lockup-dark.svg`);
  });
});
