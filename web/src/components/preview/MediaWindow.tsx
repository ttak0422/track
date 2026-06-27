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
  const title = alt.trim() || fileName(src);
  return (
    <FloatingWindow title={title} {...controls}>
      <NoteKindContext.Provider value={kind}>
        <Embed src={src} alt={alt} />
      </NoteKindContext.Provider>
    </FloatingWindow>
  );
}

function fileName(src: string): string {
  const path = src.split(/[?#]/, 1)[0] ?? src;
  const base = path.split("/").pop() ?? src;
  return base || "Media";
}
