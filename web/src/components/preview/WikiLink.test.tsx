import { act, fireEvent, render } from "@testing-library/react";
import type { ReactElement, ReactNode, Ref } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FloatingLayer } from "./FloatingLayer";
import { FloatingProvider } from "./floatingStore";
import { previewCloseDelay, previewOpenDelay } from "./stack";
import { WikiLink } from "./WikiLink";

// The page and the floating layer are siblings, as they are in Shell: a preview never renders inside
// the link that opened it, so the tests can tell the two apart.
function renderWithFloating(ui: ReactElement) {
  return render(
    <FloatingProvider>
      <div data-testid="page">{ui}</div>
      <FloatingLayer />
    </FloatingProvider>,
  );
}

// Render WikiLink in isolation: stub the router Link to a plain anchor (forwarding ref, which WikiLink
// needs to anchor the preview) and the data hooks to a resolved note, so the test exercises only the
// hover-intent open/close logic.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
  useNavigate: () => vi.fn(),
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

const copyText = vi.hoisted(() => vi.fn());
vi.mock("../markdown/clipboard", () => ({ copyText }));

function preview(container: HTMLElement) {
  return container.querySelector(".wiki-preview");
}

describe("WikiLink hover intent", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    HTMLElement.prototype.setPointerCapture = vi.fn();
    HTMLElement.prototype.releasePointerCapture = vi.fn();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

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

  // A touch screen has no resting on a link: the tap that would open the preview is the tap that
  // follows the link, and it focuses the link besides — which is the preview's other way in.
  it("opens no preview on a pointer that cannot hover", async () => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn((query: string) => ({ matches: query === "(hover: none)" })),
    );
    const { container } = renderWithFloating(<WikiLink target="Target" display="Target" />);
    const wrap = container.querySelector(".wiki-link-wrap")!;

    fireEvent.mouseEnter(wrap);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 300);
    });
    expect(preview(container)).toBeNull();

    fireEvent.focus(wrap);
    expect(preview(container)).toBeNull();
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

  it("copies the note title from the chrome copy button", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
    const { container } = renderWithFloating(<WikiLink target="Target" display="Target" />);
    const wrap = container.querySelector(".wiki-link-wrap")!;

    fireEvent.mouseEnter(wrap);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 10);
    });

    const copy = container.querySelector<HTMLButtonElement>(".wiki-preview-copy")!;
    expect(copy).not.toBeNull();
    fireEvent.click(copy);
    await act(async () => {});
    expect(copyText).toHaveBeenCalledWith("Target");
    expect(copy).toHaveAttribute("aria-label", "Title copied");
  });

  it("closes a window the pointer left without dragging", async () => {
    const { container } = renderWithFloating(<WikiLink target="Target" display="Target" />);
    const wrap = container.querySelector(".wiki-link-wrap")!;

    fireEvent.mouseEnter(wrap);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 10);
    });
    expect(preview(container)).not.toBeNull();

    fireEvent.mouseLeave(wrap);
    await act(async () => {
      vi.advanceTimersByTime(previewCloseDelay + 10);
    });
    expect(preview(container)).toBeNull();
  });

  // The window belongs to the layer, not to the link. Two consequences the popup model rests on: it
  // is not inside the link's own stacking context, so anything opened later can come in front of it;
  // and closing the preview a link sits in does not take the preview that link opened with it.
  it("puts the window in the layer rather than inside the link", async () => {
    const { container, getByTestId } = renderWithFloating(
      <WikiLink target="Target" display="Target" />,
    );
    fireEvent.mouseEnter(container.querySelector(".wiki-link-wrap")!);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 10);
    });

    expect(preview(container)).not.toBeNull();
    expect(getByTestId("page").querySelector(".wiki-preview")).toBeNull();
  });

  it("leaves the window standing when the link that opened it goes away", async () => {
    function Page({ show }: { show: boolean }) {
      return (
        <FloatingProvider>
          <div data-testid="page">{show ? <WikiLink target="Target" display="Target" /> : null}</div>
          <FloatingLayer />
        </FloatingProvider>
      );
    }
    const { container, rerender } = render(<Page show />);
    fireEvent.mouseEnter(container.querySelector(".wiki-link-wrap")!);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 10);
    });
    expect(preview(container)).not.toBeNull();

    // The preview this link was rendered in is closed: the link is gone, the window it opened is not.
    rerender(<Page show={false} />);
    await act(async () => {
      vi.advanceTimersByTime(previewCloseDelay + 300);
    });
    expect(preview(container)).not.toBeNull();
  });
});
