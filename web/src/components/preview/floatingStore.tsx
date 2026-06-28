import { useRouterState } from "@tanstack/react-router";
import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { NoteID } from "../../types";
import type { PreviewBounds } from "./bounds";
import { nextPreviewStackOrder } from "./stack";

export type FloatingContent =
  | { kind: "note"; noteID: NoteID }
  | { kind: "media"; src: string; alt: string; noteKind: string };

export interface FloatingWin {
  id: string;
  content: FloatingContent;
  // Where/how the window first appears; the window owns its live bounds/collapsed after that.
  initialBounds: PreviewBounds;
  initialCollapsed: boolean;
  stackOrder: number;
  // Pinned windows persist across navigation; unpinned ones are dropped when the route changes.
  pinned: boolean;
}

interface FloatingApi {
  windows: FloatingWin[];
  // Open a window in the layer. If one with the same content exists, raise it (and pin it if requested
  // now). Otherwise add it with the given pinned state.
  open: (
    content: FloatingContent,
    initialBounds: PreviewBounds,
    initialCollapsed: boolean,
    pinned: boolean,
  ) => void;
  setPinned: (id: string, pinned: boolean) => void;
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
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  // Navigating drops the unpinned windows (they were only meant for the page you opened them on); pinned
  // windows stay.
  useEffect(() => {
    setWindows((current) => {
      const kept = current.filter((w) => w.pinned);
      return kept.length === current.length ? current : kept;
    });
  }, [pathname]);

  const open = useCallback<FloatingApi["open"]>((content, initialBounds, initialCollapsed, pinned) => {
    const key = contentKey(content);
    setWindows((current) => {
      const existing = current.find((w) => contentKey(w.content) === key);
      if (existing) {
        const order = nextPreviewStackOrder();
        return current.map((w) =>
          w.id === existing.id ? { ...w, stackOrder: order, pinned: w.pinned || pinned } : w,
        );
      }
      return [
        ...current,
        {
          id: `${key}#${Date.now()}`,
          content,
          initialBounds,
          initialCollapsed,
          stackOrder: nextPreviewStackOrder(),
          pinned,
        },
      ];
    });
  }, []);

  const setPinned = useCallback<FloatingApi["setPinned"]>((id, pinned) => {
    setWindows((current) => current.map((w) => (w.id === id ? { ...w, pinned } : w)));
  }, []);

  const remove = useCallback<FloatingApi["remove"]>((id) => {
    setWindows((current) => current.filter((w) => w.id !== id));
  }, []);

  const bringToFront = useCallback<FloatingApi["bringToFront"]>((id) => {
    const order = nextPreviewStackOrder();
    setWindows((current) => current.map((w) => (w.id === id ? { ...w, stackOrder: order } : w)));
  }, []);

  const api = useMemo<FloatingApi>(
    () => ({ windows, open, setPinned, remove, bringToFront }),
    [windows, open, setPinned, remove, bringToFront],
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
