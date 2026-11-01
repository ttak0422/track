import DOMPurify from "dompurify";

// Keep Pele's scoped classes, CSS variables and local paint references, never active/remote content.
export function sanitizePeleSvg(svg: string): string {
  const fragment = DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true },
    FORBID_TAGS: ["a", "image", "foreignObject", "style", "use", "animate", "animateMotion", "animateTransform", "set"],
    FORBID_ATTR: ["href", "xlink:href"],
    RETURN_DOM_FRAGMENT: true,
  });
  for (const element of fragment.querySelectorAll("*")) {
    for (const attribute of [...element.attributes]) {
      if (/\\|\/\*|@/i.test(attribute.value) ||
        [...attribute.value.matchAll(/url\s*\(([^)]*)\)/gi)].some(([, raw]) => !/^#[\w.:-]+$/.test(raw.trim().replace(/^(["'])(.*)\1$/, "$2")))) {
        element.removeAttributeNode(attribute);
      }
    }
  }
  const root = fragment.querySelector("svg");
  if (!root) throw new Error("Pele did not return an SVG diagram.");
  return root.outerHTML;
}

// A popup coexists with its inline diagram: rename IDs and references without changing label text.
export function popupPeleSvg(svg: string): string {
  const document = new DOMParser().parseFromString(svg, "image/svg+xml");
  const ids = new Map([...document.querySelectorAll("[id]")].map((node) => [node.id, `${node.id}-popup`]));
  for (const node of document.querySelectorAll("*")) {
    if (node.id) node.id = ids.get(node.id)!;
    for (const attribute of [...node.attributes]) {
      if (attribute.name === "id") continue;
      if (attribute.name === "aria-labelledby" || attribute.name === "aria-describedby") {
        attribute.value = attribute.value.split(/\s+/).map((id) => ids.get(id) ?? id).join(" ");
      } else {
        attribute.value = attribute.value.replace(/url\(#([\w.:-]+)\)/g, (original, id: string) => ids.has(id) ? `url(#${ids.get(id)})` : original);
      }
    }
  }
  return new XMLSerializer().serializeToString(document.documentElement);
}
