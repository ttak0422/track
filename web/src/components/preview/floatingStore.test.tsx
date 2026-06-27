import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { FloatingProvider, useFloating } from "./floatingStore";

// The store reads the router location to drop unpinned windows on navigation; stub it to a fixed path.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
}));

const bounds = { left: 0, top: 0, width: 300, height: 200 };

function wrapper({ children }: { children: ReactNode }) {
  return <FloatingProvider>{children}</FloatingProvider>;
}

describe("FloatingProvider", () => {
  it("opens, dedupes by content key, raises, and removes", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });

    act(() => result.current.open({ kind: "note", noteID: 1 }, bounds, false, false));
    expect(result.current.windows).toHaveLength(1);

    // Same note again: no new window, but it is raised to the front.
    const firstOrder = result.current.windows[0].stackOrder;
    act(() => result.current.open({ kind: "note", noteID: 1 }, bounds, false, false));
    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].stackOrder).toBeGreaterThan(firstOrder);

    // A different note adds a second window.
    act(() => result.current.open({ kind: "note", noteID: 2 }, bounds, false, false));
    expect(result.current.windows).toHaveLength(2);

    act(() => result.current.remove(result.current.windows[0].id));
    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].content).toEqual({ kind: "note", noteID: 2 });
  });

  it("toggles pinned without closing the window", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    act(() => result.current.open({ kind: "note", noteID: 1 }, bounds, false, false));
    expect(result.current.windows[0].pinned).toBe(false);
    const id = result.current.windows[0].id;
    act(() => result.current.setPinned(id, true));
    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].pinned).toBe(true);
  });

  it("treats note and media as distinct content", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    act(() => result.current.open({ kind: "note", noteID: 1 }, bounds, false, false));
    act(() =>
      result.current.open({ kind: "media", src: "a.png", alt: "", noteKind: "note" }, bounds, false, false),
    );
    expect(result.current.windows).toHaveLength(2);
  });
});
