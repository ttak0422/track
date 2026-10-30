import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Link, Outlet, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { APIError, getNote, saveNote } from "../api";
import { NoteControlsProvider, useNoteControls } from "../noteControls";
import { queryKeys } from "../queries";
import type { NoteResponse } from "../types";
import { NoteEditor } from "./NoteEditor";
import { TabsProvider, useTabs } from "./tabs/tabsStore";

vi.mock("../api", async (original) => ({
  ...(await original<typeof import("../api")>()),
  getNote: vi.fn(),
  saveNote: vi.fn(),
  renderMarkdown: vi.fn(async (body: string) => ({ markdown: body, sourceBody: body })),
}));
vi.mock("./MarkdownView", () => ({ MarkdownView: ({ markdown, sourceMarkdown }: { markdown: string; sourceMarkdown?: string }) => <div data-testid="preview" data-copy-source={sourceMarkdown}>{markdown}</div> }));
vi.mock("./noteShared", () => ({
  journalDateFromNote: () => "", useScrollToHash: () => {},
  LoadingIndicator: ({ label }: { label: string }) => <p role="status">{label}</p>,
  NoteAside: () => null, NoteBreadcrumbs: () => null, NoteProperties: () => null, NoteStamps: () => null,
}));
vi.mock("./LineHint", () => ({ LineHint: () => null }));
vi.mock("../reading", () => ({ markSeen: () => {}, recordView: () => {}, VIEW_TICK_SEC: 1000 }));

function note(id = "a", body = `Saved ${id}`, etag = "original"): NoteResponse {
  return { note: { note_id: id, title: `Note ${id}`, body, etag, file_kind: "note", path: `${id}.md`, copy_path: `${id}.md` }, backlinks: [] };
}

const failure = () => new APIError(500, "SQL logic error: no such table: body_keywords (1)");

function Navigation() {
  const { activeID, close } = useTabs();
  const { setMode } = useNoteControls();
  return <nav>
    <button onClick={() => setMode("edit")}>Edit</button>
    <button onClick={() => setMode("split")}>Split</button>
    <button onClick={() => { if (activeID) close(activeID); }}>Close active tab</button>
    <Link to="/notes/$noteId" params={{ noteId: "b" }}>Open B</Link>
  </nav>;
}

const disposals: (() => void)[] = [];
function mount({ path = "/notes/a", homeNote = false }: { path?: string; homeNote?: boolean } = {}) {
  const root = createRootRoute({ component: () => <TabsProvider><NoteControlsProvider><Navigation /><Outlet /></NoteControlsProvider></TabsProvider> });
  const home = createRoute({ getParentRoute: () => root, path: "/", component: () => homeNote ? <NoteEditor noteID="home" /> : <p>Home</p> });
  const empty = createRoute({ getParentRoute: () => root, path: "/empty", component: () => <p>Workspace</p> });
  const reader = createRoute({ getParentRoute: () => root, path: "/notes/$noteId", component: () => {
    const { noteId } = reader.useParams();
    return <NoteEditor noteID={noteId} />;
  } });
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createRouter({ routeTree: root.addChildren([home, empty, reader]), history, defaultPendingMinMs: 0 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);
  disposals.push(() => { view.unmount(); history.destroy(); client.clear(); });
  return { client, router, history };
}

async function failRefresh(client: QueryClient) {
  vi.mocked(getNote).mockRejectedValue(failure());
  await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.note("a") }); });
  expect(await screen.findByRole("alert")).toHaveTextContent("body_keywords");
}

beforeEach(() => {
  window.localStorage.clear();
  vi.mocked(getNote).mockReset().mockImplementation(async (id) => note(id));
  vi.mocked(saveNote).mockReset();
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  vi.spyOn(window, "confirm").mockReturnValue(true);
});
afterEach(() => {
  disposals.splice(0).forEach((dispose) => dispose());
  vi.restoreAllMocks();
});

