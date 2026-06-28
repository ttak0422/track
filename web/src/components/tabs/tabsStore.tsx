import { useNavigate, useRouterState } from "@tanstack/react-router";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

// A note open in the tab bar. The title is cached so a reloaded session can label tabs before each
// note's data resolves; it is refreshed from the note query when a tab becomes active.
export interface NoteTab {
  id: number;
  title: string;
}

interface TabsApi {
  tabs: NoteTab[];
  // The note id of the route currently shown, or null when off a note (home/graph). Used to mark the
  // active tab; it may not be in `tabs` for a frame until the open effect adds it.
  activeID: number | null;
  // The single note with unsaved edits, if any. Only one note is editable at a time, so dirtiness is a
  // single id rather than a per-tab flag (and it is never persisted).
  dirtyID: number | null;
  setTitle: (id: number, title: string) => void;
  setDirty: (id: number | null) => void;
  close: (id: number) => void;
}

const TabsContext = createContext<TabsApi | null>(null);

const STORAGE_KEY = "track.tabs";

// Open tabs survive reloads (persisted to localStorage); the dirty flag does not.
function loadTabs(): NoteTab[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.flatMap((entry) => {
      const id = (entry as { id?: unknown }).id;
      if (typeof id !== "number" || !Number.isSafeInteger(id) || id <= 0) return [];
      const title = (entry as { title?: unknown }).title;
      return [{ id, title: typeof title === "string" ? title : "" }];
    });
  } catch {
    return [];
  }
}

function noteIDFromPath(pathname: string): number | null {
  const match = pathname.match(/^\/notes\/(\d+)$/);
  if (!match) return null;
  const id = Number(match[1]);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

export function TabsProvider({ children }: { children: ReactNode }) {
  const [tabs, setTabs] = useState<NoteTab[]>(loadTabs);
  const [dirtyID, setDirtyID] = useState<number | null>(null);
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const navigate = useNavigate();
  const activeID = noteIDFromPath(pathname);

  // Navigating to a note opens a tab for it (appended) unless one is already open.
  useEffect(() => {
    if (activeID === null) return;
    setTabs((current) =>
      current.some((tab) => tab.id === activeID) ? current : [...current, { id: activeID, title: "" }],
    );
  }, [activeID]);

  // Persist the open set/order (without titles' dirtiness) so a reload restores the strip.
  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(tabs));
    } catch {
      // A full or unavailable localStorage just means tabs are session-only this run.
    }
  }, [tabs]);

  const setTitle = useCallback<TabsApi["setTitle"]>((id, title) => {
    setTabs((current) =>
      current.map((tab) => (tab.id === id && tab.title !== title ? { ...tab, title } : tab)),
    );
  }, []);

  const setDirty = useCallback<TabsApi["setDirty"]>((id) => {
    setDirtyID(id);
  }, []);

  const close = useCallback<TabsApi["close"]>(
    (id) => {
      const index = tabs.findIndex((tab) => tab.id === id);
      if (index < 0) return;
      const next = tabs.filter((tab) => tab.id !== id);
      setTabs(next);
      if (id === dirtyID) setDirtyID(null);
      // Closing the active tab moves to a neighbor (the one that slides into its slot, else the tab to
      // its left); with none left, fall back home.
      if (id === activeID) {
        const target = next[index] ?? next[index - 1] ?? null;
        void navigate(
          target ? { to: "/notes/$noteId", params: { noteId: String(target.id) } } : { to: "/" },
        );
      }
    },
    [tabs, activeID, dirtyID, navigate],
  );

  const api = useMemo<TabsApi>(
    () => ({ tabs, activeID, dirtyID, setTitle, setDirty, close }),
    [tabs, activeID, dirtyID, setTitle, setDirty, close],
  );

  return <TabsContext.Provider value={api}>{children}</TabsContext.Provider>;
}

export function useTabs(): TabsApi {
  const api = useContext(TabsContext);
  if (!api) {
    throw new Error("useTabs must be used within a TabsProvider");
  }
  return api;
}
