import { renderAsync } from "pele/lazy";
import { sanitizePeleSvg, popupPeleSvg } from "./peleSvg";

let sequence = 0;

export async function renderPele(source: string, fontFamily: string, fontFamilyMono: string) {
  const { svg } = await renderAsync(source, {
    idPrefix: `track-pele-${++sequence}`,
    fontFamily,
    fontFamilyMono,
    fontSize: 16,
    responsive: false,
    autoDirection: false,
    links: false,
    images: false,
    limit: 50_000,
    outputLimit: 4_000_000,
    maxEdges: 5_000,
  });
  const clean = sanitizePeleSvg(svg);
  return { svg: clean, popupSvg: popupPeleSvg(clean) };
}
