import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode, Ref } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GraphFullView } from "./GraphFullView";
import { FloatingLayer } from "./preview/FloatingLayer";
import { FloatingProvider } from "./preview/floatingStore";
import { WikiLink } from "./preview/WikiLink";
import { previewCloseDelay, previewOpenDelay } from "./preview/stack";

const router = vi.hoisted(() => ({ pathname: "/graph", navigate: vi.fn() }));
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => router.pathname,
  useNavigate: () => router.navigate,
  Link: ({ children, ref, className }: { children: ReactNode; ref?: Ref<HTMLAnchorElement>; className?: string }) =>
    <a ref={ref} className={className}>{children}</a>,
}));
vi.mock("../vaultScope", () => ({ useVaultScope: () => ({ scope: "" }) }));
vi.mock("../queries", () => ({
  useGraphQuery: () => ({ data: { graph: { nodes: [], edges: [] } } }),
  useResolveQuery: (term: string) => ({ data: { found: true, note: { note_id: term, title: term } } }),
  useNoteQuery: (id: string) => ({ data: { note: { title: id, body: "", file_kind: "note" } } }),
  useRenderQuery: () => ({ data: { markdown: "" } }),
}));
vi.mock("./MarkdownView", () => ({ MarkdownView: () => <WikiLink target="c" display="nested c" /> }));
// Only canvas hit-testing is replaced. The hover machine, store, chrome and nested links are real.
vi.mock("./GraphCanvasLazy", () => ({
  GraphCanvas: ({ onHover }: { onHover: (id: string | null, p: { x: number; y: number }) => void }) => <>
    {["a", "b", null].map((id) => <button key={String(id)} onClick={() => onHover(id, { x: 100, y: 100 })}>
      hover {id ?? "out"}
    </button>)}
  </>,
}));

function Fixture({ graph = true }: { graph?: boolean }) {
  return <FloatingProvider>
    {graph ? <GraphFullView /> : null}
    <WikiLink target="a" display="page link a" />
    <FloatingLayer />
  </FloatingProvider>;
}
function tick(ms: number) { act(() => vi.advanceTimersByTime(ms)); }
function hover(id: string) { fireEvent.click(screen.getByText(`hover ${id}`)); }
function open(id: string) { hover(id); tick(previewOpenDelay); }
function popup(id: string): HTMLElement | null {
  return [...document.querySelectorAll<HTMLElement>(".wiki-preview")]
    .find((win) => win.querySelector(".wiki-preview-title-text")?.textContent === id) ?? null;
}
function drag(win: HTMLElement, resize = false) {
  const handle = win.querySelector(resize ? ".wiki-preview-resize-se" : ".wiki-preview-chrome")!;
  fireEvent.mouseEnter(win);
  fireEvent.pointerDown(handle, { clientX: 10, clientY: 10 });
  fireEvent.pointerMove(handle, { clientX: 50, clientY: 50 });
  fireEvent.pointerUp(handle, { clientX: 50, clientY: 50 });
}

