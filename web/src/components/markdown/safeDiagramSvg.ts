import DOMPurify from "dompurify";

// Graphviz output only needs SVG shapes, presentation attributes and text. This strict profile is
// intentionally unsuitable for engines that depend on CSS or HTML foreignObject labels (e.g. D2).
// No generated link, image, stylesheet, animation, or remote reference may escape into the note.
export function safeDiagramSvg(svg: string): string {
  const clean = DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true },
    FORBID_TAGS: ["style", "foreignObject", "image", "a", "use", "animate", "animateMotion", "animateTransform", "set"],
    FORBID_ATTR: ["style", "href", "xlink:href", "id", "class"],
    ALLOW_DATA_ATTR: false,
  });
  const parsed = new DOMParser().parseFromString(clean, "image/svg+xml");
  const root = parsed.documentElement;
  if (root.localName !== "svg" || parsed.querySelector("parsererror")) {
    throw new Error("The renderer did not return a valid SVG.");
  }
  for (const element of [root, ...root.querySelectorAll("*")]) {
    for (const attribute of [...element.attributes]) {
      // SVG presentation attributes can also contain CSS URLs, even without a style attribute.
      // Graphviz's solid fills and strokes need no references or escaped CSS tokens.
      if (/url\s*\(|\\|\/\*/i.test(attribute.value)) element.removeAttributeNode(attribute);
    }
  }
  return new XMLSerializer().serializeToString(root);
}
