import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { StorageNotice } from "./storage";
afterEach(() => { vi.unstubAllGlobals(); });
it("keeps the notice quiet and makes one explicit retry, then invalidates data after recovery", async () => {
  let ready = false;
  let complete!: () => void;
  const pending = new Promise<void>(resolve => { complete = resolve; });
  const fetcher = vi.fn(async (_url: string, options?: RequestInit) => {
    if (options?.method === "POST") { await pending; ready = true; }
    return new Response(JSON.stringify({ phase: ready ? "ready" : "unavailable", read_only: !ready, reason: "Controlled cache failure" }));
  });
  vi.stubGlobal("fetch", fetcher);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(["note", "1"], { body: "last good body" });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const { unmount } = render(<QueryClientProvider client={client}><StorageNotice /></QueryClientProvider>);
  expect(await screen.findByRole("status")).toHaveTextContent("Controlled cache failure");
  expect(screen.queryByRole("alert")).toBeNull();
  const retry = screen.getByRole("button", { name: "Retry storage" });
  fireEvent.click(retry); fireEvent.click(retry);
  expect(retry).toBeDisabled();
  expect(fetcher.mock.calls.filter(([, options]) => options?.method === "POST")).toHaveLength(1);
  await act(async () => { complete(); });
  await waitFor(() => expect(screen.queryByRole("status")).toBeNull());
  expect(invalidate).toHaveBeenCalled();
  expect(client.getQueryData(["note", "1"])).toEqual({ body: "last good body" });
  unmount(); client.clear();
});
