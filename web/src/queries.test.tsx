import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { APIError, getGraph, searchNotes, setTaskState } from "./api";
import { NotificationProvider, NotificationToast } from "./notifications";
import {
  queryKeys,
  useActivityQuery,
  useGraphQuery,
  useNewNotesQuery,
  useSearchQuery,
  useSetTaskDateMutation,
  useSetTaskStateMutation,
} from "./queries";

// The two task writes, the search, the scoped listings, and the graph fetch stay stubbed; the
// rest of the api module keeps its real implementation.
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  setTaskState: vi.fn(async () => ({ tasks: { items: [] }, etag: "next" })),
  setTaskDate: vi.fn(async () => ({ tasks: { items: [] }, etag: "next" })),
  searchNotes: vi.fn(async () => ({ results: [] })),
  getActivity: vi.fn(async (since: string, until: string) => ({
    activity: { since, until, total: 0, counts: [] },
  })),
  listNewNotes: vi.fn(async () => ({ notes: [] })),
  getGraph: vi.fn(async () => ({
    graph: { center_id: "1", nodes: [{ note_id: "1", file_kind: "note", title: "Root" }], edges: [] },
  })),
}));

// The toast is mounted by Shell below the router; the tests mount it next to the provider to observe
// it, so the router module it navigates with is stubbed out.
const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

function renderTaskMutation<T>(useHook: () => T) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const view = renderHook(useHook, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        <NotificationProvider>
          {children}
          <NotificationToast />
        </NotificationProvider>
      </QueryClientProvider>
    ),
  });
  return { invalidate, ...view };
}

// A task write rewrites the note body, so both writes must refresh the same views — a listing left
// behind keeps showing the task where it no longer is.
const taskWriteKeys = [queryKeys.note("100"), queryKeys.notes(), queryKeys.tasks(), ["render"]];

