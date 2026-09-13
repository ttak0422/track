import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FloatingWindow } from "./FloatingWindow";

describe("FloatingWindow resize handles", () => {
  it("renders a resize target on every edge and corner", () => {
    const { container } = render(
      <FloatingWindow
        title="Preview"
        initialBounds={{ left: 100, top: 100, width: 400, height: 300 }}
        pinned={false}
        stackOrder={0}
        onActivate={vi.fn()}
        onClose={vi.fn()}
        onPinToggle={vi.fn()}
      >
        <p>Preview body</p>
      </FloatingWindow>,
    );

    const handles = ["nw", "ne", "sw", "se", "w", "e", "n", "s"];
    expect(container.querySelectorAll(".wiki-preview-resize")).toHaveLength(handles.length);
    for (const handle of handles) {
      expect(container.querySelector(`.wiki-preview-resize-${handle}`)).not.toBeNull();
    }
  });
});

describe("FloatingWindow swap button", () => {
  const base = {
    title: "Preview",
    initialBounds: { left: 100, top: 100, width: 400, height: 300 },
    pinned: false,
    stackOrder: 0,
    onActivate: vi.fn(),
    onClose: vi.fn(),
    onPinToggle: vi.fn(),
  };

  it("renders an accessible swap button only when onSwap is provided, and fires it", () => {
    const onSwap = vi.fn();
    const { container, rerender } = render(
      <FloatingWindow {...base} onSwap={onSwap}>
        <p>Preview body</p>
      </FloatingWindow>,
    );

    const swap = container.querySelector<HTMLButtonElement>(".wiki-preview-swap")!;
    expect(swap).not.toBeNull();
    expect(swap).toHaveAttribute("aria-label", "Swap with the current note");
    expect(swap).toHaveAttribute("title", "Swap with page");
    fireEvent.click(swap);
    expect(onSwap).toHaveBeenCalledTimes(1);

    // Without a page note to trade with (search, graph, voice) the layer omits onSwap and the chrome
    // shows no swap button at all.
    rerender(
      <FloatingWindow {...base}>
        <p>Preview body</p>
      </FloatingWindow>,
    );
    expect(container.querySelector(".wiki-preview-swap")).toBeNull();
  });

  it("marks the window with the swap class so the chrome reserves room for the button", () => {
    const { container, rerender } = render(
      <FloatingWindow {...base} onSwap={vi.fn()}>
        <p>Preview body</p>
      </FloatingWindow>,
    );
    expect(container.querySelector(".wiki-preview")).toHaveClass("with-swap");
    expect(container.querySelector(".wiki-preview")).not.toHaveClass("with-jump");

    rerender(
      <FloatingWindow {...base} onJump={vi.fn()}>
        <p>Preview body</p>
      </FloatingWindow>,
    );
    expect(container.querySelector(".wiki-preview")).toHaveClass("with-jump");
    expect(container.querySelector(".wiki-preview")).not.toHaveClass("with-swap");
  });
});
