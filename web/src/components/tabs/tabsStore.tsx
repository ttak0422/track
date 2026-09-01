import { useNavigate, useRouterState } from "@tanstack/react-router";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { START_PAGE_ID, STATIC_MODE } from "../../runtime";
import type { NoteID } from "../../types";

// A note open in the tab bar. The title is cached so a reloaded session can label tabs before each
// note's data resolves; it is refreshed from the note query when a tab becomes active.
export interface NoteTab {
  id: NoteID;
  title: string;
}

interface TabsApi {
  tabs: NoteTab[];
  // Notes visited this browser, most recent first. Like the open strip it outlives a server restart —
  // "recently opened" is a property of the person, not of the session. It reaches further back than
  // the strip: a tab is closed when you are done with it, a visit still happened.
  recent: NoteTab[];
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
// Recently opened notes, the History panel's list.
const RECENT_KEY = "track.recent";
// Deep enough that an afternoon of reading is still in the panel. `track web` is relaunched often
// (`:Track web` from the editor), and a history that only reaches back ten notes is one an ordinary
// session walks off the end of.
const RECENT_LIMIT = 100;

// The full-page views (graph, calendar) open as ordinary tabs with fixed labels rather than separate
// overlays. Each uses a sentinel id and routes to its own path instead of /notes/$id. A note slug equal
// to a sentinel would collide, but live ids are numeric and such a static slug is vanishingly unlikely.
// ponytail: sentinel ids, revisit if slugs ever collide.
export const GRAPH_TAB_ID = "graph";
export const CALENDAR_TAB_ID = "calendar";
const VIEW_TABS: Record<string, { to: "/graph" | "/calendar"; label: string }> = {
  [GRAPH_TAB_ID]: { to: "/graph", label: "Graph" },
  [CALENDAR_TAB_ID]: { to: "/calendar", label: "Calendar" },
};

// isViewTab tells a view tab (graph/calendar) apart from a note tab, e.g. to skip note-only actions.
export function isViewTab(id: NoteID): boolean {
  return id in VIEW_TABS;
}

// The route a tab points at: a view tab goes to its own path, every other tab to its note.
export function tabRoute(id: NoteID) {
  const view = VIEW_TABS[id];
  return view
    ? ({ to: view.to } as const)
    : ({ to: "/notes/$noteId", params: { noteId: String(id) } } as const);
}

// Open tabs survive both a reload and a relaunch (persisted to localStorage); the dirty flag does
// not. The strip used to be dropped whenever `track web` started a new process, which made closing
// the workspace the one action that quietly threw away where you had been.
function loadTabs(): NoteTab[] {
  try {
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

// Exported because the reader's own note is the one thing a list of notes must not offer to open
// again (SearchPanel), and the route is where that answer lives.
export function noteIDFromPath(pathname: string): NoteID | null {
  // Tolerate a trailing slash: the prerendered static site serves each route as a directory
  // (/notes/<id>/), so the router's pathname carries the slash.
  const path = pathname.replace(/\/$/, "") || "/";
  // On the static site "/" is the start page (the root note), so give it the root note's tab — the tab
  // exists from the first render and the brand button (which navigates to "/") keeps it active.
  if (path === "/" && STATIC_MODE && START_PAGE_ID) return START_PAGE_ID;
  if (path === "/graph") return GRAPH_TAB_ID;
  if (path === "/calendar") return CALENDAR_TAB_ID;
  const match = path.match(/^\/notes\/([^/]+)$/);
  return match ? match[1] : null;
}

// Recents survive a new server session (loadTabs clears the strip, not this), so the list answers
// "where have I been" rather than "what is open".
function loadRecent(): NoteTab[] {
  try {
    const raw = window.localStorage.getItem(RECENT_KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((entry): entry is NoteTab => typeof (entry as NoteTab)?.id === "string")
      .slice(0, RECENT_LIMIT);
  } catch {
    return [];
  }
}

export function TabsProvider({ children }: { children: ReactNode }) {
  // Start empty so a prerendered page and the client's first (hydration) render agree — localStorage is
  // client-only, so reading it during render would desync SSR HTML from hydration. The persisted strip is
  // restored in a mount effect below instead.
  const [tabs, setTabs] = useState<NoteTab[]>([]);
  const [recent, setRecent] = useState<NoteTab[]>([]);
  const [dirtyID, setDirtyID] = useState<NoteID | null>(null);
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const navigate = useNavigate();
  const activeID = noteIDFromPath(pathname);
  // Skip the first persist (of the empty initial strip) so it does not clobber the saved tabs before the
  // restore effect reads them.
  const persistArmed = useRef(false);
  // Titles reported via setTitle for tabs that do not exist yet (see the append effect below).
  const knownTitles = useRef(new Map<NoteID, string>());

  // Restore the persisted strip and recents once, after mount (localStorage is client-only, so
  // reading during render would desync the prerendered HTML from hydration).
  useEffect(() => {
    const restored = loadTabs();
    if (restored.length > 0) setTabs(restored);
    const remembered = loadRecent();
    if (remembered.length > 0) setRecent(remembered);
  }, []);

  // Every note the route lands on goes to the front of the recents, deduped. View tabs (graph,
  // calendar) are not notes and are skipped.
  useEffect(() => {
    if (activeID === null || isViewTab(activeID)) return;
    setRecent((current) => {
      const title = knownTitles.current.get(activeID) ?? current.find((r) => r.id === activeID)?.title ?? "";
      const next = [{ id: activeID, title }, ...current.filter((r) => r.id !== activeID)].slice(
        0,
        RECENT_LIMIT,
      );
      try {
        window.localStorage.setItem(RECENT_KEY, JSON.stringify(next));
      } catch {
        // A full or unavailable localStorage just means recents are session-only this run.
      }
      return next;
    });
  }, [activeID]);

  // Navigating to a note brings its tab to the front of the strip, opening one if it is not already
  // there. The strip is therefore most-recent-first: the note being read is always the leftmost tab
  // (so it is always visible, whatever the strip has room for) and the order behind it is the order
  // the notes were visited in.
  useEffect(() => {
    if (activeID === null) return;
    setTabs((current) => {
      if (current[0]?.id === activeID) return current;
      const existing = current.find((tab) => tab.id === activeID);
      // View tabs carry a fixed label; note tabs get theirs once the note resolves — or from a
      // setTitle that already arrived (child effects run before this parent effect, so a note
      // hydrated from prerendered state reports its title before its tab exists).
      const tab = existing ?? {
        id: activeID,
        title: VIEW_TABS[activeID]?.label ?? knownTitles.current.get(activeID) ?? "",
      };
      return [tab, ...current.filter((entry) => entry.id !== activeID)];
    });
  }, [activeID]);

  // Persist the open set/order (without titles' dirtiness) so a reload restores the strip. The first run
  // (the empty initial strip) is skipped so it cannot overwrite the saved tabs before they are restored.
  useEffect(() => {
    if (!persistArmed.current) {
      persistArmed.current = true;
      return;
    }
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(tabs));
    } catch {
      // A full or unavailable localStorage just means tabs are session-only this run.
    }
  }, [tabs]);

  const setTitle = useCallback<TabsApi["setTitle"]>((id, title) => {
    knownTitles.current.set(id, title);
    setTabs((current) =>
      current.map((tab) => (tab.id === id && tab.title !== title ? { ...tab, title } : tab)),
    );
    // A note is recorded as recent the moment it is opened, before its title resolves; label it
    // when it does, and persist so the label survives the next launch.
    setRecent((current) => {
      if (!current.some((r) => r.id === id && r.title !== title)) return current;
      const next = current.map((r) => (r.id === id ? { ...r, title } : r));
      try {
        window.localStorage.setItem(RECENT_KEY, JSON.stringify(next));
      } catch {
        // Same as above: labels are then session-only.
      }
      return next;
    });
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
      // its left) — in a most-recent-first strip the active tab is the first, so that is the note
      // visited before it. With none left, fall back home: the empty state on the static site (whose
      // "/" is the start page), or "/" (the heatmap home) on the live workspace.
      if (id === activeID) {
        const target = next[index] ?? next[index - 1] ?? null;
        void navigate(target ? tabRoute(target.id) : { to: STATIC_MODE ? "/empty" : "/" });
      }
    },
    [tabs, activeID, dirtyID, navigate],
  );

  const api = useMemo<TabsApi>(
    () => ({ tabs, recent, activeID, dirtyID, setTitle, setDirty, close }),
    [tabs, recent, activeID, dirtyID, setTitle, setDirty, close],
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
