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
import type { NoteID } from "../../types";

// A note open in the tab bar. The title is cached so a reloaded session can label tabs before each
// note's data resolves; it is refreshed from the note query when a tab becomes active.
export interface NoteTab {
  id: NoteID;
  title: string;
}

interface TabsApi {
  tabs: NoteTab[];
  // The note id of the route currently shown, or null when off a note (home/graph). Used to mark the
  // active tab; it may not be in `tabs` for a frame until the open effect adds it.
  activeID: NoteID | null;
  // The single note with unsaved edits, if any. Only one note is editable at a time, so dirtiness is a
  // single id rather than a per-tab flag (and it is never persisted).
  dirtyID: NoteID | null;
  setTitle: (id: NoteID, title: string) => void;
  setDirty: (id: NoteID | null) => void;
  close: (id: NoteID) => void;
}

const TabsContext = createContext<TabsApi | null>(null);

const STORAGE_KEY = "track.tabs";
// The server session token the persisted tabs belong to. A reload carries the same token (keep the
// tabs); a fresh `track web` launch injects a new one (discard the tabs, so a new day's `Track new`
// starts clean rather than restoring yesterday's strip).
const SESSION_KEY = "track.tabs.session";

// The full graph opens as an ordinary tab (labelled "Graph") rather than a separate overlay. It uses a
// sentinel id and routes to /graph instead of /notes/$id. A note slug of exactly "graph" would collide,
// but live ids are numeric and that static slug is vanishingly unlikely. ponytail: sentinel id, revisit
// if slugs ever need to be "graph".
export const GRAPH_TAB_ID = "graph";

// The route a tab points at: the graph tab goes to /graph, every other tab to its note.
export function tabRoute(id: NoteID) {
  return id === GRAPH_TAB_ID
    ? ({ to: "/graph" } as const)
    : ({ to: "/notes/$noteId", params: { noteId: String(id) } } as const);
}

// Open tabs survive reloads (persisted to localStorage); the dirty flag does not. A fresh server
// launch (new session token) starts with an empty strip instead of restoring the previous run's tabs.
function loadTabs(): NoteTab[] {
  try {
    const session = window.__trackSession ?? "";
    if (session && window.localStorage.getItem(SESSION_KEY) !== session) {
      window.localStorage.setItem(SESSION_KEY, session);
      window.localStorage.removeItem(STORAGE_KEY);
      return [];
    }
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.flatMap((entry) => {
      const id = (entry as { id?: unknown }).id;
      if (typeof id !== "string" || id === "") return [];
      const title = (entry as { title?: unknown }).title;
      return [{ id, title: typeof title === "string" ? title : "" }];
    });
  } catch {
    return [];
  }
}

function noteIDFromPath(pathname: string): NoteID | null {
  if (pathname === "/graph") return GRAPH_TAB_ID;
  const match = pathname.match(/^\/notes\/([^/]+)$/);
  return match ? match[1] : null;
}

export function TabsProvider({ children }: { children: ReactNode }) {
  const [tabs, setTabs] = useState<NoteTab[]>(loadTabs);
  const [dirtyID, setDirtyID] = useState<NoteID | null>(null);
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const navigate = useNavigate();
  const activeID = noteIDFromPath(pathname);

  // Navigating to a note opens a tab for it (appended) unless one is already open.
  useEffect(() => {
    if (activeID === null) return;
    setTabs((current) =>
      current.some((tab) => tab.id === activeID)
        ? current
        : // The graph tab carries a fixed label; note tabs get theirs once the note resolves.
          [...current, { id: activeID, title: activeID === GRAPH_TAB_ID ? "Graph" : "" }],
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
        void navigate(target ? tabRoute(target.id) : { to: "/" });
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
