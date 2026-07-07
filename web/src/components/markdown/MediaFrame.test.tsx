import { act, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FloatingProvider } from "../preview/floatingStore";
import { MediaFrame } from "./MediaFrame";

// FloatingProvider reads the current route (to drop unpinned windows on navigation), so stub the router
// the same way WikiLink.test.tsx does.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
}));

// Render MediaFrame in isolation, same shape as WikiLink.test.tsx: it exercises the button-driven
// preview popup (media never previews on hover — the embed is already visible in the note).
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

describe("MediaFrame preview popup", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("never opens from hover alone", async () => {
    const { container } = renderWithFloating("assets/photo.png", "A photo");
    const frame = container.querySelector(".media-frame")!;

    fireEvent.mouseEnter(frame);
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(preview(container)).toBeNull();
  });

  it("opens from the preview button and stays when the pointer leaves", async () => {
    const { container, getByLabelText } = renderWithFloating("assets/photo.png", "A photo");
    const frame = container.querySelector(".media-frame")!;

    fireEvent.click(getByLabelText("Preview"));
    expect(preview(container)).not.toBeNull();

    fireEvent.mouseLeave(frame);
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(preview(container)).not.toBeNull();

    fireEvent.click(getByLabelText("Close preview"));
    expect(preview(container)).toBeNull();
  });

  it("drops the preview popup when the media is enlarged", async () => {
    const { container, getByLabelText } = renderWithFloating("assets/photo.png", "A photo");

    fireEvent.click(getByLabelText("Preview"));
    expect(preview(container)).not.toBeNull();

    fireEvent.click(getByLabelText("Enlarge"));
    expect(preview(container)).toBeNull();
    expect(container.querySelector("dialog.media-lightbox")).not.toBeNull();
  });
});
