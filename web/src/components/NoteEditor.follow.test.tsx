import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NoteEditor } from "./NoteEditor";

const mocks = vi.hoisted(() => ({ navigate: vi.fn(), setActions: vi.fn(), setDirty: vi.fn(), setTitle: vi.fn() }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => mocks.navigate, useBlocker: () => {} }));
vi.mock("../queries", () => ({
  useNoteQuery: () => ({ isPending: false, data: { note: { note_id: "launch~1", title: "Synthetic", body: "", etag: "original", file_kind: "note" } } }),
  useSaveNoteMutation: () => ({ isPending: false }),
  useDeleteNoteMutation: () => ({ reset: () => {} }),
  useRenderQuery: () => ({ data: { markdown: "" } }),
}));
vi.mock("../noteControls", () => ({ useNoteControls: () => ({ mode: "preview", follow: true, setActions: mocks.setActions }) }));
vi.mock("./tabs/tabsStore", () => ({ useTabs: () => ({ setTitle: mocks.setTitle, setDirty: mocks.setDirty, close: () => {} }) }));
vi.mock("./MarkdownView", () => ({ MarkdownView: () => null }));
vi.mock("./DrawioEditorDialog", () => ({ DrawioEditorDialog: () => null }));
vi.mock("./NoteMetaDialog", () => ({ NoteMetaDialog: () => null }));
vi.mock("./LineHint", () => ({ LineHint: () => null }));
vi.mock("./noteShared", () => ({ journalDateFromNote: () => null, useScrollToHash: () => {}, LoadingIndicator: () => null, NoteAside: () => null, NoteBreadcrumbs: () => null, NoteProperties: () => null, NoteStamps: () => null }));
vi.mock("../reading", () => ({ markSeen: () => {}, recordView: () => {}, VIEW_TICK_SEC: 1000 }));

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  listeners = new Map<string, (event: MessageEvent) => void>();
  closed = false;
  constructor(public url: string) { FakeEventSource.instances.push(this); }
  addEventListener(name: string, callback: (event: MessageEvent) => void) { this.listeners.set(name, callback); }
  emit(data: string) { this.listeners.get("follow")?.(new MessageEvent("follow", { data })); }
  close() { this.closed = true; }
}

beforeEach(() => {
  vi.clearAllMocks();
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ active: false }))));
});
afterEach(() => vi.unstubAllGlobals());

describe("NoteEditor follow ID boundary", () => {
  it.each([
    { vault: "work", note_id: 7, expected: "work~7" },
    { vault: "work", note_id: "AbC123", expected: "work~AbC123" },
    { vault: "", note_id: 7, expected: "7" },
  ])("navigates to $expected from the real SSE normalization path", async ({ expected, ...state }) => {
    const view = render(<NoteEditor noteID="launch~1" />);
    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/follow", expect.anything()));
    expect(FakeEventSource.instances).toHaveLength(1);
    const source = FakeEventSource.instances[0];
    expect(source.url).toBe("/api/events");
    act(() => source.emit(JSON.stringify(state)));
    expect(mocks.navigate).toHaveBeenCalledExactlyOnceWith({ to: "/notes/$noteId", params: { noteId: expected } });
    view.unmount();
    expect(source.closed).toBe(true);
  });

  it("uses the same normalization for the initial HTTP follow response", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ active: true, vault: "work", state: { note_id: 7 } }))));
    render(<NoteEditor noteID="launch~1" />);
    await waitFor(() => expect(mocks.navigate).toHaveBeenCalledExactlyOnceWith({ to: "/notes/$noteId", params: { noteId: "work~7" } }));
  });

  it("continues ignoring malformed follow JSON", async () => {
    render(<NoteEditor noteID="launch~1" />);
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    act(() => FakeEventSource.instances[0].emit("{"));
    expect(mocks.navigate).not.toHaveBeenCalled();
  });
});
