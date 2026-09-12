import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { APIError, getGraph, setTaskState } from "./api";
import { NotificationProvider, NotificationToast } from "./notifications";
import { queryKeys, useGraphQuery, useSetTaskDateMutation, useSetTaskStateMutation } from "./queries";

// Only the two task writes and the graph fetch are stubbed; the rest of the api module keeps its real
// implementation.
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  setTaskState: vi.fn(async () => ({ tasks: { items: [] }, etag: "next" })),
  setTaskDate: vi.fn(async () => ({ tasks: { items: [] }, etag: "next" })),
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
