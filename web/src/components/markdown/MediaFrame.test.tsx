import { act, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FloatingProvider } from "../preview/floatingStore";
import { previewOpenDelay } from "../preview/stack";
import { MediaFrame } from "./MediaFrame";

// FloatingProvider reads the current route (to drop unpinned windows on navigation), so stub the router
// the same way WikiLink.test.tsx does.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
}));

// Render MediaFrame in isolation, same shape as WikiLink.test.tsx: it exercises the hover-intent
// open/close logic that mirrors WikiLink's, applied to a media embed instead of a note link.
function renderWithFloating(src: string, alt: string) {
  return render(
    <FloatingProvider>
      <MediaFrame src={src} alt={alt}>
        <img src={src} alt={alt} />
      </MediaFrame>
    </FloatingProvider>,
  );
}

function preview(container: HTMLElement) {
  return container.querySelector(".wiki-preview");
}

describe("MediaFrame enlarge lightbox", () => {
  it("opens an in-window modal with the media and closes on backdrop click", () => {
    const { container, getByLabelText } = renderWithFloating("assets/photo.png", "A photo");

    fireEvent.click(getByLabelText("Enlarge"));
    const dialog = container.querySelector("dialog.media-lightbox");
    expect(dialog).not.toBeNull();
    expect(dialog?.querySelector("img")?.getAttribute("src")).toBe("assets/photo.png");

    // A backdrop click lands on the dialog element itself and closes (unmounts) the lightbox.
    fireEvent.click(dialog!);
    expect(container.querySelector("dialog.media-lightbox")).toBeNull();
  });
});

describe("MediaFrame hover preview", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("opens the preview only after the pointer rests past the open delay", async () => {
    const { container } = renderWithFloating("assets/photo.png", "A photo");
    const frame = container.querySelector(".media-frame")!;

    fireEvent.mouseEnter(frame);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay - 50);
    });
    expect(preview(container)).toBeNull();

    await act(async () => {
      vi.advanceTimersByTime(60);
    });
    expect(preview(container)).not.toBeNull();
  });

  it("does not open when the pointer leaves before the delay elapses", async () => {
    const { container } = renderWithFloating("assets/photo.png", "A photo");
    const frame = container.querySelector(".media-frame")!;

    fireEvent.mouseEnter(frame);
    await act(async () => {
      vi.advanceTimersByTime(100);
    });
    fireEvent.mouseLeave(frame);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 300);
    });

    expect(preview(container)).toBeNull();
  });

  it("drops the hover preview when the media is enlarged", async () => {
    const { container, getByLabelText } = renderWithFloating("assets/photo.png", "A photo");
    const frame = container.querySelector(".media-frame")!;

    fireEvent.mouseEnter(frame);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 10);
    });
    expect(preview(container)).not.toBeNull();

    fireEvent.click(getByLabelText("Enlarge"));
    expect(preview(container)).toBeNull();
    expect(container.querySelector("dialog.media-lightbox")).not.toBeNull();
  });

  it("closes shortly after the pointer leaves the frame", async () => {
    const { container } = renderWithFloating("assets/photo.png", "A photo");
    const frame = container.querySelector(".media-frame")!;

    fireEvent.mouseEnter(frame);
    await act(async () => {
      vi.advanceTimersByTime(previewOpenDelay + 10);
    });
    expect(preview(container)).not.toBeNull();

    fireEvent.mouseLeave(frame);
    await act(async () => {
      vi.advanceTimersByTime(300);
    });
    expect(preview(container)).toBeNull();
  });
});
