import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { useCalendarEventsQuery, useSaveCalendarEvent } from "./calendarEventQueries";
import type { CalendarEvent } from "./calendarEvents";

afterEach(() => vi.unstubAllGlobals());

it("completes a confirmed save and updates its vault cache while the list refetch stalls", async () => {
  const event: CalendarEvent = { version: 1, id: "123e4567-e89b-42d3-a456-426614174000", revision: 2, title: "Saved", all_day: true, start: "2026-10-28", end: "2026-10-29", time_zone: "", description: "", location: "", url: "", note_id: "", status: "confirmed", deleted: false, updated_at: "2026-10-28T10:00:00Z" };
  let listRequests = 0;
  const fetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === "PUT") return Promise.resolve(new Response(JSON.stringify({ event }), { status: 200 }));
    listRequests++;
    if (listRequests === 1) return Promise.resolve(new Response(JSON.stringify({ events: [{ ...event, revision: 1, title: "Old" }] }), { status: 200 }));
    return new Promise<Response>(() => {});
  });
  vi.stubGlobal("fetch", fetch);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(["calendar-events", "home"], { events: [] });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  const { result, unmount } = renderHook(() => ({ list: useCalendarEventsQuery("work"), save: useSaveCalendarEvent("work") }), { wrapper });
  try {
    await waitFor(() => expect(result.current.list.data?.events[0].title).toBe("Old"));
    await act(async () => {
      await result.current.save.mutateAsync({ id: event.id, request: { ...event, expected_revision: 1, operation_id: "123e4567-e89b-42d3-a456-426614174001" } });
    });
    await waitFor(() => expect(result.current.save.isPending).toBe(false));
    expect(listRequests).toBe(2);
    expect(result.current.list.isFetching).toBe(true);
    expect(result.current.list.data?.events).toEqual([event]);
    expect(client.getQueryData(["calendar-events", "home"])).toEqual({ events: [] });
  } finally { unmount(); client.clear(); }
});
