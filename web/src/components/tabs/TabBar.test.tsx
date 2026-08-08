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

  it("floats the open note from the button under its tab", () => {
    renderStrip();
    const float = screen.getByRole("button", { name: "Float this note" });
    // The button belongs to the tab — in the panel that hangs under it, not inline over the title.
    expect(screen.getByRole("listitem")).toContainElement(float);
    expect(float.closest(".tab-tools")).not.toBeNull();
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

  it("opens a tab when its title is clicked", () => {
    renderStrip();
    routerMock.navigate.mockClear();
    fireEvent.click(document.querySelector("button.tab-label")!);
    expect(routerMock.navigate).toHaveBeenCalled();
  });

  it("shows every tab that fits and sends the rest to the overflow menu", () => {
    const view = renderStrip();
    for (const id of ["b2", "c3", "d4", "e5", "f6"]) {
      routerMock.pathname = `/notes/${id}`;
      view.rerender(strip());
    }
    // jsdom has no layout, so stand in for one: a 400px strip and 150px tabs, which is room for two.
    const bar = screen.getByRole("list");
    Object.defineProperty(bar, "clientWidth", { configurable: true, get: () => 400 });
    Object.defineProperty(bar, "scrollWidth", {
      configurable: true,
      get: () => bar.querySelectorAll(".tab").length * 150,
    });
    view.rerender(strip());

    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    // Most recent first, so the note being read holds the first slot and is never in the menu.
    expect(screen.getAllByRole("listitem")[0]).toContainElement(
      screen.getByRole("button", { current: "page" }),
    );

    fireEvent.click(screen.getByRole("button", { name: "4 more open notes" }));
    expect(screen.getAllByRole("menuitem")).toHaveLength(4);

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
