import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
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

function storedIDs(): number[] {
  const raw = window.localStorage.getItem("track.tabs");
  return raw ? (JSON.parse(raw) as { id: number }[]).map((tab) => tab.id) : [];
}

describe("TabsProvider", () => {
  beforeEach(() => {
    routerMock.pathname = "/";
    routerMock.navigate.mockClear();
    window.localStorage.clear();
  });

  it("opens a tab when navigating to a note and dedupes repeats", () => {
    routerMock.pathname = "/notes/1";
    const { result, rerender } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs.map((tab) => tab.id)).toEqual([1]);

    routerMock.pathname = "/notes/2";
    rerender();
    expect(result.current.tabs.map((tab) => tab.id)).toEqual([1, 2]);

    // Returning to an already-open note activates it without duplicating the tab.
    routerMock.pathname = "/notes/1";
    rerender();
    expect(result.current.tabs.map((tab) => tab.id)).toEqual([1, 2]);
    expect(result.current.activeID).toBe(1);
  });

  it("persists the open tabs to localStorage and restores them on mount", () => {
    routerMock.pathname = "/notes/1";
    const { rerender, unmount } = renderHook(() => useTabs(), { wrapper });
    routerMock.pathname = "/notes/2";
    rerender();
    expect(storedIDs()).toEqual([1, 2]);
    unmount();

    routerMock.pathname = "/";
    const { result } = renderHook(() => useTabs(), { wrapper });
    expect(result.current.tabs.map((tab) => tab.id)).toEqual([1, 2]);
  });

  it("closes the active tab and navigates to the neighbor that fills its slot", () => {
    window.localStorage.setItem(
      "track.tabs",
      JSON.stringify([{ id: 1, title: "" }, { id: 2, title: "" }, { id: 3, title: "" }]),
    );
    routerMock.pathname = "/notes/2";
    const { result } = renderHook(() => useTabs(), { wrapper });

    act(() => result.current.close(2));
    expect(result.current.tabs.map((tab) => tab.id)).toEqual([1, 3]);
    expect(routerMock.navigate).toHaveBeenCalledWith({
      to: "/notes/$noteId",
      params: { noteId: "3" },
    });
  });

  it("falls back home when the last tab is closed", () => {
    window.localStorage.setItem("track.tabs", JSON.stringify([{ id: 1, title: "" }]));
    routerMock.pathname = "/notes/1";
    const { result } = renderHook(() => useTabs(), { wrapper });

    act(() => result.current.close(1));
    expect(result.current.tabs).toEqual([]);
    expect(routerMock.navigate).toHaveBeenCalledWith({ to: "/" });
  });

  it("does not navigate when closing an inactive tab", () => {
    window.localStorage.setItem(
      "track.tabs",
      JSON.stringify([{ id: 1, title: "" }, { id: 2, title: "" }]),
    );
    routerMock.pathname = "/notes/2";
    const { result } = renderHook(() => useTabs(), { wrapper });

    act(() => result.current.close(1));
    expect(result.current.tabs.map((tab) => tab.id)).toEqual([2]);
    expect(routerMock.navigate).not.toHaveBeenCalled();
  });
});
