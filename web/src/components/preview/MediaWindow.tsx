import { STATIC_MODE } from "../../runtime";
import { Embed } from "../markdown/Embed";
import { NoteKindContext } from "../markdown/context";
import { FloatingWindow, type FloatingWindowControls } from "./FloatingWindow";

interface MediaWindowProps extends FloatingWindowControls {
  src: string;
  alt: string;
  kind: string;
}

// MediaWindow frames an image/PDF/embed in a FloatingWindow so it can float and be pinned like a note.
export function MediaWindow({ src, alt, kind, ...controls }: MediaWindowProps) {
  return (
    <FloatingWindow title={mediaTitle(alt, src)} {...controls}>
      <NoteKindContext.Provider value={kind}>
        <Embed src={src} alt={alt} />
      </NoteKindContext.Provider>
    </FloatingWindow>
  );
}

// mediaTitle names the floating window: the alt text if the embed gave one (`![pod.yaml](…)`), else the
// file name. In the static export assets are content-hashed (`assets/<slug>.ext`), so a bare reference
// has no readable name — fall back to a neutral "file.<ext>" instead of showing the opaque slug. Live
// assets keep their real file name.
export function mediaTitle(alt: string, src: string, staticMode = STATIC_MODE): string {
  const named = alt.trim();
  if (named) {
    return named;
  }
  const base = (src.split(/[?#]/, 1)[0] ?? src).split("/").pop() ?? "";
  if (staticMode) {
    const dot = base.lastIndexOf(".");
    return dot > 0 ? `file${base.slice(dot)}` : "Media";
  }
  return base || "Media";
}
