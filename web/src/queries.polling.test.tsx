import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getNote } from "./api";
import { queryKeys, useNoteQuery } from "./queries";
import type { NoteResponse } from "./types";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  getNote: vi.fn(),
}));

const storageKey = ["storage-status"];
const ready = (generation = 1) => ({ phase: "ready", read_only: false, generation });
function note(readOnly = false, id = "a"): NoteResponse {
  return { note: { note_id: id, title: id, body: "Saved body", etag: "saved", file_kind: "note",
    path: `${id}.md`, copy_path: `${id}.md`, read_only: readOnly }, backlinks: [] };
}

const disposals: (() => void)[] = [];
function mount(initial?: NoteResponse) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity, gcTime: Infinity } } });
  client.setQueryData(storageKey, ready());
  if (initial) client.setQueryData(queryKeys.note("a"), initial);
  const rendered = vi.fn();
  const view = renderHook(({ id }) => {
    const result = useNoteQuery(id, { live: true });
    rendered(result.data);
    return result;
  }, { initialProps: { id: "a" }, wrapper: ({ children }: { children: ReactNode }) =>
    <QueryClientProvider client={client}>{children}</QueryClientProvider> });
  disposals.push(() => { view.unmount(); client.clear(); });
  return { client, rendered, ...view };
}

// Advance only the clock: query notifications still use their ordinary asynchronous queue.
async function poll(client: QueryClient, generation = 1) {
  vi.setSystemTime(Date.now() + 1000);
  await act(async () => {
    client.setQueryData(storageKey, ready(generation));
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-02T00:00:00Z"));
  vi.mocked(getNote).mockReset().mockResolvedValue(note());
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify(ready()), { status: 200 })));
});
afterEach(() => {
  disposals.splice(0).forEach((dispose) => dispose());
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("open note storage polling", () => {
  it("does not rerender a writable note when another observer polls unchanged storage", async () => {
    const { client, rendered, result } = mount(note());
    await poll(client);
    rendered.mockClear();
    const data = result.current.data;
    await poll(client);
    await poll(client);
    await poll(client);
    expect(result.current.data).toBe(data);
    expect(rendered).not.toHaveBeenCalled();
    expect(getNote).not.toHaveBeenCalled();
  });

  it("recovers a late read-only first response only after a newer verified poll, with bounded retries", async () => {
    let resolve!: (response: NoteResponse) => void;
    vi.mocked(getNote).mockImplementationOnce(() => new Promise((done) => { resolve = done; }));
    const { client, result } = mount();
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(1));
    vi.setSystemTime(Date.now() + 500);
    vi.mocked(getNote).mockResolvedValue(note(true));
    await act(async () => resolve(note(true)));
    await waitFor(() => expect(result.current.data?.note.read_only).toBe(true));
    expect(getNote).toHaveBeenCalledTimes(1);
    await poll(client);
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(2));
    await poll(client);
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(3));
    await poll(client);
    expect(getNote).toHaveBeenCalledTimes(3);
    await poll(client, 2);
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(4));
  });

  it("resubscribes when a writable note becomes read-only and stops when it recovers", async () => {
    const { client, result, rendered } = mount(note());
    vi.setSystemTime(Date.now() + 500);
    await act(async () => { client.setQueryData(queryKeys.note("a"), note(true)); });
    await waitFor(() => expect(result.current.data?.note.read_only).toBe(true));
    expect(getNote).not.toHaveBeenCalled();
    await poll(client);
    await waitFor(() => expect(result.current.data?.note.read_only).toBe(false));
    expect(getNote).toHaveBeenCalledTimes(1);
    rendered.mockClear();
    await poll(client);
    await poll(client);
    expect(rendered).not.toHaveBeenCalled();
    expect(getNote).toHaveBeenCalledTimes(1);
  });

  it("does not authorize recovery from a failed storage poll", async () => {
    const { client, result } = mount(note(true));
    // Initial cache timestamps are equal, so allow the existing initial recovery to settle first.
    await waitFor(() => expect(result.current.data?.note.read_only).toBe(false));
    vi.setSystemTime(Date.now() + 500);
    await act(async () => { client.setQueryData(queryKeys.note("a"), note(true)); });
    await waitFor(() => expect(result.current.data?.note.read_only).toBe(true));
    vi.mocked(getNote).mockClear();
    vi.mocked(fetch).mockRejectedValue(new Error("offline"));
    await act(async () => { await client.refetchQueries({ queryKey: storageKey }); });
    expect(client.getQueryState(storageKey)?.status).toBe("error");
    expect(getNote).not.toHaveBeenCalled();
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(ready()), { status: 200 }));
    await poll(client);
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(1));
  });

  it("gives a newly selected note its own recovery budget", async () => {
    vi.mocked(getNote).mockImplementation(async (id) => note(true, id));
    const { client, result, rerender } = mount(note(true));
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(1));
    await poll(client);
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(2));
    await poll(client);
    expect(getNote).toHaveBeenCalledTimes(2);

    vi.setSystemTime(Date.now() + 500);
    client.setQueryData(queryKeys.note("b"), note(true, "b"));
    rerender({ id: "b" });
    expect(result.current.data?.note.note_id).toBe("b");
    expect(getNote).toHaveBeenCalledTimes(2);
    await poll(client);
    await waitFor(() => expect(getNote).toHaveBeenCalledTimes(3));
    expect(getNote).toHaveBeenLastCalledWith("b");
    expect(result.current.data?.note.note_id).toBe("b");
  });
});
