import { safeDiagramSvg } from "./safeDiagramSvg";
import { maxLikeC4SourceLength, type LikeC4Result, type LikeC4View } from "./likec4Types";

const renderTimeoutMs = 30_000;

export function renderLikeC4(text: string, signal: AbortSignal): Promise<LikeC4View[]> {
  if (signal.aborted) return Promise.reject(new DOMException("Aborted", "AbortError"));
  if (text.length > maxLikeC4SourceLength) {
    return Promise.reject(new Error(`Source exceeds ${maxLikeC4SourceLength.toLocaleString("en-US")} characters.`));
  }
  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL("./likec4.worker.ts", import.meta.url), { type: "module" });
    const finish = () => {
      window.clearTimeout(timer);
      signal.removeEventListener("abort", abort);
      worker.terminate();
    };
    const abort = () => {
      finish();
      reject(new DOMException("Aborted", "AbortError"));
    };
    const timer = window.setTimeout(() => {
      finish();
      reject(new Error("Rendering exceeded 30 seconds. Reduce the model or split its views."));
    }, renderTimeoutMs);
    signal.addEventListener("abort", abort, { once: true });
    worker.onerror = (event) => {
      finish();
      reject(new Error(event.message || "The diagram worker could not start."));
    };
    worker.onmessage = (event: MessageEvent<LikeC4Result>) => {
      finish();
      if (event.data.status === "error") {
        reject(new Error(event.data.message));
        return;
      }
      try {
        resolve(event.data.views.map((view) => ({ ...view, svg: safeDiagramSvg(view.svg) })));
      } catch (error) {
        reject(error);
      }
    };
    worker.postMessage(text);
  });
}
