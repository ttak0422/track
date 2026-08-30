import { useRouterState } from "@tanstack/react-router";
import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { NoteID } from "../../types";
import type { PreviewAnchor, PreviewBounds } from "./bounds";
import {
  bringPreviewToFront,
  previewCloseDelay,
  registerPreview,
  releasePreview,
} from "./stack";

export type FloatingContent =
  | { kind: "note"; noteID: NoteID }
  // A media embed keeps the vault of the note it came from: the floating layer renders outside
  // any note, so the context that told it which vault to fetch the asset from is gone by then, and
  // two vaults can hold different files under the same "assets/<name>".
  | { kind: "media"; src: string; alt: string; noteKind: string; vault: string };

export interface FloatingWin {
  id: string;
  content: FloatingContent;
  // Where/how the window first appears; the window owns its live bounds/collapsed after that.
  initialBounds: PreviewBounds;
  initialCollapsed: boolean;
  // Pinned windows persist across navigation; unpinned ones are dropped when the route changes.
  pinned: boolean;
  // A transient window belongs to the pointer that opened it: it closes once the pointer has left
  // both its opener and the window itself. Dragging, resizing, or pinning settles it for good.
  transient: boolean;
  // Where the opener sits. A still-transient window re-places itself here on a fresh hover; a
  // settled one keeps wherever the user put it.
  anchor?: PreviewAnchor;
}

export interface OpenOptions {
  pinned?: boolean;
  transient?: boolean;
  anchor?: PreviewAnchor;
}

interface FloatingApi {
  windows: FloatingWin[];
  // Open a window in the layer, returning its id. If one with the same content exists it is raised
  // rather than duplicated — the same note is one window wherever it was opened from.
  open: (
    content: FloatingContent,
    initialBounds: PreviewBounds,
    initialCollapsed: boolean,
    options?: OpenOptions,
  ) => string;
  setPinned: (id: string, pinned: boolean) => void;
  remove: (id: string) => void;
  bringToFront: (id: string) => void;
  // The hover lifecycle of a transient window. Both the opener (a link, a graph node) and the window
  // itself drive it, which is why the timer lives here instead of in either of them — and why a
  // window outlives the opener that is unmounted while the pointer rests on it.
  hold: (id: string) => void;
  scheduleClose: (id: string) => void;
  settle: (id: string) => void;
}

const FloatingContext = createContext<FloatingApi | null>(null);

// True while rendering inside a floating window, so media inside a floating window does not offer its
// own "float this" pin (which would nest a window in a window).
export const InFloatingWindowContext = createContext(false);

function contentKey(content: FloatingContent): string {
  return content.kind === "note" ? `note:${content.noteID}` : `media:${content.vault}:${content.src}`;
}

export function FloatingProvider({ children }: { children: ReactNode }) {
  const [windows, setWindows] = useState<FloatingWin[]>([]);
  // The live list, for the callbacks that have to answer about it (open returns the id of the window
  // it raised) without re-creating themselves whenever a window opens.
  const windowsRef = useRef(windows);
  windowsRef.current = windows;
  const closeTimers = useRef(new Map<string, number>());
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  const clearCloseTimer = useCallback((id: string) => {
    const timer = closeTimers.current.get(id);
    if (timer === undefined) return;
    window.clearTimeout(timer);
    closeTimers.current.delete(id);
  }, []);

  const drop = useCallback(
    (id: string) => {
      clearCloseTimer(id);
      releasePreview(id);
      setWindows((current) => current.filter((w) => w.id !== id));
    },
    [clearCloseTimer],
  );

  // Navigating drops the unpinned windows (they were only meant for the page you opened them on); pinned
  // windows stay.
  useEffect(() => {
    setWindows((current) => {
      const kept = current.filter((w) => w.pinned);
      for (const win of current) {
        if (win.pinned) continue;
        clearCloseTimer(win.id);
        releasePreview(win.id);
      }
      return kept.length === current.length ? current : kept;
    });
  }, [pathname, clearCloseTimer]);

  const open = useCallback<FloatingApi["open"]>(
    (content, initialBounds, initialCollapsed, options = {}) => {
      const key = contentKey(content);
      const existing = windowsRef.current.find((w) => contentKey(w.content) === key);
      if (existing) {
        bringPreviewToFront(existing.id);
        clearCloseTimer(existing.id);
        setWindows((current) =>
          current.map((w) =>
            w.id === existing.id
              ? {
                  ...w,
                  pinned: w.pinned || options.pinned === true,
                  // Anything opened as a keeper settles the window; a fresh hover leaves a settled
                  // one settled, and re-places one that is still transient.
                  transient: w.transient && options.transient === true,
                  anchor: w.transient ? (options.anchor ?? w.anchor) : w.anchor,
                }
              : w,
          ),
        );
        return existing.id;
      }
      // Minted outside the updater: React may run an updater more than once for a single call, and an
      // id drawn from the clock inside it would register a second stack entry that no window ever owns
      // — so it would never be released, and would hold a rank for the rest of the session.
      const id = `${key}#${Date.now()}`;
      registerPreview(id);
      setWindows((current) => [
        ...current,
        {
          id,
          content,
          initialBounds,
          initialCollapsed,
          pinned: options.pinned === true,
          transient: options.transient === true,
          anchor: options.anchor,
        },
      ]);
      return id;
    },
    [clearCloseTimer],
  );

  const setPinned = useCallback<FloatingApi["setPinned"]>(
    (id, pinned) => {
      if (pinned) clearCloseTimer(id);
      setWindows((current) =>
        // Pinning is a keeper's gesture: it settles the window as well, so the pointer moving on
        // cannot take away what the reader just asked to keep.
        current.map((w) => (w.id === id ? { ...w, pinned, transient: pinned ? false : w.transient } : w)),
      );
    },
    [clearCloseTimer],
  );

  const remove = useCallback<FloatingApi["remove"]>((id) => drop(id), [drop]);

  const bringToFront = useCallback<FloatingApi["bringToFront"]>((id) => {
    bringPreviewToFront(id);
  }, []);

  const hold = useCallback<FloatingApi["hold"]>((id) => clearCloseTimer(id), [clearCloseTimer]);

  const scheduleClose = useCallback<FloatingApi["scheduleClose"]>(
    (id) => {
      const win = windowsRef.current.find((w) => w.id === id);
      if (!win?.transient || closeTimers.current.has(id)) return;
      closeTimers.current.set(
        id,
        window.setTimeout(() => {
          closeTimers.current.delete(id);
          drop(id);
        }, previewCloseDelay),
      );
    },
    [drop],
  );

  const settle = useCallback<FloatingApi["settle"]>(
    (id) => {
      clearCloseTimer(id);
      setWindows((current) => current.map((w) => (w.id === id ? { ...w, transient: false } : w)));
    },
    [clearCloseTimer],
  );

  const api = useMemo<FloatingApi>(
    () => ({ windows, open, setPinned, remove, bringToFront, hold, scheduleClose, settle }),
    [windows, open, setPinned, remove, bringToFront, hold, scheduleClose, settle],
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