describe("task write mutations", () => {
  beforeEach(() => vi.mocked(setTaskState).mockClear());

  it("refreshes the note, both listings, and embedded renders after a state write", async () => {
    const { invalidate, result } = renderTaskMutation(() => useSetTaskStateMutation("100"));
    act(() => result.current.mutate({ line: 1, state: "DONE", expect: "TODO", etag: "loaded" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    for (const queryKey of taskWriteKeys) expect(invalidate).toHaveBeenCalledWith({ queryKey });
  });

  it("shows a stale-note notification and refetches after a 409", async () => {
    vi.mocked(setTaskState).mockRejectedValueOnce(new APIError(409, "note changed on disk"));
    const { invalidate, result } = renderTaskMutation(() => useSetTaskStateMutation("100"));

    act(() => result.current.mutate({ line: 3, state: "DONE", expect: "TODO", etag: "stale" }));

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(setTaskState).toHaveBeenCalledWith("100", 3, "DONE", "TODO", "stale");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.note("100") });
    expect(screen.getByRole("alert")).toHaveTextContent(/changed.*reload.*retry/i);
  });

  it("refreshes the same set after a date write", async () => {
    const { invalidate, result } = renderTaskMutation(() => useSetTaskDateMutation("100"));
    act(() => result.current.mutate({ line: 1, field: "due", date: "2026-08-01", etag: "loaded" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    for (const queryKey of taskWriteKeys) expect(invalidate).toHaveBeenCalledWith({ queryKey });
  });
});

function renderSearchQuery(query: string, limit = 100, vault = "") {
  const client = new QueryClient();
  const view = renderHook(() => useSearchQuery(query, limit, vault), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  return { client, ...view };
}

// Search follows the same vault-in-the-key pattern as resolve and agenda: the vault is the second
// key element, so a scope switch refetches under its own key instead of reusing another vault's
// cached hits, and the request carries ?vault= down to the server.
describe("useSearchQuery", () => {
  beforeEach(() => vi.mocked(searchNotes).mockClear());

  it("passes the selected vault to the request and names it in the key", async () => {
    const { client, result } = renderSearchQuery("term", 100, "work");
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(searchNotes).toHaveBeenCalledWith("term", 100, "work");
    expect(client.getQueryState(["search", "work", "term", 100])?.status).toBe("success");
  });

  it("federates under the launch-vault key when no vault is selected", async () => {
    const { client, result } = renderSearchQuery("term", 50);
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(searchNotes).toHaveBeenCalledWith("term", 50, "");
    expect(client.getQueryState(["search", "", "term", 50])?.status).toBe("success");
  });
});

// The heatmap and the New widget follow the working vault, so their keys carry it (second, like the
// other vault-scoped keys) and the query functions hand it to the endpoint. The prefixes the write
// mutations invalidate (["activity"], ["notes"]) still match the new keys, so a change in one vault
// refreshes every vault's cached copy — the same coarse sweep they always made.
describe("vault-scoped activity and new-notes queries", () => {
  it("keys activity by vault, launch vault as empty", () => {
    expect(queryKeys.activity("2026-01-01", "2026-01-31")).toEqual(["activity", "", "2026-01-01", "2026-01-31"]);
    expect(queryKeys.activity("2026-01-01", "2026-01-31", "work")).toEqual([
      "activity",
      "work",
      "2026-01-01",
      "2026-01-31",
    ]);
    // The write mutations' prefix invalidation still reaches the scoped keys.
    expect(["activity"]).toEqual(queryKeys.activity("2026-01-01", "2026-01-31", "work").slice(0, 1));
  });

  it("keys new notes by vault under the notes prefix", () => {
    expect(queryKeys.newNotes(100)).toEqual(["notes", "new", "", 100]);
    expect(queryKeys.newNotes(100, "work")).toEqual(["notes", "new", "work", 100]);
    // The notes-listing invalidations (queryKeys.notes()) still reach the scoped keys.
    expect(queryKeys.newNotes(100, "work").slice(0, 1)).toEqual(queryKeys.notes());
  });

  it("passes the working vault to getActivity", async () => {
    const { getActivity } = await import("./api");
    const view = renderHook(() => useActivityQuery("2026-01-01", "2026-01-31", "work"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
      ),
    });
    await waitFor(() => expect(view.result.current.isSuccess).toBe(true));
    expect(getActivity).toHaveBeenCalledWith("2026-01-01", "2026-01-31", "work");
  });

  it("passes the working vault to listNewNotes", async () => {
    const { listNewNotes } = await import("./api");
    const view = renderHook(() => useNewNotesQuery(100, "work"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
      ),
    });
    await waitFor(() => expect(view.result.current.isSuccess).toBe(true));
    expect(listNewNotes).toHaveBeenCalledWith(100, "work");
  });
});

// The whole-vault graph is keyed by its vault, and the write-path invalidation uses the bare
// ["graph"] prefix, so a scoped entry must stay reachable through it — as must the per-note local
// graph, which shares the prefix.
describe("whole-vault graph scoping", () => {
  beforeEach(() => vi.mocked(getGraph).mockClear());

  it("keys the graph by vault and keeps the local graph under the same prefix", () => {
    expect(queryKeys.graph("")).toEqual(["graph", ""]);
    expect(queryKeys.graph("work")).toEqual(["graph", "work"]);
    expect(queryKeys.localGraph("work~7")).toEqual(["graph", "local", "work~7"]);
    for (const key of [queryKeys.graph(""), queryKeys.graph("work"), queryKeys.localGraph("work~7")]) {
      expect(key[0]).toBe("graph");
    }
  });

  it("requests the scoped graph and refreshes it when the graph prefix is invalidated", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const view = renderHook(() => useGraphQuery(true, "work"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    });

    await waitFor(() => expect(view.result.current.isSuccess).toBe(true));
    expect(getGraph).toHaveBeenCalledWith("work");
    expect(client.getQueryData(queryKeys.graph("work"))).toBeTruthy();
    expect(client.getQueryData(queryKeys.graph(""))).toBeUndefined();

    // A mutation invalidates ["graph"] as a prefix; it must reach a scoped cache entry and refetch.
    await act(async () => {
      await client.invalidateQueries({ queryKey: ["graph"] });
    });
    expect(getGraph).toHaveBeenCalledTimes(2);
  });
});
