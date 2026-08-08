import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { FloatingProvider, useFloating } from "../preview/floatingStore";
import { TabBar } from "./TabBar";
import { TabsProvider } from "./tabsStore";

// The strip reads the route to know which tab is open; the providers need nothing else from the router.
const routerMock = vi.hoisted(() => ({ pathname: "/notes/a1", navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => routerMock.pathname,
  useNavigate: () => routerMock.navigate,
}));

// Reports how many windows the floating layer holds, without rendering the layer itself.
function FloatingCount() {
  const { windows } = useFloating();
  return <output data-testid="floating-count">{windows.length}</output>;
}

// A fresh element each call: rerendering an identical element reference lets React skip the
// subtree, which would hide route changes from the strip.
function strip() {
  return (
    <FloatingProvider>
      <TabsProvider>
        <TabBar />
        <FloatingCount />
      </TabsProvider>
    </FloatingProvider>
  );
}

function renderStrip() {
  return render(strip());
}

describe("TabBar", () => {
  beforeEach(() => {
    routerMock.pathname = "/notes/a1";
    routerMock.navigate.mockClear();
    window.localStorage.clear();
  });

  it("floats the open note from the button in its tab", () => {
    renderStrip();
    const float = screen.getByRole("button", { name: "Float this note" });
    // The button belongs to the tab itself; it used to live in a popup hanging under the strip.
    expect(screen.getByRole("listitem")).toContainElement(float);
    expect(screen.getByTestId("floating-count")).toHaveTextContent("0");
    fireEvent.click(float);
    expect(screen.getByTestId("floating-count")).toHaveTextContent("1");
  });

  it("keeps the float button on every note tab, not just the active one", () => {
    const view = renderStrip();
    routerMock.pathname = "/notes/b2";
    view.rerender(strip());
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getAllByRole("button", { name: "Float this note" })).toHaveLength(2);
  });

  it("pans the strip on drag, and does not open the tab the drag ended on", () => {
    // jsdom implements neither pointer capture nor layout; stub the one and treat scrollLeft as the
    // plain property it is.
    Element.prototype.setPointerCapture = () => {};
    Element.prototype.releasePointerCapture = () => {};
    const view = renderStrip();
    routerMock.pathname = "/notes/b2";
    view.rerender(strip());
    const bar = screen.getByRole("list");
    // jsdom has no layout, so scrollLeft is just a property — enough to prove the handler math.
    Object.defineProperty(bar, "scrollWidth", { value: 800, configurable: true });
    bar.scrollLeft = 100;

    fireEvent.pointerDown(bar, { pointerType: "mouse", button: 0, pointerId: 1, clientX: 200 });
    fireEvent.pointerMove(bar, { pointerType: "mouse", pointerId: 1, clientX: 140 });
    expect(bar.scrollLeft).toBe(160); // dragged left by 60, so the strip scrolled right by 60
    fireEvent.pointerUp(bar, { pointerType: "mouse", pointerId: 1, clientX: 140 });

    // The click that ends a pan is not a request to open anything.
    routerMock.navigate.mockClear();
    fireEvent.click(screen.getAllByRole("button", { name: /Close/ })[0].parentElement!.querySelector("button.tab-label")!);
    expect(routerMock.navigate).not.toHaveBeenCalled();
    // …but the next one is.
    fireEvent.click(screen.getAllByRole("button", { name: /Close/ })[0].parentElement!.querySelector("button.tab-label")!);
    expect(routerMock.navigate).toHaveBeenCalled();
  });

  it("does not swallow a later keyboard activation when the pan's click never reached a tab", () => {
    // What a real browser does: the pan holds pointer capture, so the click that ends it is
    // retargeted to the strip and no tab handler ever sees it. The suppression has to be consumed
    // there, or it sits armed and swallows the next activation that arrives without a pointer —
    // opening a focused tab with Enter.
    Element.prototype.setPointerCapture = () => {};
    Element.prototype.releasePointerCapture = () => {};
    renderStrip();
    const bar = screen.getByRole("list");
    fireEvent.pointerDown(bar, { pointerType: "mouse", button: 0, pointerId: 1, clientX: 200 });
    fireEvent.pointerMove(bar, { pointerType: "mouse", pointerId: 1, clientX: 140 });
    fireEvent.pointerUp(bar, { pointerType: "mouse", pointerId: 1, clientX: 140 });
    fireEvent.click(bar);

    routerMock.navigate.mockClear();
    fireEvent.click(document.querySelector("button.tab-label")!);
    expect(routerMock.navigate).toHaveBeenCalled();
  });

  it("does not close a tab with the click that ends a pan either", () => {
    Element.prototype.setPointerCapture = () => {};
    Element.prototype.releasePointerCapture = () => {};
    const view = renderStrip();
    routerMock.pathname = "/notes/b2";
    view.rerender(strip());
    const bar = screen.getByRole("list");
    fireEvent.pointerDown(bar, { pointerType: "mouse", button: 0, pointerId: 1, clientX: 200 });
    fireEvent.pointerMove(bar, { pointerType: "mouse", pointerId: 1, clientX: 140 });
    fireEvent.pointerUp(bar, { pointerType: "mouse", pointerId: 1, clientX: 140 });

    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    fireEvent.click(screen.getAllByRole("button", { name: /Close/ })[0]);
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
  });

  it("ignores a drag that never moved, so a plain click still opens its tab", () => {
    Element.prototype.setPointerCapture = () => {};
    Element.prototype.releasePointerCapture = () => {};
    renderStrip();
    const bar = screen.getByRole("list");
    fireEvent.pointerDown(bar, { pointerType: "mouse", button: 0, pointerId: 1, clientX: 200 });
    fireEvent.pointerMove(bar, { pointerType: "mouse", pointerId: 1, clientX: 198 }); // 2px of travel
    fireEvent.pointerUp(bar, { pointerType: "mouse", pointerId: 1, clientX: 198 });
    routerMock.navigate.mockClear();
    fireEvent.click(document.querySelector("button.tab-label")!);
    expect(routerMock.navigate).toHaveBeenCalled();
  });

  it("shows four tabs and sends the rest to the overflow menu, keeping the open note on the strip", () => {
    const view = renderStrip();
    for (const id of ["b2", "c3", "d4", "e5", "f6"]) {
      routerMock.pathname = `/notes/${id}`;
      view.rerender(strip());
    }

    // Six notes open, four on the strip — and the sixth is the one being read, so it takes the last
    // slot rather than hiding behind the button.
    const shown = screen.getAllByRole("listitem");
    expect(shown).toHaveLength(4);
    expect(shown[3]).toContainElement(screen.getByRole("button", { current: "page" }));
    fireEvent.click(screen.getByRole("button", { name: "2 more open notes" }));
    expect(screen.getAllByRole("menuitem")).toHaveLength(2);

    routerMock.navigate.mockClear();
    fireEvent.click(screen.getAllByRole("menuitem")[0]);
    expect(routerMock.navigate).toHaveBeenCalled();
    expect(screen.queryByRole("menuitem")).not.toBeInTheDocument(); // opening one closes the menu
  });

  it("offers no float button on a view tab, which has no note to float", () => {
    routerMock.pathname = "/graph";
    renderStrip();
    expect(screen.getByRole("listitem")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Float this note" })).not.toBeInTheDocument();
  });
});
