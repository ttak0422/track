import { act, render, screen, waitFor } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider, useBlocker } from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TabsProvider, useTabs } from "../components/tabs/tabsStore";
import { useNativeTabCommands } from "./useNativeTabCommands";

let dirty = false;
function Shell() {
  useNativeTabCommands();
  const { tabs, activeID, dirtyID, setDirty, close } = useTabs();
  return <><output>{tabs.map((tab) => tab.id).join(",")}</output>
    <button onClick={() => setDirty("a")}>Mark dirty</button>
    <button onClick={() => activeID && close(activeID)}>Close current</button>
    <span data-testid="dirty">{dirtyID}</span><Outlet /></>;
}
function Note() {
  useBlocker({ shouldBlockFn: () => dirty && !window.confirm("Discard draft?"), enableBeforeUnload: false });
  return <textarea aria-label="Draft" defaultValue="draft" />;
}
async function setup(path = "/notes/a") {
  const history = createMemoryHistory({ initialEntries: [path] });
  const root = createRootRoute({ component: () => <TabsProvider><Shell /></TabsProvider> });
  const note = createRoute({ getParentRoute: () => root, path: "/notes/$noteId", component: Note });
  const graph = createRoute({ getParentRoute: () => root, path: "/graph", component: () => <p>Graph</p> });
  const home = createRoute({ getParentRoute: () => root, path: "/", component: () => <p>Home</p> });
  const router = createRouter({ routeTree: root.addChildren([note, graph, home]), history });
  render(<RouterProvider router={router} />);
  await screen.findByRole("status");
  const command = async (action: string) => {
    await act(async () => { window.dispatchEvent(new CustomEvent("track:native-tab-command", { detail: action })); });
  };
  return { history, command };
}
const ids = () => JSON.parse(localStorage.getItem("track.tabs")!).map((tab: { id: string }) => tab.id);
beforeEach(() => {
  window.__trackNativeReading = true;
  dirty = false;
  localStorage.setItem("track.tabs", JSON.stringify(["a", "graph", "b", "c"].map((id) => ({ id, title: id }))));
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
});
afterEach(() => {
  delete window.__trackNativeReading;
  delete window.__trackNativeEditorState;
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("native tab commands with the router and MRU store", () => {
  it("cycles all tabs in both directions across MRU promotions, including a view tab", async () => {
    const { history, command } = await setup();
    for (const path of ["/graph", "/notes/b", "/notes/c", "/notes/a"]) {
      await command("tabs.next");
      await waitFor(() => expect(history.location.pathname).toBe(path));
    }
    for (const path of ["/notes/c", "/notes/b", "/graph", "/notes/a"]) {
      await command("tabs.previous");
      await waitFor(() => expect(history.location.pathname).toBe(path));
    }
  });
  it("closes from a text field, leaves the last-tab workspace open, and ignores empty close", async () => {
    localStorage.setItem("track.tabs", JSON.stringify([{ id: "a", title: "a" }]));
    const confirm = vi.spyOn(window, "confirm");
    const { history, command } = await setup();
    (await screen.findByRole("textbox")).focus();
    await command("tabs.close");
    await waitFor(() => expect(history.location.pathname).toBe("/"));
    expect(ids()).toEqual([]);
    expect(confirm).not.toHaveBeenCalled();
    await command("tabs.close");
    expect(history.location.pathname).toBe("/");
  });
  it("keeps dirty tab and indicator on Cancel, confirms only once on accepted close", async () => {
    dirty = true;
    window.__trackNativeEditorState = () => true;
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history, command } = await setup();
    await act(async () => { screen.getByText("Mark dirty").click(); });
    await command("tabs.close");
    expect(history.location.pathname).toBe("/notes/a");
    expect(ids()).toEqual(["a", "graph", "b", "c"]);
    expect(screen.getByTestId("dirty")).toHaveTextContent("a");
    expect(screen.getByRole("textbox")).toHaveValue("draft");
    confirm.mockReturnValue(true);
    await command("tabs.close");
    await waitFor(() => expect(history.location.pathname).toBe("/graph"));
    expect(ids()).not.toContain("a");
    expect(confirm).toHaveBeenCalledTimes(2);
  });
  it("does not advance a canceled side-button switch and retries the same neighbor", async () => {
    dirty = true;
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history, command } = await setup();
    await command("tabs.next");
    expect(history.location.pathname).toBe("/notes/a");
    confirm.mockReturnValue(true);
    await command("tabs.next");
    await waitFor(() => expect(history.location.pathname).toBe("/graph"));
    expect(confirm).toHaveBeenCalledTimes(2);
  });
  it("checks live editor input before the dirty indicator updates, then allows a saved close", async () => {
    let liveDirty = true;
    window.__trackNativeEditorState = () => liveDirty;
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history, command } = await setup();
    await command("tabs.close");
    expect(confirm).toHaveBeenCalledOnce();
    expect(history.location.pathname).toBe("/notes/a");
    expect(ids()).toContain("a");
    liveDirty = false;
    await command("tabs.close");
    await waitFor(() => expect(history.location.pathname).toBe("/graph"));
    expect(confirm).toHaveBeenCalledOnce();
  });
  it("protects a browser tab close through the same store without the native marker", async () => {
    delete window.__trackNativeReading;
    dirty = true;
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { history } = await setup();
    await act(async () => { screen.getByText("Mark dirty").click(); });
    await act(async () => { screen.getByText("Close current").click(); });
    expect(history.location.pathname).toBe("/notes/a");
    expect(ids()).toContain("a");
    confirm.mockReturnValue(true);
    await act(async () => { screen.getByText("Close current").click(); });
    await waitFor(() => expect(history.location.pathname).toBe("/graph"));
    expect(confirm).toHaveBeenCalledTimes(2);
  });
  it("ignores native events in an ordinary web workspace", async () => {
    delete window.__trackNativeReading;
    const { history, command } = await setup();
    for (const action of ["tabs.close", "tabs.next", "tabs.previous"]) await command(action);
    expect(history.location.pathname).toBe("/notes/a");
    expect(ids()).toEqual(["a", "graph", "b", "c"]);
  });
  it("leaves dialogs alone and ignores unrelated commands", async () => {
    const { history, command } = await setup();
    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    document.body.appendChild(dialog);
    await command("tabs.close");
    dialog.remove();
    await command("application.quit");
    expect(history.location.pathname).toBe("/notes/a");
    expect(ids()).toContain("a");
  });
});
