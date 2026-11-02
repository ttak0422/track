import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Link, Outlet, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useContext } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getNote, renderMarkdown } from "../api";
import { NoteControlsProvider, useNoteControls } from "../noteControls";
import { queryKeys } from "../queries";
import type { NoteResponse, RenderResponse } from "../types";
import { NoteEditor } from "./NoteEditor";
import { TabsProvider } from "./tabs/tabsStore";
import { TaskBoardContext } from "./markdown/context";
import type { DrawioEditing } from "./markdown/drawioEditing";

vi.mock("../api", async (original) => ({
  ...(await original<typeof import("../api")>()), getNote: vi.fn(), renderMarkdown: vi.fn(),
}));
// Keep the production note/render queries and editor branching. A small formatted surface makes
// replacement with the escaped-source fallback (and remounting) observable without canvas engines.
vi.mock("./MarkdownView", () => ({ MarkdownView: ({ markdown, drawioEditing }: { markdown: string; drawioEditing?: DrawioEditing | null }) => {
  const taskBoard = useContext(TaskBoardContext);
  return <div className="markdown-view" data-testid="formatted-preview" data-task-note={taskBoard?.noteID ?? ""}
    data-drawio-editable={typeof drawioEditing?.open === "function" ? "true" : "false"}>
    <h2>{markdown.replace(/^## /, "")}</h2>
  </div>;
} }));
vi.mock("./noteShared", () => ({
  journalDateFromNote: () => "", useScrollToHash: () => {},
  LoadingIndicator: ({ label }: { label: string }) => <p role="status">{label}</p>,
  NoteAside: () => null, NoteBreadcrumbs: () => null, NoteProperties: () => null, NoteStamps: () => null,
}));
vi.mock("./LineHint", () => ({ LineHint: () => null }));
vi.mock("../reading", () => ({ markSeen: () => {}, recordView: () => {}, VIEW_TICK_SEC: 1000 }));

const sourceA = "## Formatted a";
function note(id: string, body = `## Formatted ${id}`, etag = "original"): NoteResponse {
  return { note: { note_id: id, title: `Note ${id}`, body, etag, file_kind: "note", path: `${id}.md`, copy_path: `${id}.md` }, backlinks: [] };
}
const rendered = (sourceBody: string): RenderResponse => ({ markdown: sourceBody, sourceBody });
function Navigation() {
  const { setMode } = useNoteControls();
  return <nav><button onClick={() => setMode("split")}>Split</button>
    <Link to="/notes/$noteId" params={{ noteId: "b" }}>Open B</Link></nav>;
}
const disposals: (() => void)[] = [];
function mount() {
  const root = createRootRoute({ component: () => <TabsProvider><NoteControlsProvider><Navigation /><div className="reader" style={{ overflowY: "auto" }}><Outlet /></div></NoteControlsProvider></TabsProvider> });
  const reader = createRoute({ getParentRoute: () => root, path: "/notes/$noteId", component: () => {
    const { noteId } = reader.useParams();
    return <NoteEditor noteID={noteId} />;
  } });
  const history = createMemoryHistory({ initialEntries: ["/notes/a"] });
  const router = createRouter({ routeTree: root.addChildren([reader]), history, defaultPendingMinMs: 0 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);
  disposals.push(() => { view.unmount(); history.destroy(); client.clear(); });
  return { client, router };
}
beforeEach(() => {
  localStorage.clear();
  vi.mocked(getNote).mockReset().mockImplementation(async (id) => note(id));
  vi.mocked(renderMarkdown).mockReset().mockImplementation(async (body) => rendered(body));
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  vi.spyOn(window, "confirm").mockReturnValue(true);
});
afterEach(() => {
  disposals.splice(0).forEach((dispose) => dispose());
  vi.restoreAllMocks();
});

describe("render refresh resilience", () => {
  it("keeps the formatted preview mounted through a failed refresh, pending retry, and recovery", async () => {
    const { client } = mount();
    await screen.findByRole("heading", { name: "Formatted a" });
    fireEvent.click(screen.getByRole("button", { name: "Split" }));
    const editor = screen.getByRole("textbox", { name: "Note body" }) as HTMLTextAreaElement;
    editor.focus();
    editor.setSelectionRange(3, 8);
    const preview = screen.getByTestId("formatted-preview");
    expect(preview).toHaveAttribute("data-task-note", "a");
    expect(preview).toHaveAttribute("data-drawio-editable", "true");
    preview.scrollTop = 47;
    vi.mocked(renderMarkdown).mockRejectedValue(new Error("temporary render failure"));
    await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.render(sourceA) }); });
    const retry = await screen.findByRole("button", { name: "Retry preview" });
    expect(screen.getByTestId("formatted-preview")).toBe(preview);
    expect(preview).toHaveAttribute("data-task-note", "");
    expect(preview).toHaveAttribute("data-drawio-editable", "false");
    expect(screen.getByRole("heading", { name: "Formatted a" })).toBeInTheDocument();
    expect(preview.scrollTop).toBe(47);
    expect(screen.getByRole("textbox", { name: "Note body" })).toBe(editor);
    expect(editor).toHaveFocus();
    expect([editor.selectionStart, editor.selectionEnd]).toEqual([3, 8]);
    expect(document.querySelector("pre code")).toBeNull();

    let finish!: (response: RenderResponse) => void;
    vi.mocked(renderMarkdown).mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    fireEvent.click(retry);
    expect(await screen.findByRole("button", { name: "Retrying…" })).toBeDisabled();
    expect(screen.getByTestId("formatted-preview")).toBe(preview);
    expect(preview).toHaveAttribute("data-task-note", "");
    expect(preview).toHaveAttribute("data-drawio-editable", "false");
    await act(async () => finish(rendered(sourceA)));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Retry preview" })).toBeNull());
    expect(screen.getByTestId("formatted-preview")).toBe(preview);
    expect(preview).toHaveAttribute("data-task-note", "a");
    expect(preview).toHaveAttribute("data-drawio-editable", "true");
    expect(preview.scrollTop).toBe(47);
    expect(document.querySelector("pre code")).toBeNull();
  });

  it("shows escaped current source, not the previous successful render, if a changed body fails", async () => {
    mount();
    await screen.findByRole("heading", { name: "Formatted a" });
    fireEvent.click(screen.getByRole("button", { name: "Split" }));
    const editor = screen.getByRole("textbox", { name: "Note body" });
    const draft = '## Changed draft <img src=x onerror="alert(1)">';
    vi.mocked(renderMarkdown).mockRejectedValue(new Error("cannot render new body"));
    fireEvent.change(editor, { target: { value: draft } });
    await screen.findByRole("button", { name: "Retry preview" });
    expect(document.querySelector("pre code")).toHaveTextContent(draft);
    expect(screen.queryByRole("heading", { name: "Formatted a" })).toBeNull();
    expect(document.querySelector("img")).toBeNull();
    expect(editor).toHaveValue(draft);
  });

  it("does not carry a failed note's cached preview into another note", async () => {
    const { client, router } = mount();
    await screen.findByRole("heading", { name: "Formatted a" });
    vi.mocked(renderMarkdown).mockRejectedValue(new Error("render offline"));
    await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.render(sourceA) }); });
    await screen.findByRole("button", { name: "Retry preview" });
    fireEvent.click(screen.getByRole("link", { name: "Open B" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/notes/b"));
    await waitFor(() => expect(document.querySelector("pre code")).toHaveTextContent("## Formatted b"));
    expect(screen.queryByRole("heading", { name: "Formatted a" })).toBeNull();
    expect(screen.queryByTestId("formatted-preview")).toBeNull();
  });
});

