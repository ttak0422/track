import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { queryKeys, useRenderQuery } from "./queries";

// Exercise the production hook and API together: cancelling the cache query must reach fetch.
let client: QueryClient;
let requests: { signal: AbortSignal; finish: (markdown: string) => void }[];
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  requests = [];
  vi.stubGlobal("fetch", vi.fn((_url: string, options: RequestInit) => new Promise<Response>((resolve) => {
    // Deliberately allow an aborted transport to finish late, as a cancellation race can do.
    requests.push({ signal: options.signal as AbortSignal, finish: (markdown) => resolve(new Response(JSON.stringify({ markdown }))) });
  })));
});
afterEach(() => {
  client.clear();
  vi.unstubAllGlobals();
});

describe("preview request cancellation", () => {
  it("aborts a superseded draft and ignores its late response", async () => {
    const view = renderHook(({ body }) => useRenderQuery(body, "work"), { wrapper, initialProps: { body: "old draft" } });
    expect(requests).toHaveLength(1);
    expect(requests[0].signal.aborted).toBe(false);
    expect(fetch).toHaveBeenCalledWith("/api/render?vault=work", expect.objectContaining({ method: "POST", body: JSON.stringify({ body: "old draft" }), signal: requests[0].signal }));
    view.rerender({ body: "latest draft" });
    expect(requests[0].signal.aborted).toBe(true);
    expect(requests).toHaveLength(2);
    await act(async () => requests[1].finish("latest rendered"));
    await waitFor(() => expect(view.result.current.data).toMatchObject({ markdown: "latest rendered", sourceBody: "latest draft" }));
    await act(async () => requests[0].finish("obsolete rendered"));
    expect(view.result.current.data).toMatchObject({ markdown: "latest rendered", sourceBody: "latest draft" });
    expect(client.getQueryData(queryKeys.render("old draft", "work"))).toBeUndefined();
    expect(view.result.current.isError).toBe(false);
    view.unmount();
  });

  it("aborts a pending request when the preview closes", () => {
    const view = renderHook(() => useRenderQuery("pending draft"), { wrapper });
    expect(requests[0].signal.aborted).toBe(false);
    view.unmount();
    expect(requests[0].signal.aborted).toBe(true);
  });

  it("keeps a shared request alive until its last preview closes", () => {
    const first = renderHook(() => useRenderQuery("shared body"), { wrapper });
    const second = renderHook(() => useRenderQuery("shared body"), { wrapper });
    expect(requests).toHaveLength(1);
    first.unmount();
    expect(requests[0].signal.aborted).toBe(false);
    second.unmount();
    expect(requests[0].signal.aborted).toBe(true);
  });
});
