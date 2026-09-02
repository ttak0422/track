import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FloatingProvider, useFloating } from "./floatingStore";
import { getPreviewStackOrder, previewCloseDelay } from "./stack";

const routerMock = vi.hoisted(() => ({ pathname: "/" }));

// The store reads the router location to drop unpinned windows on navigation.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => routerMock.pathname,
}));

const bounds = { left: 0, top: 0, width: 300, height: 200 };

function wrapper({ children }: { children: ReactNode }) {
  return <FloatingProvider>{children}</FloatingProvider>;
}

describe("FloatingProvider", () => {
  beforeEach(() => {
    routerMock.pathname = "/";
  });

  it("opens, dedupes by content key, raises, and removes", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });

    act(() => result.current.open({ kind: "note", noteID: "1" }, bounds, false));
    expect(result.current.windows).toHaveLength(1);

    // Same note again: no new window, but it is raised to the front.
    const firstID = result.current.windows[0].id;
    const firstOrder = getPreviewStackOrder(firstID);
    act(() => result.current.open({ kind: "note", noteID: "1" }, bounds, false));
    expect(result.current.windows).toHaveLength(1);
    expect(getPreviewStackOrder(firstID)).toBe(firstOrder);

    // A different note adds a second window.
    act(() => result.current.open({ kind: "note", noteID: "2" }, bounds, false));
    expect(result.current.windows).toHaveLength(2);
    const secondID = result.current.windows[1].id;
    act(() => result.current.bringToFront(result.current.windows[0].id));
    expect(getPreviewStackOrder(result.current.windows[0].id)).toBeGreaterThan(
      getPreviewStackOrder(result.current.windows[1].id),
    );

    act(() => result.current.remove(result.current.windows[0].id));
    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].content).toEqual({ kind: "note", noteID: "2" });
  });

  it("toggles pinned without closing the window", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    act(() => result.current.open({ kind: "note", noteID: "1" }, bounds, false));
    expect(result.current.windows[0].pinned).toBe(false);
    const id = result.current.windows[0].id;
    act(() => result.current.setPinned(id, true));
    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].pinned).toBe(true);
  });

  it("treats note and media as distinct content", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    act(() => result.current.open({ kind: "note", noteID: "1" }, bounds, false));
    act(() =>
      result.current.open({ kind: "media", src: "a.png", alt: "", noteKind: "note", vault: "" }, bounds, false),
    );
    expect(result.current.windows).toHaveLength(2);
  });

  it("drops unpinned windows on route changes while keeping pinned windows", () => {
    const { result, rerender } = renderHook(() => useFloating(), { wrapper });
    act(() => result.current.open({ kind: "note", noteID: "1" }, bounds, false));
    act(() => result.current.open({ kind: "note", noteID: "2" }, bounds, false, { pinned: true }));
    expect(result.current.windows).toHaveLength(2);

    routerMock.pathname = "/graph";
    rerender();

    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].content).toEqual({ kind: "note", noteID: "2" });
    expect(result.current.windows[0].pinned).toBe(true);
  });
});

// A transient window is the one the pointer opened and the pointer can take away. Everything else
// about it — where it sits in the stack, whether it survives navigation — is the same as any other
// window in the layer, which is the point: there is one kind of window and one flat order.
describe("transient windows", () => {
  beforeEach(() => {
    routerMock.pathname = "/";
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  function openTransient(result: { current: ReturnType<typeof useFloating> }) {
    let id = "";
    act(() => {
      id = result.current.open({ kind: "note", noteID: "1" }, bounds, false, { transient: true });
    });
    return id;
  }

  it("closes after the grace period, and holds as long as the pointer keeps it", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    const id = openTransient(result);

    act(() => result.current.scheduleClose(id));
    act(() => vi.advanceTimersByTime(previewCloseDelay - 20));
    expect(result.current.windows).toHaveLength(1); // still inside the grace period

    act(() => result.current.hold(id));
    act(() => vi.advanceTimersByTime(previewCloseDelay + 50));
    expect(result.current.windows).toHaveLength(1); // held: the pointer came back

    act(() => result.current.scheduleClose(id));
    act(() => vi.advanceTimersByTime(previewCloseDelay + 10));
    expect(result.current.windows).toHaveLength(0);
  });

  it("settles on a drag, after which leaving no longer closes it", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    const id = openTransient(result);

    act(() => result.current.settle(id));
    act(() => result.current.scheduleClose(id));
    act(() => vi.advanceTimersByTime(previewCloseDelay + 50));

    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].transient).toBe(false);
  });

  it("settles when pinned, so the pointer cannot take away what was kept", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    const id = openTransient(result);

    act(() => result.current.setPinned(id, true));
    act(() => result.current.scheduleClose(id));
    act(() => vi.advanceTimersByTime(previewCloseDelay + 50));

    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].transient).toBe(false);
  });

  // Re-hovering re-places a window the pointer still owns, and leaves a settled one where the user
  // dragged it.
  it("re-anchors while transient and stays put once settled", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    const anchor = { linkLeft: 1, linkRight: 2, linkTop: 3, linkBottom: 4 };
    const moved = { linkLeft: 9, linkRight: 9, linkTop: 9, linkBottom: 9 };
    let id = "";
    act(() => {
      id = result.current.open({ kind: "note", noteID: "1" }, bounds, false, { transient: true, anchor });
    });

    act(() => {
      result.current.open({ kind: "note", noteID: "1" }, bounds, false, { transient: true, anchor: moved });
    });
    expect(result.current.windows[0].anchor).toEqual(moved);

    act(() => result.current.settle(id));
    act(() => {
      result.current.open({ kind: "note", noteID: "1" }, bounds, false, { transient: true, anchor });
    });
    expect(result.current.windows[0].anchor).toEqual(moved);
  });
});
