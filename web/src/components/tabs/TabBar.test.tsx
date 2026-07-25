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

function renderStrip() {
  return render(
    <FloatingProvider>
      <TabsProvider>
        <TabBar />
        <FloatingCount />
      </TabsProvider>
    </FloatingProvider>,
  );
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

  it("offers no float button on a view tab, which has no note to float", () => {
    routerMock.pathname = "/graph";
    renderStrip();
    expect(screen.getByRole("listitem")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Float this note" })).not.toBeInTheDocument();
  });
});
