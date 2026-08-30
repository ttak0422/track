import { render } from "@testing-library/react";
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
