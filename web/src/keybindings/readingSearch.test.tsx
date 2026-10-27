import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider, useRouterState } from "@tanstack/react-router";
import { useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SidebarSearch } from "../components/SidebarSearch";
import { FloatingProvider } from "../components/preview/floatingStore";
import { TabsProvider } from "../components/tabs/tabsStore";
import { SearchProvider } from "../searchState";
import { useReadingKeybindings } from "./useReadingKeybindings";

vi.mock("../queries", () => ({ useSearchQuery: () => ({ data: { results: [
  { note_id: "a", title: "Alpha", match: "title" },
  { note_id: "b", title: "Beta", match: "title" },
], unavailable: [] } }) }));
vi.mock("../components/GraphCanvasLazy", () => ({ GraphCanvas: () => null }));
vi.mock("../components/preview/WikiLink", () => ({ WikiLink: () => null }));

function Workspace() {
  const ref = useRef<HTMLElement>(null);
  const path = useRouterState({ select: (state) => state.location.pathname });
  const enabled = useReadingKeybindings(ref, path.startsWith("/notes/"));
  return <SearchProvider><FloatingProvider>
    <SidebarSearch /><input aria-label="Other input" />
    <section ref={ref} data-testid="reader" tabIndex={enabled ? 0 : undefined}><Outlet /></section>
  </FloatingProvider></SearchProvider>;
}
async function setup(path = "/notes/a") {
  const root = createRootRoute({ component: () => <TabsProvider><Workspace /></TabsProvider> });
  const note = createRoute({ getParentRoute: () => root, path: "/notes/$noteId", component: () => <div className="note-preview">Prose</div> });
  const home = createRoute({ getParentRoute: () => root, path: "/", component: () => <p>Home</p> });
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createRouter({ routeTree: root.addChildren([note, home]), history });
  render(<RouterProvider router={router} />);
  const reader = await screen.findByTestId("reader");
  const scrollBy = vi.fn();
  reader.scrollBy = scrollBy;
  return { reader, scrollBy, history };
}
function press(target: Element, key: string) {
  fireEvent.keyDown(target, { key });
}
async function search() {
  press(document.activeElement!, "/");
  const field = screen.getByRole("searchbox");
  expect(field).toHaveFocus();
  fireEvent.change(field, { target: { value: "a" } });
  await screen.findByRole("link", { name: /^Alpha/ });
  return field;
}
beforeEach(() => {
  window.__trackNativeReading = true;
  window.getSelection()?.removeAllRanges();
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
});
afterEach(() => { delete window.__trackNativeReading; localStorage.clear(); vi.restoreAllMocks(); });

describe("reading focus after workspace search", () => {
  it("restores j/k after repeated Escape without consuming Escape as a reading release", async () => {
    const { reader, scrollBy } = await setup();
    fireEvent.click(screen.getByText("Prose"));
    for (let i = 0; i < 2; i++) {
      const field = await search();
      press(field, "j");
      press(field, "k");
      expect(scrollBy).not.toHaveBeenCalled();
      press(field, "Escape");
      expect(reader).toHaveFocus();
    }
    press(reader, "j");
    press(reader, "k");
    expect(scrollBy.mock.calls).toEqual([[{ top: 72, behavior: "instant" }], [{ top: -72, behavior: "instant" }]]);
    press(reader, "Escape");
    press(reader, "j");
    expect(scrollBy).toHaveBeenCalledTimes(2);
  });
  it.each(["/notes/a", "/"])("restores focus after result navigation from %s", async (path) => {
    const { reader, scrollBy, history } = await setup(path);
    if (path !== "/") fireEvent.click(screen.getByText("Prose"));
    const field = await search();
    press(field, "ArrowDown");
    expect(field).toHaveAttribute("aria-activedescendant", "search-result-b");
    press(field, "Enter");
    await waitFor(() => expect(history.location.pathname).toBe("/notes/b"));
    expect(reader).toHaveFocus();
    press(reader, "j");
    expect(scrollBy).toHaveBeenCalledOnce();
    press(reader, "Tab");
    if (path !== "/") await waitFor(() => expect(history.location.pathname).toBe("/notes/a"));
    const again = await search();
    press(again, "Escape");
    expect(reader).toHaveFocus();
  });
  it("restores after a clicked result or backdrop but respects another input", async () => {
    const { reader, scrollBy } = await setup();
    fireEvent.click(screen.getByText("Prose"));
    await search();
    fireEvent.click(screen.getByRole("link", { name: /^Beta/ }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(reader).toHaveFocus();
    await search();
    fireEvent.mouseDown(document.querySelector(".search-backdrop")!);
    expect(reader).toHaveFocus();
    await search();
    const input = screen.getByLabelText("Other input");
    act(() => { fireEvent.mouseDown(input); input.focus(); });
    expect(input).toHaveFocus();
    press(input, "j");
    expect(scrollBy).not.toHaveBeenCalled();
  });
  it.each(["input", "textarea", "contenteditable"])("returns to the original %s on dismissal but to the reader after choosing a result", async (kind) => {
    const { reader, scrollBy, history } = await setup();
    const input = kind === "input" ? screen.getByLabelText("Other input") : document.createElement(kind === "textarea" ? "textarea" : "div");
    if (kind !== "input") {
      if (kind === "contenteditable") {
        input.setAttribute("contenteditable", "true");
        input.tabIndex = 0;
        Object.defineProperty(input, "isContentEditable", { value: true }); // jsdom lacks this browser property.
      }
      document.body.append(input);
    }
    input.focus();
    fireEvent.keyDown(input, { key: "p", metaKey: true });
    press(screen.getByRole("searchbox"), "Escape");
    expect(input).toHaveFocus();
    press(input, "j");
    press(input, "k");
    expect(scrollBy).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "p", metaKey: true });
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "a" } });
    fireEvent.click(await screen.findByRole("link", { name: /^Beta/ }));
    await waitFor(() => expect(history.location.pathname).toBe("/notes/b"));
    expect(reader).toHaveFocus();
    if (kind !== "input") input.remove();
  });
  it("leaves browser focus behavior unchanged", async () => {
    delete window.__trackNativeReading;
    const { reader, scrollBy } = await setup();
    const field = await search();
    press(field, "Escape");
    expect(reader).not.toHaveFocus();
    press(document.body, "j");
    expect(scrollBy).not.toHaveBeenCalled();
  });
});
