import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { FloatingLayer } from "./FloatingLayer";
import { FloatingProvider, useFloating } from "./floatingStore";

// The page and the floating layer are siblings, as they are in Shell. FloatingLayer reads the current
// route to know which note is the page (the swap target) and navigates on a swap, so the router is
// stubbed the same way WikiLink.test.tsx does.
const routerMock = vi.hoisted(() => ({ pathname: "/", navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => routerMock.pathname,
  useNavigate: () => routerMock.navigate,
}));

// NoteWindow fetches the note body and its sanitized render; both arrive instantly in these tests so
// the window draws its chrome right away.
vi.mock("../../queries", () => ({
  useNoteQuery: () => ({
    data: { note: { title: "T", body: "", file_kind: "note", copy_path: "" } },
    isPending: false,
    isError: false,
  }),
  useRenderQuery: () => ({ data: { markdown: "" } }),
}));
vi.mock("../markdown/clipboard", () => ({ copyText: vi.fn(() => Promise.resolve(true)) }));
// The note body itself is not under test here; a stub keeps the renderer out of the way.
vi.mock("../MarkdownView", () => ({
  MarkdownView: () => <div data-testid="body" />,
}));

const bounds = { left: 0, top: 0, width: 300, height: 200 };

// OpenButton opens a note window through the real store, like a search result's float button would;
// Probe reports the first window's note so a swap's content change is observable.
function OpenButton({ noteID }: { noteID: string }) {
  const floating = useFloating();
  return (
    <button type="button" onClick={() => floating.open({ kind: "note", noteID }, bounds, false, { pinned: true })}>
      open {noteID}
    </button>
  );
}

function Probe() {
  const floating = useFloating();
  const first = floating.windows[0];
  return <span data-testid="win">{first && first.content.kind === "note" ? first.content.noteID : "none"}</span>;
}

function renderLayer() {
  return render(
    <FloatingProvider>
      <OpenButton noteID="2" />
      <Probe />
      <FloatingLayer />
    </FloatingProvider>,
  );
}

describe("FloatingLayer note swap", () => {
  beforeEach(() => {
    routerMock.pathname = "/";
    routerMock.navigate.mockReset();
  });

  it("offers no swap button while the page shows no note", () => {
    routerMock.pathname = "/voice";
    const { container } = renderLayer();

    fireEvent.click(container.querySelector("button")!);
    expect(container.querySelector(".wiki-preview")).not.toBeNull();
    expect(container.querySelector(".wiki-preview-swap")).toBeNull();
  });

  it("offers no swap button when the window already shows the page note itself", () => {
    routerMock.pathname = "/notes/2";
    const { container } = renderLayer();

    fireEvent.click(container.querySelector("button")!);
    expect(container.querySelector(".wiki-preview-swap")).toBeNull();
  });

  it("swaps the page note with the window's note on click", () => {
    routerMock.pathname = "/notes/1";
    const { container } = renderLayer();

    fireEvent.click(container.querySelector("button")!);
    const swap = container.querySelector<HTMLButtonElement>(".wiki-preview-swap")!;
    expect(swap).not.toBeNull();
    fireEvent.click(swap);

    // The window's note becomes the page…
    expect(routerMock.navigate).toHaveBeenCalledWith({
      to: "/notes/$noteId",
      params: { noteId: "2" },
    });
    // …and the page's note takes the window's place in the same window.
    expect(container.querySelector('[data-testid="win"]')).toHaveTextContent("1");
    // The window now shows the note the page is on, so there is nothing left to swap with it.
    expect(container.querySelector(".wiki-preview-swap")).toBeNull();
  });

  it("keeps the window mounted under its own bounds across the swap", () => {
    routerMock.pathname = "/notes/1";
    const { container } = renderLayer();

    fireEvent.click(container.querySelector("button")!);
    const preview = container.querySelector<HTMLElement>(".wiki-preview")!;
    const style = preview.style;
    fireEvent.click(container.querySelector(".wiki-preview-swap")!);

    // One window only, still the same element: the swap changed content, not geometry or identity.
    expect(container.querySelectorAll(".wiki-preview")).toHaveLength(1);
    const swapped = container.querySelector<HTMLElement>(".wiki-preview")!;
    expect(swapped).toBe(preview);
    expect(swapped.style.left).toBe(style.left);
    expect(swapped.style.top).toBe(style.top);
  });
});