import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { WikiLink } from "../preview/WikiLink";
import "leaflet/dist/leaflet.css";

export type MapType = "roadmap" | "satellite" | "hybrid" | "terrain";

interface MapMarker {
  type: string;
  lat: number;
  long: number;
  target: string;
  display: string;
  description: string;
}

export interface MapFenceProps {
  lat: number;
  long: number;
  zoom: number;
  type: MapType;
  markers: MapMarker[];
}

function coordinate(value: string | undefined, limit: number): number | null {
  if (!value?.trim()) return null;
  const number = Number(value);
  return Number.isFinite(number) && Math.abs(number) <= limit ? number : null;
}

export function parseMapFence(body: string): MapFenceProps | null {
  const values = new Map<string, string>();
  const markers: MapMarker[] = [];
  for (const line of body.split(/\r?\n/)) {
    if (!line.trim()) continue;
    const match = line.match(/^\s*(lat|long|zoom|type|marker):\s*(.*?)\s*$/);
    if (!match) return null;
    const [, key, value] = match;
    if (key === "marker") {
      const marker = value.match(/^([^,]+),\s*([^,]+),\s*([^,]+),\s*\[\[([^\]]+)\]\]\s*,\s*(.*)$/);
      if (!marker) return null;
      const [, kind, latitude, longitude, link, description] = marker;
      const lat = coordinate(latitude, 90);
      const long = coordinate(longitude, 180);
      const [target, ...label] = link.split("|");
      if (lat === null || long === null || !target.trim()) return null;
      markers.push({ type: kind.trim(), lat, long, target: target.trim(), display: label.join("|").trim() || target.trim(), description });
    } else {
      if (values.has(key) || !value) return null;
      values.set(key, value);
    }
  }
  const lat = coordinate(values.get("lat"), 90);
  const long = coordinate(values.get("long"), 180);
  const zoom = Number(values.get("zoom") ?? 10);
  const type = values.get("type") ?? "roadmap";
  if (lat === null || long === null || !Number.isInteger(zoom) || zoom < 0 || zoom > 19) return null;
  if (type !== "roadmap" && type !== "satellite" && type !== "hybrid" && type !== "terrain") return null;
  return { lat, long, zoom, type, markers };
}

export function MapFence({ lat, long, zoom, type, markers }: MapFenceProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState(false);
  const [popups, setPopups] = useState<HTMLElement[]>([]);

  useEffect(() => {
    setFailed(false);
    setPopups([]);
    let cancelled = false;
    let map: import("leaflet").Map | undefined;
    let observer: ResizeObserver | undefined;
    async function renderMap() {
      try {
        const L = await import("leaflet");
        if (cancelled || !containerRef.current) return;
        map = L.map(containerRef.current, { attributionControl: true }).setView([lat, long], zoom);
        const layers = tileLayers(L, type);
        layers.base.on("tileerror", () => { if (!cancelled) setFailed(true); });
        layers.base.addTo(map);
        if (layers.overlay) layers.overlay.addTo(map);
        const containers = markers.map((marker) => {
          const popup = document.createElement("div");
          // ponytail: marker types share a circle; add an icon registry when distinct types are needed.
          L.marker([marker.lat, marker.long], { title: marker.display, alt: marker.display, icon: L.divIcon({ className: "map-fence-marker", iconSize: [16, 16] }) }).addTo(map!).bindPopup(popup);
          return popup;
        });
        setPopups(containers);
        // Leaflet freezes its pixel size at construction; if the container reaches its final
        // layout width a frame later (sidebar, popup, font swap), tiles come out offset or gray
        // until the size is re-read. Sync once, then follow resizes.
        requestAnimationFrame(() => { if (!cancelled) map?.invalidateSize(); });
        if (typeof ResizeObserver !== "undefined" && containerRef.current) {
          const target = containerRef.current;
          observer = new ResizeObserver(() => { if (!cancelled) map?.invalidateSize(); });
          observer.observe(target);
        }
      } catch {
        if (!cancelled) setFailed(true);
      }
    }
    void renderMap();
    return () => {
      cancelled = true;
      observer?.disconnect();
      map?.remove();
    };
  }, [lat, long, zoom, type, markers]);

  const href = `https://www.openstreetmap.org/?mlat=${encodeURIComponent(lat)}&mlon=${encodeURIComponent(long)}#map=${zoom}/${lat}/${long}`;
  return <>
    <div className="map-fence" hidden={failed} ref={containerRef} role="region" aria-label="Map" />
    {failed && <a className="md-link map-fence-fallback" href={href} target="_blank" rel="noreferrer noopener">Open map</a>}
    {popups.map((popup, index) => markers[index] ? createPortal(<>
      <WikiLink target={markers[index].target} display={markers[index].display} />
      {markers[index].description && <p>{markers[index].description}</p>}
    </>, popup, String(index)) : null)}
  </>;
}

function tileLayers(L: typeof import("leaflet"), type: MapType) {
  const osm = "© OpenStreetMap contributors";
  const esri = "Tiles © Esri";
  if (type === "roadmap") return { base: L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", { attribution: osm }) };
  const imagery = L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}", { attribution: `${esri}, © OpenStreetMap contributors` });
  if (type === "hybrid") {
    return {
      base: imagery,
      overlay: L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/Reference/World_Boundaries_and_Places/MapServer/tile/{z}/{y}/{x}", { attribution: esri }),
    };
  }
  if (type === "satellite") return { base: imagery };
  return { base: L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/World_Topo_Map/MapServer/tile/{z}/{y}/{x}", { attribution: esri }) };
}
