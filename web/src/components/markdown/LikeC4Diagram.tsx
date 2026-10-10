import { useEffect, useState } from "react";
import { useVisible } from "../../hooks/useVisible";
import { DiagramFrame } from "./MermaidDiagram";
import type { LikeC4Result } from "./likec4Types";

export function LikeC4Diagram({ text }: { text: string }) {
  const { ref, visible } = useVisible<HTMLDivElement>();
  const [state, setState] = useState<LikeC4Result | { status: "loading" }>({ status: "loading" });
  const [selected, setSelected] = useState("");
  useEffect(() => {
    if (!visible) return;
    const abort = new AbortController();
    setState({ status: "loading" });
    void import("./likec4Engine").then(({ renderLikeC4 }) => renderLikeC4(text, abort.signal)).then(
      (views) => {
        if (abort.signal.aborted) return;
        setSelected((previous) => views.some((view) => view.id === previous) ? previous : views[0].id);
        setState({ status: "ready", views });
      },
      (error: unknown) => {
        if (!abort.signal.aborted) {
          const detail = error instanceof Error ? error.message : "Could not render the model.";
          setState({ status: "error", message: `LikeC4 render failed: ${detail}` });
        }
      },
    );
    return () => abort.abort();
  }, [text, visible]);

  const view = state.status === "ready" ? state.views.find((item) => item.id === selected) ?? state.views[0] : null;
  return (
    <div ref={ref} className="likec4-diagram">
      {state.status === "ready" && state.views.length > 1 && (
        <div className="likec4-views" role="group" aria-label="LikeC4 views">
          {state.views.map((item) => (
            <button key={item.id} type="button" aria-pressed={item.id === view?.id}
              onClick={() => setSelected(item.id)} title={item.id}>
              {item.title}
            </button>
          ))}
        </div>
      )}
      <DiagramFrame
        key={view?.id ?? "pending"}
        state={view ? { status: "ready", svg: view.svg } : state.status === "error" ? state : { status: "loading" }}
        source={text}
        sourceLang="likec4"
        label={view ? `LikeC4 diagram: ${view.title}` : "LikeC4 diagram"}
        className="graphviz-diagram"
      />
    </div>
  );
}
