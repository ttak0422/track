import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GraphFullView } from "./GraphFullView";

// The whole-vault graph route on a published page must paint through the real lazy canvas: the
// GraphFullView-to-GraphCanvasLazy stack is what the SSG regression exercised (the loaded canvas
// collapsed to 1x1 behind a bare wrapper, so the note-aside graph never painted). This test keeps
// GraphCanvasLazy unmocked, unlike GraphFullView.test.tsx, so the lazy host box, the
// IntersectionObserver gate, and the canvas mount all run for real.
const floating = vi.hoisted(() => ({
  windows: [] as { id: string; content: { kind: string; noteID: string } }[],
  open: vi.fn(() => "win-1"),
  hold: vi.fn(),
  scheduleClose: vi.fn(),
}));
const navigate = vi.hoisted(() => vi.fn());
const graphQuery = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("../queries", () => ({ useGraphQuery: graphQuery }));
vi.mock("../vaultScope", () => ({ useVaultScope: () => ({ scope: "" }) }));
vi.mock("./preview/floatingStore", () => ({ useFloating: () => floating }));

describe("GraphFullView lazy graph canvas", () => {
  let intersectionCallback: ((entries: { isIntersecting: boolean }[]) => void) | undefined;

  beforeEach(() => {
    intersectionCallback = undefined;
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(callback: (entries: { isIntersecting: boolean }[]) => void) {
          intersectionCallback = callback;
        }
        observe() {}
        disconnect() {}
      },
    );
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
      clearRect: vi.fn(),
      save: vi.fn(),
      translate: vi.fn(),
      scale: vi.fn(),
      beginPath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      stroke: vi.fn(),
      arc: vi.fn(),
      fill: vi.fn(),
      restore: vi.fn(),
      setTransform: vi.fn(),
      measureText: vi.fn(() => ({ width: 40 })),
      fillText: vi.fn(),
      rect: vi.fn(),
    } as unknown as CanvasRenderingContext2D);
    graphQuery.mockReset();
    graphQuery.mockReturnValue({
      isPending: false,
      data: {
        graph: {
          center_id: "",
          nodes: [
            { note_id: "1fBExGozUeXAXNB324DsDk", file_kind: "note", title: "Babel" },
            { note_id: "5bsKWpzrLcdY0SkLtwsCr9", file_kind: "note", title: "Foreign" },
          ],
          edges: [{ source_id: "1fBExGozUeXAXNB324DsDk", target_id: "5bsKWpzrLcdY0SkLtwsCr9" }],
        },
      },
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("mounts the whole-vault graph canvas directly inside the sized host once visible", async () => {
    const { container } = render(<GraphFullView />);
    const host = container.querySelector(".graph-full .graph-canvas-host") as HTMLElement;
    expect(host).not.toBeNull();
    // Waiting for the viewport: the host reserves the space, no canvas yet.
    expect(container.querySelector(".graph-full canvas")).toBeNull();

    act(() => {
      intersectionCallback?.([{ isIntersecting: true }]);
    });

    await waitFor(() => {
      expect(container.querySelector(".graph-full canvas.graph-canvas")).not.toBeNull();
    });
    const canvas = container.querySelector(".graph-full canvas.graph-canvas") as HTMLCanvasElement;
    // The canvas is the host's direct flex child: a bare wrapper between them is what collapsed the
    // published graph to 1x1 and left the page showing an empty graph box.
    expect(canvas.parentElement).toBe(host);
    expect(container.querySelector(".graph-scope")?.textContent).toContain("notes");
  });
});