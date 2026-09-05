import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MapFence, parseMapFence } from "./MapFence";

vi.mock("leaflet", () => ({
  map: vi.fn(() => {
    throw new Error("map unavailable");
  }),
  tileLayer: vi.fn(),
  marker: vi.fn(),
}));

describe("parseMapFence", () => {
  it("parses Org-style options and defaults", () => {
    expect(parseMapFence(":lat 29.245 :lon 50.31 :zoom 11 :type satellite")).toEqual({
      lat: 29.245,
      lon: 50.31,
      zoom: 11,
      type: "satellite",
    });
    expect(parseMapFence(":lat 1 :lon 2")).toEqual({ lat: 1, lon: 2, zoom: 10, type: "roadmap" });
  });

  it("rejects missing and invalid options", () => {
    expect(parseMapFence(":lat nope :lon 2")).toBeNull();
    expect(parseMapFence(":lat 1 :lon 2 :type streets")).toBeNull();
    expect(parseMapFence(":lat 1")).toBeNull();
  });
});

describe("MapFence", () => {
  it("falls back to a link when Leaflet cannot render", async () => {
    render(<MapFence lat={1} lon={2} zoom={10} type="roadmap" label="Fallback place" />);
    await waitFor(() => expect(screen.getByRole("link", { name: "Fallback place" })).toBeInTheDocument());
  });
});
