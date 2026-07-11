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
    window.__trackSession = undefined;
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

  it("opens a tab when navigating to a note and dedupes repeats", () => {
    routerMock.pathname = "/notes/a1";
    const { result, rerender } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a1"]);

    routerMock.pathname = "/notes/b2";
    rerender();
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a1", "b2"]);

    // Returning to an already-open note activates it without duplicating the tab.
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
    expect(storedIDs()).toEqual(["a1", "b2"]);
    unmount();

    routerMock.pathname = "/";
    const { result } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a1", "b2"]);
  });

  it("keeps restored tabs when the session token is unchanged (a reload)", () => {
    window.__trackSession = "s1";
    window.localStorage.setItem("track.tabs.session", "s1");
    window.localStorage.setItem("track.tabs", JSON.stringify([{ id: "a", title: "" }]));
    const { result } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a"]);
  });

  it("discards restored tabs when the session token changes (a fresh launch)", () => {
    window.__trackSession = "s2";
    window.localStorage.setItem("track.tabs.session", "s1");
    window.localStorage.setItem("track.tabs", JSON.stringify([{ id: "a", title: "" }]));
    const { result } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs).toEqual([]);
    // The new token is adopted so a subsequent reload keeps whatever tabs open this run.
    expect(window.localStorage.getItem("track.tabs.session")).toBe("s2");
  });

  it("closes the active tab and navigates to the neighbor that fills its slot", () => {
    window.localStorage.setItem(
      "track.tabs",
      JSON.stringify([{ id: "a", title: "" }, { id: "b", title: "" }, { id: "c", title: "" }]),
    );
    routerMock.pathname = "/notes/b";
    const { result } = renderHook(() => useTabs(), { wrapper });

    act(() => result.current.close("b"));
    expect(result.current.tabs.map((tab) => tab.id)).toEqual(["a", "c"]);
    expect(routerMock.navigate).toHaveBeenCalledWith({
      to: "/notes/$noteId",
      params: { noteId: "c" },
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
