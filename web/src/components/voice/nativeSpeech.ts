import type { SpeechRecognitionLike } from "./useSpeechRecognition";

export type SpeechBackend = "macos" | "browser";

declare global {
  interface Window {
    __trackNativeSpeech?: { postMessage(message: Record<string, string>): void };
  }
}

export function hasNativeSpeech() {
  return typeof window !== "undefined" && Boolean(window.__trackNativeSpeech);
}

// Separate preferences keep a native choice from changing the web client's default.
function storageKey() { return `track.voice.backend.${hasNativeSpeech() ? "native" : "web"}`; }

export function readSpeechBackend(): SpeechBackend {
  try {
    const value = localStorage.getItem(storageKey());
    if (value === "browser" || (value === "macos" && hasNativeSpeech())) return value;
  } catch { /* A blocked storage area still has a predictable default. */ }
  return hasNativeSpeech() ? "macos" : "browser";
}

export function saveSpeechBackend(backend: SpeechBackend) {
  try { localStorage.setItem(storageKey(), backend); } catch { /* Session choice still works. */ }
}

/** Adapts cumulative native hypotheses to the existing final/interim editing contract. */
export function createNativeRecognition(): SpeechRecognitionLike {
  let id: string | null = null;
  let finalized = false;
  const cleanup = () => {
    window.removeEventListener("track-native-speech", receive);
    id = null;
  };
  const receive = (event: Event) => {
    const detail = (event as CustomEvent).detail;
    if (!id || detail?.id !== id) return;
    if ((detail.type === "partial" || detail.type === "final") && !finalized && typeof detail.text === "string") {
      const isFinal = detail.type === "final";
      finalized = isFinal;
      recognition.onresult?.({ resultIndex: 0, results: [{ isFinal, 0: { transcript: detail.text } }] });
    } else if (detail.type === "error") {
      recognition.onerror?.({ error: "native", message: detail.text });
    } else if (detail.type === "ended") {
      cleanup();
      recognition.onend?.();
    }
  };
  const recognition: SpeechRecognitionLike = {
    continuous: false, interimResults: true, lang: "ja-JP",
    onresult: null, onend: null, onerror: null,
    start() {
      if (id) return;
      id = crypto.randomUUID();
      finalized = false;
      window.addEventListener("track-native-speech", receive);
      try { window.__trackNativeSpeech!.postMessage({ action: "start", id, locale: recognition.lang }); }
      catch (error) { cleanup(); throw error; }
    },
    stop() { if (id) window.__trackNativeSpeech?.postMessage({ action: "stop", id }); },
    abort() {
      if (id) window.__trackNativeSpeech?.postMessage({ action: "cancel", id });
      cleanup();
    },
  };
  return recognition;
}
