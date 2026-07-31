import { useEffect, useRef, useState } from "react";
import { CodeBlock } from "./CodeBlock";
import { loadDrawioViewer } from "./drawioViewer";

interface DrawioDiagramProps {
  text: string;
}

type DrawioState = "loading" | "ready" | "error";

// DrawioDiagram renders fenced ```drawio blocks and .drawio attachments with draw.io's own static
// viewer (see drawioViewer.ts for why the renderer is vendored, not an npm dep). The source is what
// draw.io saves: an <mxfile> whose <diagram> pages hold either literal <mxGraphModel> XML or the
// legacy base64+deflate payload — the viewer decodes both itself — or a bare <mxGraphModel>.
// Unlike the SVG-string engines (Mermaid/Graphviz/D2) the viewer owns a live container, so this
// renders into a host div instead of the shared DiagramFrame; the error path shows the same
// message-plus-source fallback the other engines use.
// ponytail: page 1 of a multi-page file renders; wire GraphViewer's toolbar/pages config if
// flipping pages inside a note ever matters.
export function DrawioDiagram({ text }: DrawioDiagramProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<DrawioState>("loading");
  const [message, setMessage] = useState("");

  useEffect(() => {
    let cancelled = false;
    setState("loading");

    const root = parsedRoot(text);
    if (root === null) {
      setState("error");
      setMessage("draw.io render failed: the source is not an <mxfile> or <mxGraphModel> document.");
      return;
    }

    loadDrawioViewer()
      .then((viewer) => {
        const host = hostRef.current;
        if (cancelled || !host) return;
        host.replaceChildren();
        // The viewer reads its diagram and options from data-mxgraph. The host deliberately does
        // NOT carry the "mxgraph" class the script auto-scans for, so this stays the only render.
        // No toolbar and no lightbox links: a note diagram is a static figure.
        host.dataset.mxgraph = JSON.stringify({ xml: text, page: 0, nav: false, toolbar: null });
        viewer.createViewerForElement(host);
        setState("ready");
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setState("error");
        setMessage(error instanceof Error ? `draw.io render failed: ${error.message}` : "draw.io render failed.");
      });

    return () => {
      cancelled = true;
    };
  }, [text]);

  if (state === "error") {
    return (
      <div className="mermaid-diagram mermaid-diagram-error">
        <p>{message}</p>
        <CodeBlock lang="xml" text={text} />
      </div>
    );
  }

  return (
    <div className="drawio-diagram" role="img" aria-label="draw.io diagram">
      {state === "loading" ? <div className="mermaid-diagram mermaid-diagram-loading">Rendering diagram...</div> : null}
      <div ref={hostRef} />
    </div>
  );
}

// parsedRoot accepts the two shapes draw.io saves and rejects everything else up front, so garbage
// gets the engines' usual message-plus-source fallback instead of whatever the viewer does with it.
function parsedRoot(text: string): "mxfile" | "mxGraphModel" | null {
  const doc = new DOMParser().parseFromString(text, "text/xml");
  const tag = doc.documentElement.tagName;
  if (doc.querySelector("parsererror")) return null;
  return tag === "mxfile" || tag === "mxGraphModel" ? tag : null;
}
