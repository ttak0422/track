import { useEffect, useRef, useState } from "react";
import { CodeBlock } from "./CodeBlock";
import "leaflet/dist/leaflet.css";

export type MapType = "roadmap" | "satellite" | "hybrid" | "terrain";

export interface MapFenceProps {
  lat: number;
  lon: number;
  zoom: number;
  type: string;
  label: string;
}

export function parseMapFence(info: string): Omit<MapFenceProps, "label"> | null {
  const values = new Map<string, string>();
  const tokens = info.trim().split(/\s+/).filter(Boolean);
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    if (!token.startsWith(":")) return null;
    const value = tokens[++index];
    if (!value || values.has(token.slice(1))) return null;
    values.set(token.slice(1), value);
  }
  const lat = Number(values.get("lat"));
  const lon = Number(values.get("lon"));
  const zoom = Number(values.get("zoom") ?? 10);
  const type = values.get("type") ?? "roadmap";
  if (!Number.isFinite(lat) || !Number.isFinite(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180) return null;
  if (!Number.isInteger(zoom) || zoom < 0 || zoom > 19) return null;
  if (!( ["roadmap", "satellite", "hybrid", "terrain"] as string[]).includes(type)) return null;
  return { lat, lon, zoom, type };
}

export function MapFence({ lat, lon, zoom, type, label }: MapFenceProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let map: import("leaflet").Map | undefined;
    async function renderMap() {
      try {
        const L = await import("leaflet");
        if (cancelled || !containerRef.current) return;
        map = L.map(containerRef.current, { attributionControl: true }).setView([lat, lon], zoom);
        map.on("tileerror", () => setFailed(true));
        const layers = tileLayers(L, type as MapType);
        layers.base.addTo(map);
        if (layers.overlay) layers.overlay.addTo(map);
        L.marker([lat, lon]).addTo(map).bindPopup(label || "Map location").openPopup();
      } catch {
        if (!cancelled) setFailed(true);
      }
    }
    void renderMap();
    return () => {
      cancelled = true;
      map?.remove();
    };
  }, [lat, lon, zoom, type, label]);

  if (failed) {
    const href = `https://www.openstreetmap.org/?mlat=${encodeURIComponent(lat)}&mlon=${encodeURIComponent(lon)}#map=${zoom}/${lat}/${lon}`;
    return <a className="md-link map-fence-fallback" href={href} target="_blank" rel="noreferrer noopener">{label || href}</a>;
  }
  return <div className="map-fence" ref={containerRef} role="region" aria-label={label || "Map"} />;
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
  return { base: L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", { attribution: `${osm} · ${esri}` }) };
}
