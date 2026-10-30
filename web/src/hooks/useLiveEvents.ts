import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { queryKeys, refreshNewNotes } from "../queries";
import { STATIC_MODE } from "../runtime";
import { subscribeToLiveEvents } from "./liveEventSubscription";

// Subscribes to the server's change stream and refreshes data when the vault
// changes on disk (edits from Neovim/CLI or a cloud sync). The backend reindexes
// before emitting, so invalidated queries refetch up-to-date results. A separate
// `data` event covers the vault's data/ directory: those files feed embedded
// viewspec charts (data.source / overlays[].source) without being indexed, so
// only the rendered charts are refetched.
export function useLiveEvents() {
  const queryClient = useQueryClient();

  useEffect(() => {
    // The published static site has no server/change stream to subscribe to.
    if (STATIC_MODE || typeof EventSource === "undefined") {
      return;
    }
    return subscribeToLiveEvents(new EventSource("/api/events"), {
      change: () => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.notes(), exact: true });
        void refreshNewNotes(queryClient);
        void queryClient.invalidateQueries({ queryKey: ["note"] });
        void queryClient.invalidateQueries({ queryKey: ["search"] });
        void queryClient.invalidateQueries({ queryKey: ["activity"] });
        void queryClient.invalidateQueries({ queryKey: ["agenda"] });
        void queryClient.invalidateQueries({ queryKey: ["graph"] });
        // Both task listings: a checkbox written from Neovim or the CLI moves a task in or out of
        // them, and the tasks page is nothing but that one query, so without this it keeps showing
        // work already finished elsewhere.
        void queryClient.invalidateQueries({ queryKey: queryKeys.tasks() });
        void queryClient.invalidateQueries({ queryKey: ["calendar-events"] });
      },
      data: () => {
        // Cancel first so a pending initial response cannot absorb this invalidation.
        for (const prefix of ["viewspec", "metrics-dashboard"]) {
          const queryKey = [prefix];
          void queryClient.cancelQueries({ queryKey }).then(() => queryClient.invalidateQueries({ queryKey }));
        }
      },
    });
  }, [queryClient]);
}
