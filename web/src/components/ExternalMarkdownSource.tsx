import { createContext, type ReactNode, useCallback, useContext, useEffect, useRef, useState } from "react";
import { ExternalMarkdownSource } from "../externalMarkdown";

interface SourceState {
  source: ExternalMarkdownSource | null;
  select: (files: readonly File[], label?: string) => ExternalMarkdownSource;
  clear: () => void;
}
const SourceContext = createContext<SourceState | null>(null);

// Above the route outlet: visiting a note and using Back keeps the chosen files available. The
// Opened contents never go into local/session storage or note history; an editor handoff has already
// consumed and removed its temporary server copy before passing Files to this provider.
export function ExternalMarkdownProvider({ children }: { children: ReactNode }) {
  const [source, setSource] = useState<ExternalMarkdownSource | null>(null);
  const current = useRef<ExternalMarkdownSource | null>(null);
  useEffect(() => () => current.current?.dispose(), []);
  const select = useCallback((files: readonly File[], label?: string) => {
    const next = new ExternalMarkdownSource(files, label);
    current.current?.dispose();
    current.current = next;
    setSource(next);
    return next;
  }, []);
  const clear = useCallback(() => {
    current.current?.dispose();
    current.current = null;
    setSource(null);
  }, []);
  return <SourceContext.Provider value={{ source, select, clear }}>{children}</SourceContext.Provider>;
}

export function useExternalMarkdownSource(): SourceState {
  const state = useContext(SourceContext);
  if (!state) throw new Error("External Markdown reader needs its source provider.");
  return state;
}
