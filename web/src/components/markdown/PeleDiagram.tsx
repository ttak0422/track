import { type ReactNode, type RefObject, useEffect, useState } from "react";
import { useVisible } from "../../hooks/useVisible";
import { DiagramFrame, type DiagramState } from "./MermaidDiagram";

// Compare the shared subset; keep the original engine for features requiring a fidelity adapter.
export function peleFallbackReason(source: string): string | null {
  if (/\$\$/.test(source)) return "Math labels use the existing KaTeX renderer.";
  if (/^\s*---\s*\r?\n|%%\s*\{/m.test(source)) return "Mermaid frontmatter and directives retain their original configuration.";
  if (/<\/?[a-z][^>\n]*>/i.test(source)) return "HTML labels retain their original rendering.";
  if (/\([\w-]+:[\w-]+\)|\b(?:icon|img)\s*:/i.test(source)) return "Custom icons and images retain their original renderer.";
  return null;
}

export function PeleDiagram({ text, onFailure, toolbar }: { text: string; onFailure: (reason: string) => void; toolbar: ReactNode }) {
  const { ref, visible } = useVisible<HTMLDivElement>();
  const [state, setState] = useState<DiagramState>({ status: "loading" });
  const fonts = useDiagramFonts(ref);

  useEffect(() => {
    if (!visible) return;
    let cancelled = false;
    setState({ status: "loading" });
    async function draw() {
      try {
        const { renderPele } = await import("./peleEngine");
        await document.fonts?.ready;
        const result = await renderPele(text, fonts.family, fonts.mono);
        if (!cancelled) setState({ status: "ready", ...result });
      } catch (error) {
        if (!cancelled) onFailure(`Pele could not render this source. ${error instanceof Error ? error.message : "Unknown error."}`);
      }
    }
    void draw();
    return () => { cancelled = true; };
  }, [text, fonts, visible, onFailure]);

  return <DiagramFrame visibilityRef={ref} state={state} source={text} sourceLang="mermaid" label="Pele diagram" className="pele-diagram" toolbar={toolbar} />;
}

function readFonts(element: HTMLElement | null = null) {
  const css = getComputedStyle(element ?? document.documentElement);
  return {
    family: css.getPropertyValue("--pele-font").trim() || css.getPropertyValue("--font-sans").trim() || css.fontFamily || "sans-serif",
    mono: css.getPropertyValue("--pele-font-mono").trim() || css.getPropertyValue("--font-mono").trim() || "monospace",
    revision: 0,
  };
}

// Color-only changes leave the SVG intact. Font changes and late webfonts need fresh measurements.
function useDiagramFonts(ref: RefObject<HTMLDivElement | null>) {
  const [fonts, setFonts] = useState(() => readFonts());
  useEffect(() => {
    const check = () => {
      const next = readFonts(ref.current);
      setFonts((old) => old.family === next.family && old.mono === next.mono ? old : { ...next, revision: old.revision });
    };
    const loaded = () => setFonts((old) => ({ ...old, revision: old.revision + 1 }));
    const observer = new MutationObserver(check);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["style", "class", "data-theme"] });
    if (ref.current) observer.observe(ref.current, { attributes: true, attributeFilter: ["style", "class"] });
    check();
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    media?.addEventListener("change", check);
    document.fonts?.addEventListener("loadingdone", loaded);
    window.addEventListener("resize", check);
    return () => {
      observer.disconnect();
      media?.removeEventListener("change", check);
      document.fonts?.removeEventListener("loadingdone", loaded);
      window.removeEventListener("resize", check);
    };
  }, [ref]);
  return fonts;
}
