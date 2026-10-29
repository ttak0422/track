import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { listCalendarEvents, saveCalendarEvent, type CalendarEvent, type CalendarEventWrite } from "./calendarEvents";
import { STATIC_MODE } from "./runtime";
import { useVaultScope } from "./vaultScope";

export function useCalendarEventsQuery(vault: string) {
  return useQuery({
    queryKey: ["calendar-events", vault],
    queryFn: () => listCalendarEvents(vault),
    enabled: !STATIC_MODE,
    // CLI/sync changes must refresh even on a filesystem that does not deliver watcher events.
    refetchInterval: 30_000,
  });
}

export function useSaveCalendarEvent(vault: string) {
  const client = useQueryClient();
  const { activeName } = useVaultScope();
  return useMutation({
    mutationFn: ({ id, request }: { id: string; request: CalendarEventWrite }) => saveCalendarEvent(vault, id, request, vault || activeName),
    retry: false,
    onSuccess: ({ event }) => {
      const queryKey = ["calendar-events", vault];
      client.setQueryData<{ events: CalendarEvent[] }>(queryKey, (current) => ({
        events: [...(current?.events ?? []).filter((item) => item.id !== event.id), event],
      }));
      // A confirmed write completes even if the subsequent list request stalls.
      void client.invalidateQueries({ queryKey });
    },
  });
}
