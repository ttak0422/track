import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { APIError, searchNotes, setTaskState } from "./api";
import { NotificationProvider, NotificationToast } from "./notifications";
import { queryKeys, useSearchQuery, useSetTaskDateMutation, useSetTaskStateMutation } from "./queries";

// The two task writes and the search stay stubbed; the rest of the api module keeps its real
// implementation.
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  setTaskState: vi.fn(async () => ({ tasks: { items: [] }, etag: "next" })),
  setTaskDate: vi.fn(async () => ({ tasks: { items: [] }, etag: "next" })),
  searchNotes: vi.fn(async () => ({ results: [] })),
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
