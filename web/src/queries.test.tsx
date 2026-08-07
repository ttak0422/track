import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { APIError, setTaskState } from "./api";
import { NotificationProvider, NotificationToast } from "./notifications";
import { queryKeys, useSetTaskDateMutation, useSetTaskStateMutation } from "./queries";

// Only the two task writes are stubbed; the rest of the api module keeps its real implementation.
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  setTaskState: vi.fn(async () => ({ tasks: { items: [] }, etag: "next" })),
  setTaskDate: vi.fn(async () => ({ tasks: { items: [] }, etag: "next" })),
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