describe("note load recovery", () => {
  it("only verifies Copy Range columns for an adopted, saved body", async () => {
    mount();
    await waitFor(() => expect(screen.getByTestId("preview")).toHaveAttribute("data-copy-source", "Saved a"));
    fireEvent.click(screen.getByRole("button", { name: "Split" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Note body" }), { target: { value: "Unsaved draft" } });
    expect(screen.getByTestId("preview")).not.toHaveAttribute("data-copy-source");
    await waitFor(() => expect(screen.getByTestId("preview")).toHaveTextContent("Unsaved draft"));
    expect(screen.getByTestId("preview")).not.toHaveAttribute("data-copy-source");
  });


  it("retains a cached preview through failed retries, then refreshes without reloading", async () => {
    const { client } = mount();
    await screen.findByText("Saved a");
    const preview = screen.getByTestId("preview");
    await failRefresh(client);
    expect(screen.getByTestId("preview")).toBe(preview);
    expect(preview).toHaveTextContent("Saved a");
    expect(screen.getByRole("alert")).toHaveTextContent("last loaded version");
    expect(screen.queryByRole("button", { name: "Close tab" })).toBeNull();

    const beforeRetry = vi.mocked(getNote).mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(beforeRetry + 1));
    await waitFor(() => expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled());
    expect(preview).toHaveTextContent("Saved a");

    let recover!: (value: NoteResponse) => void;
    vi.mocked(getNote).mockImplementation(() => new Promise((resolve) => { recover = resolve; }));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("button", { name: "Retrying…" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Retrying…" }));
    expect(getNote).toHaveBeenCalledTimes(beforeRetry + 2);
    await act(async () => recover(note("a", "Recovered a", "new")));
    await screen.findByText("Recovered a");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(window.__trackNativeEditorState?.()).toBe(false);
    expect(saveNote).not.toHaveBeenCalled();
  });

  it("keeps the draft and original ETag through refresh failure, recovery and a rejected save", async () => {
    const { client, router } = mount();
    await screen.findByText("Saved a");
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const editor = screen.getByRole("textbox", { name: "Note body" });
    fireEvent.change(editor, { target: { value: "Unsaved draft" } });
    await failRefresh(client);
    expect(screen.getByRole("textbox", { name: "Note body" })).toBe(editor);
    expect(editor).toHaveValue("Unsaved draft");
    expect(window.__trackNativeEditorState?.()).toBe(true);

    vi.mocked(window.confirm).mockReturnValue(false);
    fireEvent.click(screen.getByRole("button", { name: "Close active tab" }));
    expect(router.state.location.pathname).toBe("/notes/a");
    expect(editor).toHaveValue("Unsaved draft");
    expect(JSON.parse(localStorage.getItem("track.tabs")!)).toEqual([expect.objectContaining({ id: "a" })]);

    vi.mocked(getNote).mockResolvedValue(note("a", "Changed on disk", "external"));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText("This note changed on disk while you were editing.");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(editor).toHaveValue("Unsaved draft");
    expect(saveNote).not.toHaveBeenCalled();
    vi.mocked(saveNote).mockRejectedValue(new APIError(409, "Save conflict"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("Save conflict");
    expect(saveNote).toHaveBeenCalledExactlyOnceWith("a", { body: "Unsaved draft", etag: "original" });
    expect(editor).toHaveValue("Unsaved draft");
    expect(screen.queryByText("Saved.")).toBeNull();
    expect(window.__trackNativeEditorState?.()).toBe(true);
  });

  it("allows an initial load to fail again and then recover with Retry", async () => {
    vi.mocked(getNote).mockRejectedValue(failure());
    mount();
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(2));
    await screen.findByRole("alert");
    vi.mocked(getNote).mockResolvedValue(note());
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText("Saved a");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("closes failed tabs to the actual neighbor, then home, without waiting for the server", async () => {
    localStorage.setItem("track.tabs", JSON.stringify([{ id: "a", title: "A" }, { id: "b", title: "B" }]));
    vi.mocked(getNote).mockRejectedValue(failure());
    const { router } = mount();
    fireEvent.click(await screen.findByRole("button", { name: "Close tab" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/notes/b"));
    fireEvent.click(await screen.findByRole("button", { name: "Close tab" }));
    await screen.findByText("Home");
    expect(router.state.location.pathname).toBe("/");
    expect(JSON.parse(localStorage.getItem("track.tabs")!)).toEqual([]);
    expect(window.confirm).not.toHaveBeenCalled();
  });

  it("can dismiss a pending first-load retry without its late response reopening the note", async () => {
    vi.mocked(getNote).mockRejectedValue(failure());
    const { router } = mount();
    await screen.findByRole("alert");
    let recover!: (value: NoteResponse) => void;
    vi.mocked(getNote).mockImplementation(() => new Promise((resolve) => { recover = resolve; }));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByRole("status");
    fireEvent.click(screen.getByRole("button", { name: "Close tab" }));
    await screen.findByText("Home");
    await act(async () => recover(note()));
    expect(router.state.location.pathname).toBe("/");
    expect(screen.getByText("Home")).toBeInTheDocument();
    expect(JSON.parse(localStorage.getItem("track.tabs")!)).toEqual([]);
  });

  it("leaves a failed configured home note for the workspace, with working Back and Forward", async () => {
    vi.mocked(getNote).mockRejectedValue(failure());
    const { router, history } = mount({ path: "/", homeNote: true });
    expect(await screen.findByRole("button", { name: "Go to workspace" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Close tab" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Go to workspace" }));
    await screen.findByText("Workspace");
    expect(router.state.location.pathname).toBe("/empty");
    await act(async () => history.back());
    await screen.findByRole("button", { name: "Go to workspace" });
    expect(router.state.location.pathname).toBe("/");
    await act(async () => history.forward());
    await screen.findByText("Workspace");
  });

  it("does not let a late retry replace a newer note after navigation", async () => {
    const { client, router } = mount();
    await screen.findByText("Saved a");
    await failRefresh(client);
    let recover!: (value: NoteResponse) => void;
    vi.mocked(getNote).mockImplementation((id) => id === "a"
      ? new Promise((resolve) => { recover = resolve; }) : Promise.resolve(note(id)));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByRole("button", { name: "Retrying…" });
    fireEvent.click(screen.getByRole("link", { name: "Open B" }));
    await screen.findByText("Saved b");
    await act(async () => recover(note("a", "Late a", "late")));
    expect(router.state.location.pathname).toBe("/notes/b");
    expect(screen.getByTestId("preview")).toHaveTextContent("Saved b");
    expect(screen.queryByText("Late a")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
