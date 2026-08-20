import { act, renderHook } from "@testing-library/react";
import { useEffect, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { TabsProvider, useTabs } from "./tabsStore";

// The store reads the route to know the active note and to open/close tabs.
const routerMock = vi.hoisted(() => ({ pathname: "/", navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => routerMock.pathname,
  useNavigate: () => routerMock.navigate,
}));

function wrapper({ children }: { children: ReactNode }) {
  return <TabsProvider>{children}</TabsProvider>;
}

function storedIDs(): string[] {
  const raw = window.localStorage.getItem("track.tabs");
  return raw ? (JSON.parse(raw) as { id: string }[]).map((tab) => tab.id) : [];
}

describe("TabsProvider", () => {
  beforeEach(() => {
    routerMock.pathname = "/";
    routerMock.navigate.mockClear();
    window.localStorage.clear();
  });

  it("keeps a title reported before its tab exists (prerender hydration order)", () => {
    // A note hydrated from prerendered state knows its title on first render, so the reader's
    // setTitle effect fires before the provider's append effect creates the tab (child effects run
    // first). The late-appended tab must still pick the title up instead of staying unlabeled.
    function TitleReporter() {
      const { setTitle } = useTabs();
      useEffect(() => {
        setTitle("a1", "Alpha");
      }, [setTitle]);
      return null;
    }
    routerMock.pathname = "/notes/a1";
    const { result } = renderHook(() => useTabs(), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <TabsProvider>
          <TitleReporter />
          {children}
        </TabsProvider>
      ),
    });
    expect(result.current.tabs).toEqual([{ id: "a1", title: "Alpha" }]);
  });

  it("opens a tab when navigating to a note, most recent first, and dedupes repeats", () => {
    routerMock.pathname = "/notes/a1";
    const { result, rerender } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a1"]);

    // The note being read is always the leftmost tab, so it is always on the strip whatever the
    // strip has room for.
    routerMock.pathname = "/notes/b2";
    rerender();
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["b2", "a1"]);

    // Returning to an already-open note moves it to the front instead of duplicating the tab.
    routerMock.pathname = "/notes/a1";
    rerender();
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a1", "b2"]);
    expect(result.current.activeID).toBe("a1");
  });

  it("persists the open tabs to localStorage and restores them on mount", () => {
    routerMock.pathname = "/notes/a1";
    const { rerender, unmount } = renderHook(() => useTabs(), { wrapper });
    routerMock.pathname = "/notes/b2";
    rerender();
    expect(storedIDs()).toEqual(["b2", "a1"]);
    unmount();

    routerMock.pathname = "/";
    const { result } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["b2", "a1"]);
  });

  it("keeps restored tabs across a relaunch, not only a reload", () => {
    // The strip used to be keyed to the server process, so quitting `track web` threw it away. It is
    // plain localStorage now: only closing a tab closes it.
    window.localStorage.setItem("track.tabs", JSON.stringify([{ id: "a", title: "" }]));
    const { result } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a"]);
  });

  it("closes the active tab and navigates to the note visited before it", () => {
    window.localStorage.setItem(
      "track.tabs",
      JSON.stringify([{ id: "a", title: "" }, { id: "b", title: "" }, { id: "c", title: "" }]),
    );
    routerMock.pathname = "/notes/b";
    const { result } = renderHook(() => useTabs(), { wrapper });
    // Opening b brought it to the front of the most-recent-first strip.
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["b", "a", "c"]);

    act(() => result.current.close("b"));
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a", "c"]);
    expect(routerMock.navigate).toHaveBeenCalledWith({
      to: "/notes/$noteId",
      params: { noteId: "a" },
    });
  });

  it("falls back home when the last tab is closed", () => {
    window.localStorage.setItem("track.tabs", JSON.stringify([{ id: "a", title: "" }]));
    routerMock.pathname = "/notes/a";
    const { result } = renderHook(() => useTabs(), { wrapper });

    act(() => result.current.close("a"));
    expect(result.current.tabs).toEqual([]);
    expect(routerMock.navigate).toHaveBeenCalledWith({ to: "/" });
  });

  it("opens the full graph as a tab labelled Graph", () => {
    routerMock.pathname = "/graph";
    const { result } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs).toEqual([{ id: "graph", title: "Graph" }]);
    expect(result.current.activeID).toBe("graph");
  });

  it("opens the calendar as a tab labelled Calendar and routes back to it", () => {
    routerMock.pathname = "/calendar";
    const { result } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs).toEqual([{ id: "calendar", title: "Calendar" }]);
    expect(result.current.activeID).toBe("calendar");
  });

  it("routes back to /graph when closing a note tab next to the graph tab", () => {
    window.localStorage.setItem(
      "track.tabs",
      JSON.stringify([{ id: "graph", title: "Graph" }, { id: "a", title: "" }]),
    );
    routerMock.pathname = "/notes/a";
    const { result } = renderHook(() => useTabs(), { wrapper });

    act(() => result.current.close("a"));
    expect(routerMock.navigate).toHaveBeenCalledWith({ to: "/graph" });
  });

  it("remembers recently opened notes, most recent first, and outlives a relaunch", () => {
    routerMock.pathname = "/notes/a1";
    const { result, rerender } = renderHook(() => useTabs(), { wrapper });
    routerMock.pathname = "/notes/b2";
    rerender();
    routerMock.pathname = "/graph"; // a view, not a note: never recorded
    rerender();
    routerMock.pathname = "/notes/a1"; // revisiting moves it back to the front, no duplicate
    rerender();
    expect(result.current.recent.map((note) => note.id)).toEqual(["a1", "b2"]);

    // A fresh `track web` launch keeps both: where you have been is not the server's to forget.
    routerMock.pathname = "/";
    const second = renderHook(() => useTabs(), { wrapper });
    expect(second.result.current.tabs.map((note) => note.id)).toEqual(["a1", "graph", "b2"]);
    expect(second.result.current.recent.map((note) => note.id)).toEqual(["a1", "b2"]);
  });

  it("keeps a history deeper than the strip, capped so it cannot grow without bound", () => {
    const { result, rerender } = renderHook(() => useTabs(), { wrapper });
    for (let i = 0; i < 110; i += 1) {
      routerMock.pathname = `/notes/n${i}`;
      rerender();
    }
    expect(result.current.recent).toHaveLength(100);
    expect(result.current.recent[0].id).toBe("n109");
    expect(result.current.recent[99].id).toBe("n10");
  });

  it("does not navigate when closing an inactive tab", () => {
    window.localStorage.setItem(
      "track.tabs",
      JSON.stringify([{ id: "a", title: "" }, { id: "b", title: "" }]),
    );
    routerMock.pathname = "/notes/b";
    const { result } = renderHook(() => useTabs(), { wrapper });

    act(() => result.current.close("a"));
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["b"]);
    expect(routerMock.navigate).not.toHaveBeenCalled();
  });
});
