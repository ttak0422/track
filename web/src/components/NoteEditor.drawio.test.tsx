import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { NoteEditor } from "./NoteEditor";

const mocks = vi.hoisted(() => ({
  note: { note_id: "main~1", title: "Synthetic", body: "```drawio\n<mxfile />\n```\n", etag: "original", file_kind: "note" },
  save: vi.fn(), setMode: vi.fn(), dirty: vi.fn(), blocker: vi.fn(), stale: false, renderError:false, loadError:false, retry:vi.fn(), seen:vi.fn(),
}));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn(), useBlocker: (value: unknown) => mocks.blocker(value) }));
vi.mock("../queries", () => ({
  useNoteQuery: () => ({ isPending: false, isError:mocks.loadError, error:new Error("synthetic read failure"), data: { note: mocks.note } }),
  useSaveNoteMutation: () => ({ mutateAsync: mocks.save, isPending: false }),
  useDeleteNoteMutation: () => ({ reset: vi.fn() }),
  useRenderQuery: (body: string) => ({isError:mocks.renderError,isFetching:false,refetch:mocks.retry, data: mocks.renderError?undefined:{
    sourceBody: mocks.stale ? "older" : body, markdown: "synthetic",
    drawio_fences: [{ id: "capability", start_line: 0, end_line: 2, xml: "<mxfile />" }],
  } }),
}));
vi.mock("./MarkdownView", async () => {
 const {useContext}=await import("react");const {TaskBoardContext}=await import("./markdown/context");
 return {MarkdownView:({drawioEditing}:any)=>{const target=useContext(TaskBoardContext);return <><output data-testid="task-target">{target.noteID}</output>{drawioEditing?<button onClick={()=>drawioEditing.open(drawioEditing.fences[0])}>Edit diagram</button>:<p>Read only</p>}</>}};
});
vi.mock("./DrawioEditorDialog", () => ({ DrawioEditorDialog: ({ onApply, onClose }: any) => <div role="dialog"><button onClick={() => onApply('<mxfile><diagram id="edited" /></mxfile>')}>Apply synthetic</button><button onClick={onClose}>Cancel diagram</button></div> }));
vi.mock("../noteControls", async () => {
  const { useState } = await import("react");
  return { useNoteControls: () => {
    const [mode, setMode] = useState("preview");
    return { mode, setMode: (value: string) => { mocks.setMode(value); setMode(value); }, setActions: () => {}, follow: false };
  } };
});
vi.mock("./tabs/tabsStore", () => ({ useTabs: () => ({ setTitle: () => {}, setDirty: mocks.dirty, close: () => {} }) }));
vi.mock("./noteShared", () => ({ journalDateFromNote: () => null, useScrollToHash: () => {}, LoadingIndicator: () => null, NoteAside: () => null, NoteBreadcrumbs: () => null, NoteProperties: () => null, NoteStamps: () => null }));
vi.mock("./LineHint", () => ({ LineHint: () => null }));
vi.mock("../reading", () => ({ markSeen: mocks.seen, recordView: () => {}, VIEW_TICK_SEC: 1000 }));

beforeEach(() => { mocks.renderError=false;mocks.loadError=false;mocks.retry.mockReset();mocks.seen.mockReset(); mocks.save.mockReset(); mocks.setMode.mockReset(); mocks.dirty.mockReset(); mocks.stale = false; mocks.note = { ...mocks.note, body: "```drawio\n<mxfile />\n```\n", etag: "original" }; });
function open() { fireEvent.click(screen.getByText("Edit diagram")); }

describe("draw.io in the note draft lifecycle", () => {
  it("protects an unapplied session, then applies to Split and saves with the original ETag", async () => {
    mocks.save.mockResolvedValue({ etag: "saved" });
    render(<NoteEditor noteID="main~1" />); open();
    expect(window.__trackNativeEditorState?.()).toBe(true);
    expect(mocks.dirty).toHaveBeenLastCalledWith("main~1");
    const blocker = mocks.blocker.mock.calls.at(-1)![0] as { enableBeforeUnload: () => boolean };
    expect(blocker.enableBeforeUnload()).toBe(true);
    fireEvent.click(screen.getByText("Apply synthetic"));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.setMode).toHaveBeenCalledWith("split");
    const updated = '```drawio\n<mxfile><diagram id="edited" /></mxfile>\n```\n';
    expect(screen.getByRole("textbox", { name: "Note body" })).toHaveValue(updated);
    expect(mocks.save).not.toHaveBeenCalled();
    await act(async () => { fireEvent.submit(screen.getByRole("textbox").closest("form")!); });
    expect(mocks.save).toHaveBeenCalledExactlyOnceWith({ body: updated, etag: "original" });
  });
  it("cancel leaves a previously saved note clean and does not save", () => {
    render(<NoteEditor noteID="main~1" />); open();
    fireEvent.click(screen.getByText("Cancel diagram"));
    expect(window.__trackNativeEditorState?.()).toBe(false);
    expect(mocks.save).not.toHaveBeenCalled();
  });
  it("does not offer editing on a stale render", () => {
    mocks.stale = true;
    render(<NoteEditor noteID="main~1" />);
    expect(screen.queryByText("Edit diagram")).toBeNull();
  });
  it("keeps unapplied XML bound to its original draft on a disk refresh", async () => {
    const view = render(<NoteEditor noteID="main~1" />); open();
    mocks.note = { ...mocks.note, body: "external edit\n", etag: "external" };
    view.rerender(<NoteEditor noteID="main~1" />);
    fireEvent.click(screen.getByText("Apply synthetic"));
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toContain('id="edited"');
    expect(screen.getByText(/changed on disk/)).toBeInTheDocument();
    mocks.save.mockRejectedValue(new Error("409 conflict"));
    await act(async () => { fireEvent.submit(screen.getByRole("textbox").closest("form")!); });
    expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ etag: "original" }));
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toContain('id="edited"');
    expect(window.__trackNativeEditorState?.()).toBe(true);
  });
  it("drops the previous session on a confirmed route switch and never applies it to another note", async () => {
    const view = render(<NoteEditor noteID="main~1" />); open();
    mocks.note = { ...mocks.note, body: "next note\n", etag: "next" };
    view.rerender(<NoteEditor noteID="main~2" />);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(window.__trackNativeEditorState?.()).toBe(false);
    expect(mocks.save).not.toHaveBeenCalled();
  });
});

it("keeps stale rendered task controls inert until the current body is rendered",()=>{
 mocks.stale=true;const view=render(<NoteEditor noteID="main~1" />);
 expect(screen.getByTestId("task-target").textContent).toBe("");
 mocks.stale=false;view.rerender(<NoteEditor noteID="main~1" />);
 expect(screen.getByTestId("task-target").textContent).toBe("main~1");
});
it("shows source after initial preview failure and offers an explicit retry",()=>{
 mocks.renderError=true;render(<NoteEditor noteID="main~1" />);
 expect(screen.getByText(/Preview is unavailable/)).toBeTruthy();
 expect(screen.queryByTestId("task-target")).toBeNull();
 fireEvent.click(screen.getByText("Retry preview"));expect(mocks.retry).toHaveBeenCalledOnce();
});
it("does not mark a failed read as seen or expose cached task writes",()=>{
 mocks.loadError=true;render(<NoteEditor noteID="main~1" />);
 expect(mocks.seen).not.toHaveBeenCalled();
 expect(screen.getByTestId("task-target").textContent).toBe("");
});
