import { createContext, type ReactNode, useCallback, useContext, useMemo, useState } from "react";
import type { PreviewBounds } from "./bounds";
import { nextPreviewStackOrder } from "./stack";

export type FloatingContent =
  | { kind: "note"; noteID: number }
  | { kind: "media"; src: string; alt: string; noteKind: string };

export interface FloatingWin {
  id: string;
  content: FloatingContent;
  // Where/how the window first appears; the window owns its live bounds/collapsed after that.
  initialBounds: PreviewBounds;
  initialCollapsed: boolean;
  stackOrder: number;
}

interface FloatingApi {
  windows: FloatingWin[];
  // Pin a window into the layer. If one with the same content is already pinned, raise it instead.
  pin: (content: FloatingContent, initialBounds: PreviewBounds, initialCollapsed: boolean) => void;
  remove: (id: string) => void;
  bringToFront: (id: string) => void;
}

const FloatingContext = createContext<FloatingApi | null>(null);

// True while rendering inside a floating window, so media inside a floating window does not offer its
// own "float this" pin (which would nest a window in a window).
export const InFloatingWindowContext = createContext(false);

function contentKey(content: FloatingContent): string {
  return content.kind === "note" ? `note:${content.noteID}` : `media:${content.src}`;
}

export function FloatingProvider({ children }: { children: ReactNode }) {
  const [windows, setWindows] = useState<FloatingWin[]>([]);

  const pin = useCallback<FloatingApi["pin"]>((content, initialBounds, initialCollapsed) => {
    const key = contentKey(content);
    setWindows((current) => {
      const existing = current.find((w) => contentKey(w.content) === key);
      if (existing) {
        const order = nextPreviewStackOrder();
        return current.map((w) => (w.id === existing.id ? { ...w, stackOrder: order } : w));
      }
      return [
        ...current,
        { id: `${key}#${Date.now()}`, content, initialBounds, initialCollapsed, stackOrder: nextPreviewStackOrder() },
      ];
    });
  }, []);

  const remove = useCallback<FloatingApi["remove"]>((id) => {
    setWindows((current) => current.filter((w) => w.id !== id));
  }, []);

  const bringToFront = useCallback<FloatingApi["bringToFront"]>((id) => {
    const order = nextPreviewStackOrder();
    setWindows((current) => current.map((w) => (w.id === id ? { ...w, stackOrder: order } : w)));
  }, []);

  const api = useMemo<FloatingApi>(
    () => ({ windows, pin, remove, bringToFront }),
    [windows, pin, remove, bringToFront],
  );

  return <FloatingContext.Provider value={api}>{children}</FloatingContext.Provider>;
}

export function useFloating(): FloatingApi {
  const api = useContext(FloatingContext);
  if (!api) {
    throw new Error("useFloating must be used within a FloatingProvider");
  }
  return api;
}
