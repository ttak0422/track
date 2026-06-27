import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { FloatingProvider, useFloating } from "./floatingStore";

const bounds = { left: 0, top: 0, width: 300, height: 200 };

function wrapper({ children }: { children: ReactNode }) {
  return <FloatingProvider>{children}</FloatingProvider>;
}

describe("FloatingProvider", () => {
  it("pins, dedupes by content key, raises, and removes", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });

    act(() => result.current.pin({ kind: "note", noteID: 1 }, bounds, false));
    expect(result.current.windows).toHaveLength(1);

    // Same note again: no new window, but it is raised to the front.
    const firstOrder = result.current.windows[0].stackOrder;
    act(() => result.current.pin({ kind: "note", noteID: 1 }, bounds, false));
    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].stackOrder).toBeGreaterThan(firstOrder);

    // A different note adds a second window.
    act(() => result.current.pin({ kind: "note", noteID: 2 }, bounds, false));
    expect(result.current.windows).toHaveLength(2);

    act(() => result.current.remove(result.current.windows[0].id));
    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].content).toEqual({ kind: "note", noteID: 2 });
  });

  it("treats note and media as distinct content", () => {
    const { result } = renderHook(() => useFloating(), { wrapper });
    act(() => result.current.pin({ kind: "note", noteID: 1 }, bounds, false));
    act(() => result.current.pin({ kind: "media", src: "a.png", alt: "", noteKind: "note" }, bounds, false));
    expect(result.current.windows).toHaveLength(2);
  });
});
