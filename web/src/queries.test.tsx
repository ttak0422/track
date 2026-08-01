import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { queryKeys, useSetTaskDateMutation, useSetTaskStateMutation } from "./queries";

// Only the two task writes are stubbed; the rest of the api module keeps its real implementation.
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  setTaskState: vi.fn(async () => ({ tasks: { items: [] } })),
  setTaskDate: vi.fn(async () => ({ tasks: { items: [] } })),
}));

function renderTaskMutation<T>(useHook: () => T) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const view = renderHook(useHook, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  return { invalidate, ...view };
}

// A task write rewrites the note body, so both writes must refresh the same views — a listing left
// behind keeps showing the task where it no longer is.
const taskWriteKeys = [queryKeys.note("100"), queryKeys.notes(), queryKeys.tasks(), ["render"]];

describe("task write mutations", () => {
  it("refreshes the note, both listings, and embedded renders after a state write", async () => {
    const { invalidate, result } = renderTaskMutation(() => useSetTaskStateMutation("100"));
    act(() => result.current.mutate({ line: 1, state: "DONE", expect: "TODO" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    for (const queryKey of taskWriteKeys) expect(invalidate).toHaveBeenCalledWith({ queryKey });
  });

  it("refreshes the same set after a date write", async () => {
    const { invalidate, result } = renderTaskMutation(() => useSetTaskDateMutation("100"));
    act(() => result.current.mutate({ line: 1, field: "due", date: "2026-08-01" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    for (const queryKey of taskWriteKeys) expect(invalidate).toHaveBeenCalledWith({ queryKey });
  });
});
