import { renderLikeC4Model } from "./likec4Model";
import type { LikeC4Result } from "./likec4Types";

// No LSP connection is started: only a source string enters, and SVG strings leave this worker.
self.onmessage = async (event: MessageEvent<string>) => {
  let result: LikeC4Result;
  try {
    result = { status: "ready", views: await renderLikeC4Model(event.data) };
  } catch (error) {
    result = { status: "error", message: error instanceof Error ? error.message : "Could not render the model." };
  }
  self.postMessage(result);
};
