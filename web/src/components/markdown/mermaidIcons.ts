import type { Mermaid } from "mermaid";

const registered = new WeakSet<Mermaid>();

// Keep the standard logos: names from Mermaid's architecture examples. The pack is a local
// Vite chunk, loaded only when a diagram uses it; neither source nor icon names go to a CDN.
// Register once per renderer so theme changes and additional diagrams reuse Mermaid's icon cache.
export function registerMermaidIcons(mermaid: Mermaid): void {
  if (registered.has(mermaid)) return;
  mermaid.registerIconPacks([
    {
      name: "logos",
      loader: () => import("@iconify-json/logos").then((module) => module.icons),
    },
  ]);
  registered.add(mermaid);
}
