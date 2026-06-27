import type { MermaidConfig } from "mermaid";
import { useEffect, useState } from "react";
import { CodeBlock } from "./CodeBlock";

interface MermaidDiagramProps {
  text: string;
}

type DiagramState =
  | { status: "loading" }
  | { status: "ready"; svg: string }
  | { status: "error"; message: string };

let renderSequence = 0;

// MermaidDiagram renders fenced ```mermaid blocks in the browser. Mermaid owns parsing and SVG
// generation; securityLevel strict keeps diagram directives from loosening the renderer.
export function MermaidDiagram({ text }: MermaidDiagramProps) {
  const [state, setState] = useState<DiagramState>({ status: "loading" });
  const themeVersion = useThemeVersion();

  useEffect(() => {
    let cancelled = false;
    const renderID = `track-mermaid-${++renderSequence}`;
    setState({ status: "loading" });

    async function renderDiagram() {
      try {
        const { default: mermaid } = await import("mermaid");
        mermaid.initialize(mermaidConfig());
        const { svg } = await mermaid.render(renderID, text);
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

  if (state.status === "error") {
    return (
      <div className="mermaid-diagram mermaid-diagram-error">
        <p>{state.message}</p>
        <CodeBlock lang="mermaid" text={text} />
      </div>
    );
  }

  if (state.status === "loading") {
    return <div className="mermaid-diagram mermaid-diagram-loading">Rendering diagram...</div>;
  }

  return (
    <div
      className="mermaid-diagram"
      role="img"
      aria-label="Mermaid diagram"
      dangerouslySetInnerHTML={{ __html: state.svg }}
    />
  );
}

function mermaidConfig(): MermaidConfig {
  const css = getComputedStyle(document.documentElement);
  const color = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback;

  return {
    startOnLoad: false,
    securityLevel: "strict",
    theme: "base",
    themeVariables: {
      background: color("--panel", "#ffffff"),
      primaryColor: color("--panel-soft", "#f6f6f3"),
      primaryTextColor: color("--text", "#20231f"),
      primaryBorderColor: color("--line", "#d9d6cd"),
      secondaryColor: color("--panel", "#ffffff"),
      tertiaryColor: color("--bg", "#faf9f5"),
      lineColor: color("--muted", "#666a60"),
      noteBkgColor: color("--panel-soft", "#f6f6f3"),
      noteTextColor: color("--text", "#20231f"),
      noteBorderColor: color("--line", "#d9d6cd"),
    },
    fontFamily: 'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  };
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== "") {
    return `Mermaid render failed: ${error.message}`;
  }
  return "Mermaid render failed.";
}

function useThemeVersion(): number {
  const [version, setVersion] = useState(0);

  useEffect(() => {
    const bump = () => setVersion((value) => value + 1);
    const observer = new MutationObserver(bump);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    media?.addEventListener("change", bump);

    return () => {
      observer.disconnect();
      media?.removeEventListener("change", bump);
    };
  }, []);

  return version;
}
