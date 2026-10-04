import { createContext, type ReactNode, useContext, useEffect, useRef, useState } from "react";
import { ExternalMarkdownSource } from "../externalMarkdown";

interface SourceState {
  source: ExternalMarkdownSource | null;
  select: (files: readonly File[]) => ExternalMarkdownSource;
  clear: () => void;
}
const SourceContext = createContext<SourceState | null>(null);

// Above the route outlet: visiting a note and using Back keeps the chosen files available. The
// contents never go into local/session storage, the note tab history, or the backend.
export function ExternalMarkdownProvider({ children }: { children: ReactNode }) {
  const [source, setSource] = useState<ExternalMarkdownSource | null>(null);
  const current = useRef<ExternalMarkdownSource | null>(null);
  useEffect(() => () => current.current?.dispose(), []);
  return <SourceContext.Provider value={{
    source,
    select(files) {
      const next = new ExternalMarkdownSource(files);
      current.current?.dispose();
      current.current = next;
      setSource(next);
      return next;
    },
    clear() {
      current.current?.dispose();
      current.current = null;
      setSource(null);
    },
  }}>{children}</SourceContext.Provider>;
}

export function useExternalMarkdownSource(): SourceState {
  const state = useContext(SourceContext);
  if (!state) throw new Error("External Markdown reader needs its source provider.");
  return state;
}
