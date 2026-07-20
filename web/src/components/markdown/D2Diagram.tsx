import { useEffect, useState } from "react";
import { DiagramFrame, type DiagramState, isDarkColor, useThemeVersion } from "./MermaidDiagram";

interface D2DiagramProps {
  text: string;
}

// D2's stock theme ids: 0 is the neutral default, 200 is the dark default ("Dark Mauve"). A theme
// declared inside the diagram (vars.d2-config) still wins — compile merges it over these.
const lightThemeID = 0;
const darkThemeID = 200;

// Tighter than D2's 100px default, sized to sit inside a note like the other diagram engines.
const padPx = 16;

// The engine (compiler + renderer on WebAssembly) is a singleton: it costs ~8MB of lazily loaded
// code and spawns a worker, so every diagram and re-render shares one instance.
let enginePromise: Promise<InstanceType<typeof import("@terrastruct/d2").D2>> | null = null;
function loadEngine() {
  enginePromise ??= import("@terrastruct/d2").then(({ D2 }) => new D2());
  return enginePromise;
}

// Salts D2's element ids so several diagrams on one page stay valid HTML (same job as Mermaid's
// renderSequence).
let renderSalt = 0;

// D2Diagram renders fenced ```d2 blocks with D2 compiled to WebAssembly (@terrastruct/d2). It is
// wired exactly like Mermaid/Graphviz: the engine is imported lazily so a note without a d2 block
// never loads it, and a compile error falls back to the message plus the source. D2 themes its own
// SVG, so the render picks a light/dark theme id and re-renders when the app theme flips.
export function D2Diagram({ text }: D2DiagramProps) {
  const [state, setState] = useState<DiagramState>({ status: "loading" });
  const themeVersion = useThemeVersion();

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });

    async function renderDiagram() {
      try {
        const d2 = await loadEngine();
        const bg = getComputedStyle(document.documentElement).getPropertyValue("--bg").trim();
        const themeID = isDarkColor(bg) ? darkThemeID : lightThemeID;
        // The full-request form, not compile(text, options): the package's shorthand declaration
        // disagrees with its implementation about where the options ride, this shape both agree on.
        const { diagram, renderOptions } = await d2.compile({
          fs: { index: text },
          options: { themeID, pad: padPx },
        });
        const svg = await d2.render(diagram, {
          ...renderOptions,
          noXMLTag: true,
          salt: String(++renderSalt),
        });
        if (!cancelled) setState({ status: "ready", svg });
      } catch (error) {
        if (!cancelled) setState({ status: "error", message: errorMessage(error) });
      }
    }

    void renderDiagram();
    return () => {
      cancelled = true;
    };
  }, [text, themeVersion]);

  return <DiagramFrame state={state} source={text} sourceLang="d2" label="D2 diagram" />;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== "") {
    return `D2 render failed: ${error.message}`;
  }
  return "D2 render failed.";
}
