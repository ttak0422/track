import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as L from "leaflet";
import { MapFence, parseMapFence } from "./MapFence";

vi.mock("leaflet", () => ({
  map: vi.fn(() => {
    throw new Error("map unavailable");
  }),
  tileLayer: vi.fn(),
  marker: vi.fn(),
  divIcon: vi.fn(),
}));

vi.mock("../preview/WikiLink", () => ({ WikiLink: ({ target, display }: { target: string; display: string }) => <a href={target}>{display}</a> }));

describe("parseMapFence", () => {
  it("parses body fields and repeated markers with wiki links", () => {
    expect(parseMapFence("lat: 29.245\nlong: 50.31\nzoom: 11\ntype: satellite\nmarker: default,29.245,50.31,[[カーグ島|島]], 港, 石油\nmarker: default,0,0,[[Zero]], ")).toEqual({
      lat: 29.245, long: 50.31, zoom: 11, type: "satellite",
      markers: [
        { type: "default", lat: 29.245, long: 50.31, target: "カーグ島", display: "島", description: "港, 石油" },
        { type: "default", lat: 0, long: 0, target: "Zero", display: "Zero", description: "" },
      ],
    });
    expect(parseMapFence("lat: 0\nlong: 0")).toEqual({ lat: 0, long: 0, zoom: 10, type: "roadmap", markers: [] });
  });

  it("rejects old syntax, missing fields, duplicates and invalid coordinates", () => {
    for (const body of ["", ":lat 1 :lon 2", "lat: 1", "lat: \nlong: 2", "lat: 91\nlong: 0", "lat: 1\nlong: 181", "lat: 1\nlong: 2\nlat: 3", "lat: 1\nlong: 2\nzoom: -1", "lat: 1\nlong: 2\ntype: streets", "lat: 1\nlong: 2\nmarker: default,NaN,0,[[X]], bad", "lat: 1\nlong: 2\nmarker: default,0,0,<script>, bad"]) {
      expect(parseMapFence(body), body).toBeNull();
    }
  });
});

describe("MapFence", () => {
  it("falls back to a link when Leaflet cannot render", async () => {
    render(<MapFence lat={1} long={2} zoom={10} type="roadmap" markers={[]} />);
    await waitFor(() => expect(screen.getByRole("link", { name: "Open map" })).toBeInTheDocument());
  });
});

it("renders every marker with a safe popup and the shared note link", async () => {
  const remove = vi.fn();
  const invalidateSize = vi.fn();
  let container: HTMLElement;
  vi.mocked(L.map).mockImplementationOnce((element) => {
   container = element as HTMLElement;
   const map = { setView: () => map, remove, invalidateSize };
   return map as unknown as L.Map;
  });
 vi.mocked(L.tileLayer).mockImplementation(() => {
  const layer = { on: () => layer, addTo: () => layer };
  return layer as unknown as L.TileLayer;
 });
 vi.mocked(L.marker).mockImplementation(() => {
  const marker = { addTo: () => marker, bindPopup: (element: HTMLElement) => { container.append(element); return marker; } };
  return marker as unknown as L.Marker;
 });
 const props = parseMapFence("lat: 0\nlong: 0\nmarker: default,0,0,[[Place|島]], <img src=x onerror=alert(1)>\nmarker: default,1,2,[[Other]], second")!;
 const view = render(<MapFence {...props} />);
 await waitFor(() => expect(screen.getByRole("link", { name: "島" })).toHaveAttribute("href", "Place"));
 expect(screen.getByRole("link", { name: "Other" })).toBeInTheDocument();
 expect(screen.getByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
  expect(view.container.querySelector("img")).toBeNull();
  await waitFor(() => expect(invalidateSize).toHaveBeenCalled());
  view.unmount();
  expect(remove).toHaveBeenCalledOnce();
});
