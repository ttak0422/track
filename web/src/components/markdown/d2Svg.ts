import DOMPurify from "dompurify";

// D2 0.9 renders Markdown labels as native SVG. Its output still permits active links and remote
// icons, so sanitize before attaching it to the reader. Keep generated CSS and embedded fonts;
// unlike LikeC4/Graphviz, D2 needs them for labels and themes.
export function sanitizeD2Svg(svg: string): string {
  const fragment = DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true },
    FORBID_TAGS: ["a", "image", "foreignObject", "animate", "animateMotion", "animateTransform", "set"],
    RETURN_DOM_FRAGMENT: true,
  });
  for (const element of fragment.querySelectorAll("*")) {
    for (const attribute of [...element.attributes]) {
      if ((attribute.localName === "href" && !attribute.value.startsWith("#")) ||
          !localCssResources(attribute.value)) element.removeAttributeNode(attribute);
    }
    if (element.localName === "style" && !localCssResources(element.textContent ?? "")) element.remove();
  }
  const root = fragment.querySelector("svg");
  if (!root) throw new Error("D2 did not produce an SVG diagram");
  return root.outerHTML;
}

// Accept only D2's literal local paint references and embedded WOFF fonts. Reject CSS escapes,
// comments and at-rules that could hide a resource URL from this deliberately narrow check.
function localCssResources(value: string): boolean {
  if (/\\|\/\*|@import|@namespace/i.test(value)) return false;
  return [...value.matchAll(/url\s*\(([^)]*)\)/gi)].every(([, raw]) => {
    const url = raw.trim().replace(/^(["'])(.*)\1$/, "$2");
    return /^#[\w.-]+$/.test(url) || /^data:application\/font-woff;base64,[A-Za-z0-9+/=]+$/.test(url);
  });
}
