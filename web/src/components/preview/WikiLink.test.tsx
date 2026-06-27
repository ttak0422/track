import { act, fireEvent, render } from "@testing-library/react";
import type { ReactElement, ReactNode, Ref } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FloatingProvider } from "./floatingStore";
import { previewOpenDelay } from "./stack";
import { WikiLink } from "./WikiLink";

function renderWithFloating(ui: ReactElement) {
  return render(<FloatingProvider>{ui}</FloatingProvider>);
}

// Render WikiLink in isolation: stub the router Link to a plain anchor (forwarding ref, which WikiLink
// needs to anchor the preview) and the data hooks to a resolved note, so the test exercises only the
// hover-intent open/close logic.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
  Link: ({
    children,
    className,
    ref,
  }: {
    children: ReactNode;
    className?: string;
    ref?: Ref<HTMLAnchorElement>;
  }) => (
    <a className={className} ref={ref}>
      {children}
    </a>
  ),
}));

vi.mock("../../queries", () => ({
  useResolveQuery: (term: string) => ({
    data: term ? { found: true, note: { note_id: 1 } } : undefined,
    isPending: false,
  }),
  useNoteQuery: () => ({
    data: { note: { title: "Target", body: "", file_kind: "note" } },
    isPending: false,
    isError: false,
  }),
  useRenderQuery: () => ({ data: { markdown: "" } }),
}));

function preview(container: HTMLElement) {
  return container.querySelector(".wiki-preview");
}

describe("WikiLink hover intent", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    HTMLElement.prototype.setPointerCapture = vi.fn();
    HTMLElement.prototype.releasePointerCapture = vi.fn();
  });
  afterEach(() => vi.useRealTimers());

  it("opens the preview only after the pointer rests past the open delay", async () => {
    const { container } = renderWithFloating(<WikiLink target="Target" display="Target" />);
    const wrap = container.querySelector(".wiki-link-wrap")!;

    fireEvent.mouseEnter(wrap);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay - 50);
    });
    expect(preview(container)).toBeNull(); // still within the intent delay

    await act(async () => {
      vi.advanceTimersByTime(60);
    });
    expect(preview(container)).not.toBeNull();
  });

  it("does not open when the pointer leaves before the delay elapses", async () => {
    const { container } = renderWithFloating(<WikiLink target="Target" display="Target" />);
    const wrap = container.querySelector(".wiki-link-wrap")!;

    fireEvent.mouseEnter(wrap);
    await act(async () => {
      vi.advanceTimersByTime(100);
    });
    fireEvent.mouseLeave(wrap);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 300);
    });

    expect(preview(container)).toBeNull();
  });

  it("keeps a dragged preview open until the user closes it", async () => {
    const { container } = renderWithFloating(<WikiLink target="Target" display="Target" />);
    const wrap = container.querySelector(".wiki-link-wrap")!;

    fireEvent.mouseEnter(wrap);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 10);
    });
    expect(preview(container)).not.toBeNull();

    const chrome = container.querySelector(".wiki-preview-chrome")!;
    fireEvent.pointerDown(chrome, { pointerId: 1, clientX: 100, clientY: 100 });
    fireEvent.pointerMove(chrome, { pointerId: 1, clientX: 120, clientY: 100 });
    fireEvent.mouseLeave(wrap);
    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    expect(preview(container)).not.toBeNull();

    fireEvent.click(container.querySelector(".wiki-preview-close")!);
    expect(preview(container)).toBeNull();
  });
});