it("keeps the visible passage at the same viewport pixel when refresh notices appear and disappear", async () => {
  const { client } = mount();
  const heading = await screen.findByRole("heading", { name: "Formatted a" });
  const reader = heading.closest<HTMLElement>(".reader")!;
  // jsdom has no layout. Model a real outer reading viewport and the in-flow
  // recovery notice's height; production query/React snapshot lifecycles run unchanged.
  const noticeHeight = () => reader.textContent?.includes("Could not refresh the preview") ? 80 : 0;
  let scroll = 800;
  Object.defineProperties(reader, {
    clientHeight: { configurable: true, get: () => 500 },
    scrollHeight: { configurable: true, get: () => 1800 + noticeHeight() },
    scrollTop: { configurable: true, get: () => scroll, set: (value: number) => { scroll = Math.max(0, Math.min(value, 1300 + noticeHeight())); } },
  });
  vi.spyOn(reader, "getBoundingClientRect").mockImplementation(() => ({ top: 0, bottom: 500, height: 500 } as DOMRect));
  vi.spyOn(heading, "getBoundingClientRect").mockImplementation(() => {
    const top = 900 + noticeHeight() - reader.scrollTop;
    return { top, bottom: top + 60, height: 60 } as DOMRect;
  });
  expect(heading.getBoundingClientRect().top).toBe(100);
  vi.mocked(renderMarkdown).mockRejectedValue(new Error("temporary render failure"));
  await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.render(sourceA) }); });
  await screen.findByRole("button", { name: "Retry preview" });
  expect.soft(heading.getBoundingClientRect().top).toBe(100);
  expect.soft(reader.scrollTop).toBe(880);
  vi.mocked(renderMarkdown).mockImplementation(async (body) => rendered(body));
  fireEvent.click(screen.getByRole("button", { name: "Retry preview" }));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Retry preview" })).toBeNull());
  expect(heading.getBoundingClientRect().top).toBe(100);
  expect(reader.scrollTop).toBe(800);
});