describe("graph preview lifecycle through the floating layer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    router.pathname = "/graph";
    router.navigate.mockReset();
    vi.stubGlobal("PointerEvent", MouseEvent);
    HTMLElement.prototype.setPointerCapture = vi.fn();
    HTMLElement.prototype.releasePointerCapture = vi.fn();
  });
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

  it("closes A after hovering B directly, then closes B on leaving", () => {
    render(<Fixture />);
    open("a"); open("b");
    expect(popup("a")).toBeNull();
    expect(popup("b")).not.toBeNull();
    hover("out"); tick(previewCloseDelay);
    expect(document.querySelectorAll(".wiki-preview")).toHaveLength(0);
  });

  it("cancels B's pending open when returning to A", () => {
    render(<Fixture />);
    open("a"); hover("b"); tick(40); hover("a"); tick(previewOpenDelay);
    expect(popup("b")).toBeNull();
    expect(popup("a")).not.toBeNull();
    hover("out"); tick(previewCloseDelay);
    expect(popup("a")).toBeNull();
  });

  it.each([false, true])("keeps graph previews transient after a popup drag (resize=%s)", (resize) => {
    render(<Fixture />);
    open("a"); hover("out");
    const win = popup("a")!;
    drag(win, resize);
    fireEvent.focus(win.querySelector("button")!);
    fireEvent.mouseLeave(win); tick(previewCloseDelay);
    expect(popup("a")).toBeNull();
  });

  it("holds on pointer entry, closes on exit, and opens again repeatedly", () => {
    render(<Fixture />);
    for (let i = 0; i < 3; i++) {
      open("a"); hover("out");
      fireEvent.mouseEnter(popup("a")!); tick(previewCloseDelay * 2);
      expect(popup("a")).not.toBeNull();
      fireEvent.mouseLeave(popup("a")!); tick(previewCloseDelay);
      expect(popup("a")).toBeNull();
    }
  });

  it("does not settle a graph popup on focus handoff", () => {
    render(<Fixture />);
    open("a"); hover("out");
    fireEvent.focus(popup("a")!.querySelector("button")!);
    fireEvent.blur(popup("a")!.querySelector("button")!); tick(previewCloseDelay);
    expect(popup("a")).toBeNull();
  });

  it("leaves nested note links independent without keeping the graph parent", () => {
    render(<Fixture />);
    open("a"); hover("out");
    const parent = popup("a")!;
    fireEvent.mouseEnter(parent);
    const link = parent.querySelector(".wiki-link-wrap")!;
    fireEvent.mouseEnter(link); tick(previewOpenDelay);
    fireEvent.mouseLeave(link); fireEvent.mouseLeave(parent);
    fireEvent.mouseEnter(popup("c")!); tick(previewCloseDelay);
    expect(popup("a")).toBeNull();
    expect(popup("c")).not.toBeNull();
    fireEvent.mouseLeave(popup("c")!); tick(previewCloseDelay);
    expect(popup("c")).toBeNull();
  });

  it("preserves the ordinary note link's drag-to-keep behavior", () => {
    render(<Fixture />);
    const link = screen.getByText("page link a").parentElement!;
    fireEvent.mouseEnter(link); tick(previewOpenDelay); fireEvent.mouseLeave(link);
    drag(popup("a")!);
    fireEvent.mouseLeave(popup("a")!); tick(previewCloseDelay);
    expect(popup("a")).not.toBeNull();
    open("b"); hover("out"); tick(previewCloseDelay);
    expect(popup("a")).not.toBeNull(); expect(popup("b")).toBeNull();
  });

  it("holds during pointer capture and closes after release outside", () => {
    render(<Fixture />);
    open("a"); hover("out");
    const win = popup("a")!;
    fireEvent.mouseEnter(win);
    const handle = win.querySelector(".wiki-preview-chrome")!;
    fireEvent.pointerDown(handle, { clientX: 10, clientY: 10 });
    fireEvent.pointerMove(handle, { clientX: 50, clientY: 50 });
    fireEvent.mouseLeave(win); tick(previewCloseDelay * 2);
    expect(popup("a")).not.toBeNull();
    fireEvent.pointerUp(handle, { clientX: -10, clientY: -10 }); tick(previewCloseDelay);
    expect(popup("a")).toBeNull();
  });

  it("releases a graph popup when pointer capture is interrupted", () => {
    render(<Fixture />); open("a"); hover("out");
    const win = popup("a")!;
    const handle = win.querySelector(".wiki-preview-chrome")!;
    fireEvent.mouseEnter(win);
    fireEvent.pointerDown(handle, { clientX: 10, clientY: 10 });
    fireEvent.pointerMove(handle, { clientX: 50, clientY: 50 });
    fireEvent.lostPointerCapture(handle); tick(previewCloseDelay);
    expect(popup("a")).toBeNull();
  });

  it.each([0, previewOpenDelay])("reset cancels pending and open previews (elapsed=%s)", (elapsed) => {
    render(<Fixture />); hover("a"); tick(elapsed);
    fireEvent.click(screen.getByRole("button", { name: "Reset graph view" }));
    tick(previewOpenDelay + previewCloseDelay);
    expect(popup("a")).toBeNull();
  });

  it("returns an explicitly unpinned graph preview to its hover lifetime", () => {
    render(<Fixture />); open("a");
    const win = popup("a")!;
    fireEvent.click(win.querySelector('[aria-label="Pin preview"]')!);
    fireEvent.click(win.querySelector('[aria-label="Unpin preview"]')!);
    hover("out"); fireEvent.mouseLeave(win); tick(previewCloseDelay);
    expect(popup("a")).toBeNull();
  });

  it("keeps explicit pins across navigation and supports explicit open as page", () => {
    const view = render(<Fixture />);
    open("a");
    fireEvent.click(popup("a")!.querySelector('[aria-label="Pin preview"]')!);
    hover("out"); tick(previewCloseDelay);
    router.pathname = "/notes/b"; view.rerender(<Fixture />);
    expect(popup("a")).not.toBeNull();
    fireEvent.click(popup("a")!.querySelector('[aria-label="Open as page"]')!);
    expect(router.navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "a" } });
    fireEvent.click(popup("a")!.querySelector('[aria-label="Close preview"]')!);
    expect(popup("a")).toBeNull();
  });

  it("closes unpinned previews on navigation and on graph unmount", () => {
    const view = render(<Fixture />);
    open("a"); view.rerender(<Fixture graph={false} />); tick(previewCloseDelay);
    expect(popup("a")).toBeNull();
    view.rerender(<Fixture />); open("b");
    router.pathname = "/notes/a"; view.rerender(<Fixture />);
    expect(popup("b")).toBeNull();
  });
});
