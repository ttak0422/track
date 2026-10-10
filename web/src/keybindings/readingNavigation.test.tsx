import { act, render, screen, waitFor } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider, useBlocker } from "@tanstack/react-router";
import { useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TabsProvider } from "../components/tabs/tabsStore";
import { useReadingKeybindings } from "./useReadingKeybindings";

let dirty = false;
function ReadingShell() {
  const reader = useRef<HTMLElement>(null);
  const enabled = useReadingKeybindings(reader, true);
  return <section ref={reader} data-testid="reader" tabIndex={enabled ? 0 : undefined}><Outlet /></section>;
}
function Note() {
  useBlocker({ shouldBlockFn: () => dirty && !window.confirm("Discard draft?"), enableBeforeUnload: false });
  return <div className="note-preview">Rendered draft</div>;
}
async function setup() {
  const history = createMemoryHistory({ initialEntries: ["/notes/a"] });
  const root = createRootRoute({ component: () => <TabsProvider><ReadingShell /></TabsProvider> });
  const note = createRoute({ getParentRoute: () => root, path: "/notes/$noteId", component: Note });
  const router = createRouter({ routeTree: root.addChildren([note]), history });
  render(<RouterProvider router={router} />);
  const reader = await screen.findByTestId("reader");
  await screen.findByText("Rendered draft");
  reader.focus();
  const press = async (shiftKey = false) => {
    await act(async () => { reader.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", shiftKey, bubbles: true, cancelable: true })); });
  };
  return { history, reader, press };
}
beforeEach(() => {
  window.__trackNativeReading = true;
  dirty = false;
  localStorage.setItem("track.tabs", JSON.stringify(["a", "graph", "b", "calendar", "c", "markdown"].map((id) => ({ id, title: id }))));
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
});
afterEach(() => { delete window.__trackNativeReading; localStorage.clear(); vi.restoreAllMocks(); });

describe("reading shortcuts through the real router and MRU store", () => {
  it("cycles three notes forward and backward with wrap, skipping view tabs", async () => {
    const { history, press, reader } = await setup();
    for (const id of ["b", "c", "a"]) {
      await press();
      await waitFor(() => expect(history.location.pathname).toBe(`/notes/${id}`));
      expect(reader).toHaveFocus();
      expect(JSON.parse(localStorage.getItem("track.tabs")!)[0].id).toBe(id);
    }
    for (const id of ["c", "b", "a"]) {
      await press(true);
      await waitFor(() => expect(history.location.pathname).toBe(`/notes/${id}`));
    }
  });
  it("retains the existing dirty-note guard and does not advance after Cancel", async () => {
    dirty = true;
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history, press } = await setup();
    await press();
    expect(confirm).toHaveBeenCalledOnce();
    expect(history.location.pathname).toBe("/notes/a");
    expect(screen.getByText("Rendered draft")).toBeVisible();
    confirm.mockReturnValue(true);
    await press();
    await waitFor(() => expect(history.location.pathname).toBe("/notes/b"));
    expect(confirm).toHaveBeenCalledTimes(2);
  });
});
