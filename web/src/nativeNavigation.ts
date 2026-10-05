import { useLayoutEffect, useRef, type RefObject } from "react";

declare global {
  interface Window {
    // Read-only editor state. This is deliberately not a native message/command bridge.
    // Absence means an unrecognised page, for which the shell stays conservative.
    __trackNativeEditorState?: () => boolean;
  }
}

interface EditorState {
  body: string;
  textarea: RefObject<HTMLTextAreaElement | null>;
  savedBody: () => string;
  pending: boolean;
  isDeleted: () => boolean;
}

export function useNativeEditorState(state: EditorState): void {
  const current = useRef(state);
  useLayoutEffect(() => { current.current = state; });
  useLayoutEffect(() => {
    let composing = false;
    const start = (event: Event) => {
      if (event.target === current.current.textarea.current) composing = true;
    };
    const end = (event: Event) => {
      if (event.target === current.current.textarea.current) composing = false;
    };
    const read = () => {
      const editor = current.current;
      if (editor.isDeleted()) return false;
      // Inspect the actual textarea as well as React state: marked IME text and the final input
      // event can precede React's commit. The saved baseline is read at query time, not copied.
      return composing || editor.pending ||
        (editor.textarea.current?.value ?? editor.body) !== editor.savedBody();
    };
    window.__trackNativeEditorState = read;
    document.addEventListener("compositionstart", start, true);
    document.addEventListener("compositionend", end, true);
    return () => {
      document.removeEventListener("compositionstart", start, true);
      document.removeEventListener("compositionend", end, true);
      if (window.__trackNativeEditorState === read) delete window.__trackNativeEditorState;
    };
  }, []);
}
